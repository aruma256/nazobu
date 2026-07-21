package server

// expenseService の統合テスト。実 MySQL（testdb パッケージ経由）に対して
// セッション認証込みで RPC ハンドラを呼び、DB 往復を含む挙動を検証する。
// ticketService の統合テストと同じヘルパー（createTestUser / createTestEvent /
// setSessionCookie / connectCode）を共有する。TEST_DB_HOST 未設定の環境では skip される。

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/aruma256/nazobu/backend/internal/auth"
	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1/nazobuv1connect"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/aruma256/nazobu/backend/internal/id"
	"github.com/aruma256/nazobu/backend/internal/testdb"
)

// createTestTicket は event に紐づく ticket を作成して ID を返す。
// expense の ticket_id 紐付き検証用なので、参加者は付けず本体だけを作る。
func createTestTicket(t *testing.T, db *sql.DB, eventID, purchasedBy string) string {
	t.Helper()
	ticketID := id.New()
	if err := queries.New(db).CreateTicket(context.Background(), queries.CreateTicketParams{
		ID:              ticketID,
		EventID:         eventID,
		StartAt:         time.Date(2026, 8, 1, 14, 0, 0, 0, jst),
		PricePerPerson:  3000,
		MaxParticipants: 4,
		PurchasedBy:     purchasedBy,
		MeetingPlace:    "",
	}); err != nil {
		t.Fatalf("ticket 作成に失敗: %v", err)
	}
	return ticketID
}

// mustCreateExpense は sessionUserID のセッションで CreateExpense を呼び、成功前提で Expense を返す。
func mustCreateExpense(t *testing.T, ctx context.Context, svc nazobuv1connect.ExpenseServiceHandler, db *sql.DB, sessionUserID string, msg *nazobuv1.CreateExpenseRequest) *nazobuv1.Expense {
	t.Helper()
	req := connect.NewRequest(msg)
	setSessionCookie(t, db, req, sessionUserID)
	res, err := svc.CreateExpense(ctx, req)
	if err != nil {
		t.Fatalf("CreateExpense に失敗: %v", err)
	}
	return res.Msg.Expense
}

// mustGetExpense は sessionUserID のセッションで GetExpense を呼び、成功前提でレスポンスを返す。
func mustGetExpense(t *testing.T, ctx context.Context, svc nazobuv1connect.ExpenseServiceHandler, db *sql.DB, expenseID, sessionUserID string) *nazobuv1.GetExpenseResponse {
	t.Helper()
	req := connect.NewRequest(&nazobuv1.GetExpenseRequest{ExpenseId: expenseID})
	setSessionCookie(t, db, req, sessionUserID)
	res, err := svc.GetExpense(ctx, req)
	if err != nil {
		t.Fatalf("GetExpense に失敗: %v", err)
	}
	return res.Msg
}

// findExpenseParticipant は参加者一覧から user_id に一致する 1 件を返す。無ければ fail する。
func findExpenseParticipant(t *testing.T, parts []*nazobuv1.ExpenseParticipant, userID string) *nazobuv1.ExpenseParticipant {
	t.Helper()
	for _, p := range parts {
		if p.UserId == userID {
			return p
		}
	}
	t.Fatalf("参加者に user %s がいない", userID)
	return nil
}

func TestIntegrationCreateAndGetExpense(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newExpenseService(db)

	adminID := createTestUser(t, db, "admin-user", auth.RoleAdmin)
	payerID := createTestUser(t, db, "payer-member", auth.RoleMember)
	friendAID := createTestUser(t, db, "friend-a", auth.RoleMember)
	friendBID := createTestUser(t, db, "friend-b", auth.RoleMember)
	outsiderID := createTestUser(t, db, "outsider-member", auth.RoleMember)
	eventID := createTestEvent(t, db, "テスト公演")
	ticketID := createTestTicket(t, db, eventID, payerID)

	// ticket に紐付かない単独の精算を member（payer）が登録する。立替者はログイン user になる。
	created := mustCreateExpense(t, ctx, svc, db, payerID, &nazobuv1.CreateExpenseRequest{
		Title:      "打ち上げ",
		OccurredOn: "2026-07-20",
		Participants: []*nazobuv1.ExpenseParticipantInput{
			{UserId: friendAID, Amount: 1000},
			{UserId: friendBID, Amount: 2000},
		},
	})
	if created.Id == "" {
		t.Fatal("作成された expense の ID が空")
	}
	if created.PaidByUserId != payerID {
		t.Errorf("PaidByUserId = %q, want %q（立替者はログイン user）", created.PaidByUserId, payerID)
	}
	if created.TicketId != "" {
		t.Errorf("TicketId = %q, 紐付きなしのはず", created.TicketId)
	}
	if created.OccurredOn != "2026-07-20" {
		t.Errorf("OccurredOn = %q, want 2026-07-20", created.OccurredOn)
	}

	// GetExpense で参加者の負担額・精算状態が DB 往復で取れること。
	got := mustGetExpense(t, ctx, svc, db, created.Id, payerID)
	if len(got.Participants) != 2 {
		t.Fatalf("参加者数 = %d, want 2", len(got.Participants))
	}
	pa := findExpenseParticipant(t, got.Participants, friendAID)
	if pa.Amount != 1000 || pa.Settled {
		t.Errorf("friendA = {amount:%d settled:%v}, want {1000 false}", pa.Amount, pa.Settled)
	}
	if pa.Name != "friend-a" {
		t.Errorf("friendA.Name = %q, want friend-a", pa.Name)
	}
	pb := findExpenseParticipant(t, got.Participants, friendBID)
	if pb.Amount != 2000 || pb.Settled {
		t.Errorf("friendB = {amount:%d settled:%v}, want {2000 false}", pb.Amount, pb.Settled)
	}

	// can_edit: admin と立替者本人は true、無関係な member は false。
	if !mustGetExpense(t, ctx, svc, db, created.Id, adminID).CanEdit {
		t.Error("admin の CanEdit が false")
	}
	if !got.CanEdit {
		t.Error("立替者本人の CanEdit が false")
	}
	if mustGetExpense(t, ctx, svc, db, created.Id, outsiderID).CanEdit {
		t.Error("無関係な member の CanEdit が true")
	}

	// ticket 紐付きありの登録では event_title が公演名で埋まること。
	linked := mustCreateExpense(t, ctx, svc, db, payerID, &nazobuv1.CreateExpenseRequest{
		TicketId:   ticketID,
		Title:      "公演後の飲み会",
		OccurredOn: "2026-07-21",
		Participants: []*nazobuv1.ExpenseParticipantInput{
			{UserId: friendAID, Amount: 1500},
		},
	})
	if linked.TicketId != ticketID {
		t.Errorf("TicketId = %q, want %q", linked.TicketId, ticketID)
	}
	if linked.EventTitle != "テスト公演" {
		t.Errorf("EventTitle = %q, want テスト公演", linked.EventTitle)
	}
}

func TestIntegrationCreateExpenseValidation(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newExpenseService(db)

	payerID := createTestUser(t, db, "payer-member", auth.RoleMember)
	friendID := createTestUser(t, db, "friend", auth.RoleMember)
	eventID := createTestEvent(t, db, "テスト公演")
	ticketID := createTestTicket(t, db, eventID, payerID)

	// 妥当なリクエストを土台に、1 項目ずつ壊して InvalidArgument になることを確認する。
	newReq := func(mutate func(*nazobuv1.CreateExpenseRequest)) *connect.Request[nazobuv1.CreateExpenseRequest] {
		msg := &nazobuv1.CreateExpenseRequest{
			TicketId:   ticketID,
			Title:      "打ち上げ",
			OccurredOn: "2026-07-20",
			Participants: []*nazobuv1.ExpenseParticipantInput{
				{UserId: friendID, Amount: 1000},
			},
		}
		mutate(msg)
		req := connect.NewRequest(msg)
		setSessionCookie(t, db, req, payerID)
		return req
	}

	cases := []struct {
		name   string
		mutate func(*nazobuv1.CreateExpenseRequest)
	}{
		{"title 空", func(m *nazobuv1.CreateExpenseRequest) { m.Title = "" }},
		{"occurred_on 不正", func(m *nazobuv1.CreateExpenseRequest) { m.OccurredOn = "2026/07/20" }},
		{"participants 空", func(m *nazobuv1.CreateExpenseRequest) { m.Participants = nil }},
		{"立替者自身を participants に含める", func(m *nazobuv1.CreateExpenseRequest) {
			m.Participants = []*nazobuv1.ExpenseParticipantInput{{UserId: payerID, Amount: 1000}}
		}},
		{"存在しない ticket_id", func(m *nazobuv1.CreateExpenseRequest) { m.TicketId = id.New() }},
		{"存在しない user", func(m *nazobuv1.CreateExpenseRequest) {
			m.Participants = []*nazobuv1.ExpenseParticipantInput{{UserId: id.New(), Amount: 1000}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateExpense(ctx, newReq(tc.mutate))
			if got := connectCode(t, err); got != connect.CodeInvalidArgument {
				t.Errorf("code = %v, want %v", got, connect.CodeInvalidArgument)
			}
		})
	}
}

func TestIntegrationListExpenses(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newExpenseService(db)

	payerID := createTestUser(t, db, "payer-member", auth.RoleMember)
	friendAID := createTestUser(t, db, "friend-a", auth.RoleMember)
	friendBID := createTestUser(t, db, "friend-b", auth.RoleMember)
	eventID := createTestEvent(t, db, "テスト公演")
	ticketID := createTestTicket(t, db, eventID, payerID)

	// occurred_on が異なる 3 件を作る（作成順と降順が一致しないようにする）。
	older := mustCreateExpense(t, ctx, svc, db, payerID, &nazobuv1.CreateExpenseRequest{
		Title: "7/10 の会", OccurredOn: "2026-07-10",
		Participants: []*nazobuv1.ExpenseParticipantInput{{UserId: friendAID, Amount: 500}},
	})
	newest := mustCreateExpense(t, ctx, svc, db, payerID, &nazobuv1.CreateExpenseRequest{
		Title: "7/20 の会", OccurredOn: "2026-07-20",
		Participants: []*nazobuv1.ExpenseParticipantInput{{UserId: friendAID, Amount: 500}},
	})
	// ticket 紐付きあり。参加者 2 名・片方精算済みで集計を検証する。
	middle := mustCreateExpense(t, ctx, svc, db, payerID, &nazobuv1.CreateExpenseRequest{
		TicketId: ticketID, Title: "7/15 の会", OccurredOn: "2026-07-15",
		Participants: []*nazobuv1.ExpenseParticipantInput{
			{UserId: friendAID, Amount: 1000},
			{UserId: friendBID, Amount: 2000},
		},
	})
	// middle の friendA を精算済みにしておく。
	settleReq := connect.NewRequest(&nazobuv1.UpdateExpenseParticipantSettlementRequest{
		ExpenseId: middle.Id, UserId: friendAID, Settled: true,
	})
	setSessionCookie(t, db, settleReq, payerID)
	if _, err := svc.UpdateExpenseParticipantSettlement(ctx, settleReq); err != nil {
		t.Fatalf("精算状態の更新に失敗: %v", err)
	}

	// ticket_id 未指定は全件を occurred_on 降順で返すこと。
	listReq := connect.NewRequest(&nazobuv1.ListExpensesRequest{})
	setSessionCookie(t, db, listReq, payerID)
	listRes, err := svc.ListExpenses(ctx, listReq)
	if err != nil {
		t.Fatalf("ListExpenses に失敗: %v", err)
	}
	gotOrder := make([]string, 0, len(listRes.Msg.Expenses))
	for _, e := range listRes.Msg.Expenses {
		gotOrder = append(gotOrder, e.Id)
	}
	wantOrder := []string{newest.Id, middle.Id, older.Id}
	if !slices.Equal(gotOrder, wantOrder) {
		t.Errorf("並び順 = %v, want %v（occurred_on 降順）", gotOrder, wantOrder)
	}

	// middle の集計（合計金額 / 参加者数 / 精算済み数 / 参加者名）を検証する。
	var mid *nazobuv1.Expense
	for _, e := range listRes.Msg.Expenses {
		if e.Id == middle.Id {
			mid = e
		}
	}
	if mid == nil {
		t.Fatal("一覧に middle がいない")
	}
	if mid.TotalAmount != 3000 {
		t.Errorf("TotalAmount = %d, want 3000", mid.TotalAmount)
	}
	if mid.ParticipantCount != 2 {
		t.Errorf("ParticipantCount = %d, want 2", mid.ParticipantCount)
	}
	if mid.SettledCount != 1 {
		t.Errorf("SettledCount = %d, want 1", mid.SettledCount)
	}
	for _, want := range []string{"friend-a", "friend-b"} {
		if !slices.Contains(mid.ParticipantNames, want) {
			t.Errorf("ParticipantNames に %q がいない: %v", want, mid.ParticipantNames)
		}
	}

	// ticket_id 指定はその ticket に紐づく expense のみ返すこと。
	filterReq := connect.NewRequest(&nazobuv1.ListExpensesRequest{TicketId: ticketID})
	setSessionCookie(t, db, filterReq, payerID)
	filterRes, err := svc.ListExpenses(ctx, filterReq)
	if err != nil {
		t.Fatalf("ListExpenses(ticket 絞り込み) に失敗: %v", err)
	}
	if len(filterRes.Msg.Expenses) != 1 || filterRes.Msg.Expenses[0].Id != middle.Id {
		t.Errorf("ticket 絞り込み結果 = %d 件, middle のみのはず", len(filterRes.Msg.Expenses))
	}
}

func TestIntegrationUpdateExpense(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newExpenseService(db)

	adminID := createTestUser(t, db, "admin-user", auth.RoleAdmin)
	payerID := createTestUser(t, db, "payer-member", auth.RoleMember)
	friendAID := createTestUser(t, db, "friend-a", auth.RoleMember)
	friendBID := createTestUser(t, db, "friend-b", auth.RoleMember)
	friendCID := createTestUser(t, db, "friend-c", auth.RoleMember)
	outsiderID := createTestUser(t, db, "outsider-member", auth.RoleMember)

	// payer が [friendA:1000, friendB:2000] で登録し、friendA を精算済みにしておく。
	created := mustCreateExpense(t, ctx, svc, db, payerID, &nazobuv1.CreateExpenseRequest{
		Title: "打ち上げ", OccurredOn: "2026-07-20",
		Participants: []*nazobuv1.ExpenseParticipantInput{
			{UserId: friendAID, Amount: 1000},
			{UserId: friendBID, Amount: 2000},
		},
	})
	settleReq := connect.NewRequest(&nazobuv1.UpdateExpenseParticipantSettlementRequest{
		ExpenseId: created.Id, UserId: friendAID, Settled: true,
	})
	setSessionCookie(t, db, settleReq, payerID)
	if _, err := svc.UpdateExpenseParticipantSettlement(ctx, settleReq); err != nil {
		t.Fatalf("精算状態の更新に失敗: %v", err)
	}

	// admin が全量置換で更新: friendA は残す（amount 変更 / settled_at 保持）、
	// friendB は消す、friendC は新規追加。paid_by は payer のまま。
	updateReq := connect.NewRequest(&nazobuv1.UpdateExpenseRequest{
		ExpenseId: created.Id, Title: "打ち上げ（更新）", OccurredOn: "2026-07-22",
		PaidByUserId: payerID,
		Participants: []*nazobuv1.ExpenseParticipantInput{
			{UserId: friendAID, Amount: 1500},
			{UserId: friendCID, Amount: 3000},
		},
	})
	setSessionCookie(t, db, updateReq, adminID)
	if _, err := svc.UpdateExpense(ctx, updateReq); err != nil {
		t.Fatalf("UpdateExpense に失敗: %v", err)
	}

	got := mustGetExpense(t, ctx, svc, db, created.Id, payerID)
	if got.Expense.Title != "打ち上げ（更新）" || got.Expense.OccurredOn != "2026-07-22" {
		t.Errorf("本体更新が反映されていない: title=%q occurred_on=%q", got.Expense.Title, got.Expense.OccurredOn)
	}
	if len(got.Participants) != 2 {
		t.Fatalf("更新後の参加者数 = %d, want 2", len(got.Participants))
	}
	// 残った friendA は amount が更新され、settled_at（精算済み）が保持されていること。
	pa := findExpenseParticipant(t, got.Participants, friendAID)
	if pa.Amount != 1500 {
		t.Errorf("friendA.Amount = %d, want 1500", pa.Amount)
	}
	if !pa.Settled {
		t.Error("friendA の精算済みが更新で消えた（settled_at が保持されていない）")
	}
	// 新規 friendC は追加され、未精算であること。
	pc := findExpenseParticipant(t, got.Participants, friendCID)
	if pc.Amount != 3000 || pc.Settled {
		t.Errorf("friendC = {amount:%d settled:%v}, want {3000 false}", pc.Amount, pc.Settled)
	}
	// 消えた friendB は含まれないこと。
	for _, p := range got.Participants {
		if p.UserId == friendBID {
			t.Error("削除したはずの friendB が残っている")
		}
	}

	// 新 paid_by が participants に含まれると InvalidArgument。
	badReq := connect.NewRequest(&nazobuv1.UpdateExpenseRequest{
		ExpenseId: created.Id, Title: "打ち上げ", OccurredOn: "2026-07-22",
		PaidByUserId: friendCID,
		Participants: []*nazobuv1.ExpenseParticipantInput{
			{UserId: friendAID, Amount: 1500},
			{UserId: friendCID, Amount: 3000},
		},
	})
	setSessionCookie(t, db, badReq, adminID)
	if _, err := svc.UpdateExpense(ctx, badReq); connectCode(t, err) != connect.CodeInvalidArgument {
		t.Errorf("paid_by を participants に含めた code = %v, want %v", connectCode(t, err), connect.CodeInvalidArgument)
	}

	// 無関係な member（非 admin・非立替者）の更新は PermissionDenied。
	denyReq := connect.NewRequest(&nazobuv1.UpdateExpenseRequest{
		ExpenseId: created.Id, Title: "勝手に更新", OccurredOn: "2026-07-22",
		PaidByUserId: payerID,
		Participants: []*nazobuv1.ExpenseParticipantInput{{UserId: friendAID, Amount: 1500}},
	})
	setSessionCookie(t, db, denyReq, outsiderID)
	if _, err := svc.UpdateExpense(ctx, denyReq); connectCode(t, err) != connect.CodePermissionDenied {
		t.Errorf("他 member の更新 code = %v, want %v", connectCode(t, err), connect.CodePermissionDenied)
	}

	// paid_by を admin に付け替えられること（新 paid_by は participants に含まない）。
	changePayerReq := connect.NewRequest(&nazobuv1.UpdateExpenseRequest{
		ExpenseId: created.Id, Title: "打ち上げ（立替者変更）", OccurredOn: "2026-07-22",
		PaidByUserId: adminID,
		Participants: []*nazobuv1.ExpenseParticipantInput{
			{UserId: friendAID, Amount: 1500},
			{UserId: friendCID, Amount: 3000},
		},
	})
	setSessionCookie(t, db, changePayerReq, adminID)
	changed, err := svc.UpdateExpense(ctx, changePayerReq)
	if err != nil {
		t.Fatalf("paid_by 変更に失敗: %v", err)
	}
	if changed.Msg.Expense.PaidByUserId != adminID {
		t.Errorf("PaidByUserId = %q, want %q", changed.Msg.Expense.PaidByUserId, adminID)
	}
	if changed.Msg.Expense.PayerName != "admin-user" {
		t.Errorf("PayerName = %q, want admin-user", changed.Msg.Expense.PayerName)
	}
}

func TestIntegrationUpdateExpenseParticipantSettlement(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newExpenseService(db)

	payerID := createTestUser(t, db, "payer-member", auth.RoleMember)
	friendID := createTestUser(t, db, "friend", auth.RoleMember)
	strangerID := createTestUser(t, db, "stranger", auth.RoleMember)
	outsiderID := createTestUser(t, db, "outsider-member", auth.RoleMember)

	created := mustCreateExpense(t, ctx, svc, db, payerID, &nazobuv1.CreateExpenseRequest{
		Title: "打ち上げ", OccurredOn: "2026-07-20",
		Participants: []*nazobuv1.ExpenseParticipantInput{{UserId: friendID, Amount: 1000}},
	})

	toggle := func(t *testing.T, sessionUserID, userID string, settled bool) error {
		t.Helper()
		req := connect.NewRequest(&nazobuv1.UpdateExpenseParticipantSettlementRequest{
			ExpenseId: created.Id, UserId: userID, Settled: settled,
		})
		setSessionCookie(t, db, req, sessionUserID)
		_, err := svc.UpdateExpenseParticipantSettlement(ctx, req)
		return err
	}
	settledOf := func(t *testing.T, userID string) bool {
		t.Helper()
		got := mustGetExpense(t, ctx, svc, db, created.Id, payerID)
		return findExpenseParticipant(t, got.Participants, userID).Settled
	}

	// 未精算 → 精算済み → 未精算 とトグルできること。
	if err := toggle(t, payerID, friendID, true); err != nil {
		t.Fatalf("精算済みへの更新に失敗: %v", err)
	}
	if !settledOf(t, friendID) {
		t.Error("精算済みにならなかった")
	}
	if err := toggle(t, payerID, friendID, false); err != nil {
		t.Fatalf("未精算への更新に失敗: %v", err)
	}
	if settledOf(t, friendID) {
		t.Error("未精算に戻らなかった")
	}

	// 参加者でない user を指定すると NotFound。
	if err := toggle(t, payerID, strangerID, true); connectCode(t, err) != connect.CodeNotFound {
		t.Errorf("非参加者指定 code = %v, want %v", connectCode(t, err), connect.CodeNotFound)
	}

	// 無関係な member の操作は PermissionDenied。
	if err := toggle(t, outsiderID, friendID, true); connectCode(t, err) != connect.CodePermissionDenied {
		t.Errorf("他 member の操作 code = %v, want %v", connectCode(t, err), connect.CodePermissionDenied)
	}
}

func TestIntegrationDeleteExpense(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newExpenseService(db)

	payerID := createTestUser(t, db, "payer-member", auth.RoleMember)
	friendAID := createTestUser(t, db, "friend-a", auth.RoleMember)
	friendBID := createTestUser(t, db, "friend-b", auth.RoleMember)
	outsiderID := createTestUser(t, db, "outsider-member", auth.RoleMember)

	created := mustCreateExpense(t, ctx, svc, db, payerID, &nazobuv1.CreateExpenseRequest{
		Title: "打ち上げ", OccurredOn: "2026-07-20",
		Participants: []*nazobuv1.ExpenseParticipantInput{
			{UserId: friendAID, Amount: 1000},
			{UserId: friendBID, Amount: 2000},
		},
	})

	// 無関係な member の削除は PermissionDenied。
	denyReq := connect.NewRequest(&nazobuv1.DeleteExpenseRequest{ExpenseId: created.Id})
	setSessionCookie(t, db, denyReq, outsiderID)
	if _, err := svc.DeleteExpense(ctx, denyReq); connectCode(t, err) != connect.CodePermissionDenied {
		t.Errorf("他 member の削除 code = %v, want %v", connectCode(t, err), connect.CodePermissionDenied)
	}

	// 立替者本人は削除でき、expense_participants も CASCADE で消えること。
	deleteReq := connect.NewRequest(&nazobuv1.DeleteExpenseRequest{ExpenseId: created.Id})
	setSessionCookie(t, db, deleteReq, payerID)
	if _, err := svc.DeleteExpense(ctx, deleteReq); err != nil {
		t.Fatalf("DeleteExpense に失敗: %v", err)
	}

	getReq := connect.NewRequest(&nazobuv1.GetExpenseRequest{ExpenseId: created.Id})
	setSessionCookie(t, db, getReq, payerID)
	if _, err := svc.GetExpense(ctx, getReq); connectCode(t, err) != connect.CodeNotFound {
		t.Errorf("削除後の GetExpense code = %v, want %v", connectCode(t, err), connect.CodeNotFound)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM expense_participants WHERE expense_id = ?", created.Id).Scan(&count); err != nil {
		t.Fatalf("expense_participants の件数取得に失敗: %v", err)
	}
	if count != 0 {
		t.Errorf("削除後の expense_participants = %d 件, CASCADE で 0 になるはず", count)
	}
}

func TestIntegrationExpenseTicketDeletionSetsNull(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newExpenseService(db)

	payerID := createTestUser(t, db, "payer-member", auth.RoleMember)
	friendID := createTestUser(t, db, "friend", auth.RoleMember)
	eventID := createTestEvent(t, db, "テスト公演")
	ticketID := createTestTicket(t, db, eventID, payerID)

	created := mustCreateExpense(t, ctx, svc, db, payerID, &nazobuv1.CreateExpenseRequest{
		TicketId: ticketID, Title: "公演後の飲み会", OccurredOn: "2026-07-20",
		Participants: []*nazobuv1.ExpenseParticipantInput{{UserId: friendID, Amount: 1000}},
	})
	if created.TicketId != ticketID {
		t.Fatalf("登録直後の TicketId = %q, want %q", created.TicketId, ticketID)
	}

	// ticket を直接削除する。expense は残り、ticket_id は SET NULL されるはず。
	if _, err := db.Exec("DELETE FROM tickets WHERE id = ?", ticketID); err != nil {
		t.Fatalf("ticket の削除に失敗: %v", err)
	}

	got := mustGetExpense(t, ctx, svc, db, created.Id, payerID)
	if got.Expense.TicketId != "" {
		t.Errorf("ticket 削除後の TicketId = %q, NULL（空文字）のはず", got.Expense.TicketId)
	}
	if got.Expense.EventTitle != "" {
		t.Errorf("ticket 削除後の EventTitle = %q, 空文字のはず", got.Expense.EventTitle)
	}
	// 金銭記録として expense 本体と参加者は残ること。
	if len(got.Participants) != 1 {
		t.Errorf("ticket 削除後の参加者数 = %d, want 1（expense は残る）", len(got.Participants))
	}
}
