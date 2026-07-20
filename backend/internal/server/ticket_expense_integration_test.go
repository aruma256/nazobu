package server

// 追加精算（ticket_expenses）の統合テスト。実 MySQL に対してセッション認証込みで
// RPC ハンドラを呼び、登録・更新・削除・精算トグルとマイページへの反映を検証する。
// TEST_DB_HOST 未設定の環境では skip される。

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/aruma256/nazobu/backend/internal/auth"
	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/aruma256/nazobu/backend/internal/testdb"
)

// mustGetTicket は userID のセッションで GetTicket を呼ぶ。
func mustGetTicket(t *testing.T, db *sql.DB, ticketID, userID string) *nazobuv1.GetTicketResponse {
	t.Helper()
	svc := newTicketService(db)
	req := connect.NewRequest(&nazobuv1.GetTicketRequest{TicketId: ticketID})
	setSessionCookie(t, db, req, userID)
	res, err := svc.GetTicket(context.Background(), req)
	if err != nil {
		t.Fatalf("GetTicket に失敗: %v", err)
	}
	return res.Msg
}

func TestIntegrationTicketExpenseLifecycle(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newTicketService(db)

	adminID := createTestUser(t, db, "admin-user", auth.RoleAdmin)
	organizerID := createTestUser(t, db, "organizer-user", auth.RoleMember) // 飲み会の幹事（チケット立替者ではない）
	memberID := createTestUser(t, db, "member-user", auth.RoleMember)
	eventID := createTestEvent(t, db, "テスト公演")
	ticketID := mustCreateTicket(t, db, adminID, eventID, "2026-07-01T14:00:00+09:00", []string{adminID, organizerID, memberID})

	// 幹事（チケット参加者・非立替者）が不均等な負担額で追加精算を登録できる
	createReq := connect.NewRequest(&nazobuv1.CreateTicketExpenseRequest{
		TicketId: ticketID,
		Title:    "打ち上げ飲み会",
		Participants: []*nazobuv1.TicketExpenseParticipantInput{
			{UserId: organizerID, Amount: 3000},
			{UserId: memberID, Amount: 4500}, // 多めに飲食した人
		},
	})
	setSessionCookie(t, db, createReq, organizerID)
	if _, err := svc.CreateTicketExpense(ctx, createReq); err != nil {
		t.Fatalf("CreateTicketExpense に失敗: %v", err)
	}

	// GetTicket に expense が対象者・負担額つきで載ること
	detail := mustGetTicket(t, db, ticketID, organizerID)
	if len(detail.Expenses) != 1 {
		t.Fatalf("expenses 数 = %d, want 1", len(detail.Expenses))
	}
	expense := detail.Expenses[0]
	if expense.Title != "打ち上げ飲み会" {
		t.Errorf("Title = %q, want %q", expense.Title, "打ち上げ飲み会")
	}
	if expense.PaidByUserId != organizerID || expense.PayerName != "organizer-user" {
		t.Errorf("立替者 = %q(%q), want organizer", expense.PaidByUserId, expense.PayerName)
	}
	if !expense.CanEdit {
		t.Error("立替者本人の CanEdit が false")
	}
	if len(expense.Participants) != 2 {
		t.Fatalf("対象者数 = %d, want 2", len(expense.Participants))
	}
	byUser := make(map[string]*nazobuv1.TicketExpenseParticipant, len(expense.Participants))
	for _, p := range expense.Participants {
		byUser[p.UserId] = p
	}
	if p := byUser[organizerID]; p == nil || p.Amount != 3000 || !p.IsPayer || !p.Settled {
		t.Errorf("幹事の行が不正: %+v（amount 3000 / is_payer / settled を期待）", p)
	}
	if p := byUser[memberID]; p == nil || p.Amount != 4500 || p.IsPayer || p.Settled {
		t.Errorf("member の行が不正: %+v（amount 4500 / 未精算を期待）", p)
	}

	// 対象者（非立替者）から見ると expense は編集不可、expense 追加は参加者なので可
	memberView := mustGetTicket(t, db, ticketID, memberID)
	if memberView.Expenses[0].CanEdit {
		t.Error("非立替者の expense CanEdit が true になっている")
	}
	if !memberView.CanAddExpense {
		t.Error("チケット参加者の CanAddExpense が false になっている")
	}

	// 精算トグル: 立替者が member を精算済みにできる
	settleReq := connect.NewRequest(&nazobuv1.UpdateTicketExpenseSettlementRequest{
		ExpenseId: expense.Id,
		UserId:   memberID,
		Settled:  true,
	})
	setSessionCookie(t, db, settleReq, organizerID)
	if _, err := svc.UpdateTicketExpenseSettlement(ctx, settleReq); err != nil {
		t.Fatalf("UpdateTicketExpenseSettlement に失敗: %v", err)
	}
	detail = mustGetTicket(t, db, ticketID, organizerID)
	for _, p := range detail.Expenses[0].Participants {
		if p.UserId == memberID && !p.Settled {
			t.Error("精算済みにしたはずの member が未精算のまま")
		}
	}

	// 更新: 金額変更（精算状態は維持）+ 対象者の入れ替え（admin 追加・幹事自身を除外）
	updateReq := connect.NewRequest(&nazobuv1.UpdateTicketExpenseRequest{
		ExpenseId: expense.Id,
		Title:    "二次会",
		Participants: []*nazobuv1.TicketExpenseParticipantInput{
			{UserId: memberID, Amount: 5000},
			{UserId: adminID, Amount: 2000},
		},
	})
	setSessionCookie(t, db, updateReq, organizerID)
	if _, err := svc.UpdateTicketExpense(ctx, updateReq); err != nil {
		t.Fatalf("UpdateTicketExpense に失敗: %v", err)
	}
	detail = mustGetTicket(t, db, ticketID, organizerID)
	expense = detail.Expenses[0]
	if expense.Title != "二次会" {
		t.Errorf("更新後 Title = %q, want %q", expense.Title, "二次会")
	}
	if len(expense.Participants) != 2 {
		t.Fatalf("更新後の対象者数 = %d, want 2", len(expense.Participants))
	}
	byUser = make(map[string]*nazobuv1.TicketExpenseParticipant, len(expense.Participants))
	for _, p := range expense.Participants {
		byUser[p.UserId] = p
	}
	if p := byUser[memberID]; p == nil || p.Amount != 5000 || !p.Settled {
		t.Errorf("member の行が不正: %+v（金額 5000 に変わり、精算済みが維持されることを期待）", p)
	}
	if p := byUser[adminID]; p == nil || p.Amount != 2000 || p.Settled {
		t.Errorf("admin の行が不正: %+v（新規追加は未精算を期待）", p)
	}
	if _, ok := byUser[organizerID]; ok {
		t.Error("指定から外した幹事が対象者に残っている")
	}

	// 削除: 立替者が削除でき、GetTicket から消える
	deleteReq := connect.NewRequest(&nazobuv1.DeleteTicketExpenseRequest{ExpenseId: expense.Id})
	setSessionCookie(t, db, deleteReq, organizerID)
	if _, err := svc.DeleteTicketExpense(ctx, deleteReq); err != nil {
		t.Fatalf("DeleteTicketExpense に失敗: %v", err)
	}
	detail = mustGetTicket(t, db, ticketID, organizerID)
	if len(detail.Expenses) != 0 {
		t.Errorf("削除後も expenses が残っている: %d 件", len(detail.Expenses))
	}
}

func TestIntegrationTicketExpenseValidationAndAuthorization(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newTicketService(db)

	adminID := createTestUser(t, db, "admin-user", auth.RoleAdmin)
	participantID := createTestUser(t, db, "participant-user", auth.RoleMember)
	outsiderID := createTestUser(t, db, "outsider-user", auth.RoleMember) // チケット非参加者
	eventID := createTestEvent(t, db, "テスト公演")
	ticketID := mustCreateTicket(t, db, adminID, eventID, "2026-07-01T14:00:00+09:00", []string{adminID, participantID})

	newCreateReq := func(participants []*nazobuv1.TicketExpenseParticipantInput) *connect.Request[nazobuv1.CreateTicketExpenseRequest] {
		return connect.NewRequest(&nazobuv1.CreateTicketExpenseRequest{
			TicketId:     ticketID,
			Title:        "飲み会",
			Participants: participants,
		})
	}
	validParticipants := []*nazobuv1.TicketExpenseParticipantInput{{UserId: participantID, Amount: 3000}}

	t.Run("session cookie なしは Unauthenticated", func(t *testing.T) {
		_, err := svc.CreateTicketExpense(ctx, newCreateReq(validParticipants))
		if got := connectCode(t, err); got != connect.CodeUnauthenticated {
			t.Errorf("code = %v, want %v", got, connect.CodeUnauthenticated)
		}
	})

	t.Run("チケット非参加者の登録は PermissionDenied", func(t *testing.T) {
		req := newCreateReq(validParticipants)
		setSessionCookie(t, db, req, outsiderID)
		_, err := svc.CreateTicketExpense(ctx, req)
		if got := connectCode(t, err); got != connect.CodePermissionDenied {
			t.Errorf("code = %v, want %v", got, connect.CodePermissionDenied)
		}
	})

	t.Run("admin はチケット非参加者でも登録できる", func(t *testing.T) {
		req := newCreateReq(validParticipants)
		setSessionCookie(t, db, req, adminID)
		if _, err := svc.CreateTicketExpense(ctx, req); err != nil {
			t.Errorf("admin の登録に失敗: %v", err)
		}
	})

	t.Run("チケット非参加者を対象にすると FailedPrecondition", func(t *testing.T) {
		req := newCreateReq([]*nazobuv1.TicketExpenseParticipantInput{{UserId: outsiderID, Amount: 3000}})
		setSessionCookie(t, db, req, participantID)
		_, err := svc.CreateTicketExpense(ctx, req)
		if got := connectCode(t, err); got != connect.CodeFailedPrecondition {
			t.Errorf("code = %v, want %v", got, connect.CodeFailedPrecondition)
		}
	})

	t.Run("入力バリデーションは InvalidArgument", func(t *testing.T) {
		cases := map[string]*connect.Request[nazobuv1.CreateTicketExpenseRequest]{
			"title なし": connect.NewRequest(&nazobuv1.CreateTicketExpenseRequest{
				TicketId:     ticketID,
				Participants: validParticipants,
			}),
			"対象者なし": newCreateReq(nil),
			"対象者の重複": newCreateReq([]*nazobuv1.TicketExpenseParticipantInput{
				{UserId: participantID, Amount: 3000},
				{UserId: participantID, Amount: 4000},
			}),
			"負の金額": newCreateReq([]*nazobuv1.TicketExpenseParticipantInput{
				{UserId: participantID, Amount: -1},
			}),
		}
		for name, req := range cases {
			setSessionCookie(t, db, req, participantID)
			_, err := svc.CreateTicketExpense(ctx, req)
			if got := connectCode(t, err); got != connect.CodeInvalidArgument {
				t.Errorf("%s: code = %v, want %v", name, got, connect.CodeInvalidArgument)
			}
		}
	})

	// 以降は participant が立て替えた expense を使う
	createReq := newCreateReq([]*nazobuv1.TicketExpenseParticipantInput{
		{UserId: participantID, Amount: 3000},
		{UserId: adminID, Amount: 3000},
	})
	setSessionCookie(t, db, createReq, participantID)
	if _, err := svc.CreateTicketExpense(ctx, createReq); err != nil {
		t.Fatalf("CreateTicketExpense に失敗: %v", err)
	}
	detail := mustGetTicket(t, db, ticketID, participantID)
	var expenseID string
	for _, c := range detail.Expenses {
		if c.PaidByUserId == participantID {
			expenseID = c.Id
		}
	}
	if expenseID == "" {
		t.Fatal("participant 立替の expense が見つからない")
	}

	t.Run("立替者でも admin でもない編集は PermissionDenied", func(t *testing.T) {
		req := connect.NewRequest(&nazobuv1.UpdateTicketExpenseRequest{
			ExpenseId:     expenseID,
			Title:        "改名",
			Participants: validParticipants,
		})
		setSessionCookie(t, db, req, outsiderID)
		_, err := svc.UpdateTicketExpense(ctx, req)
		if got := connectCode(t, err); got != connect.CodePermissionDenied {
			t.Errorf("code = %v, want %v", got, connect.CodePermissionDenied)
		}
	})

	t.Run("立替者本人への精算トグルは FailedPrecondition", func(t *testing.T) {
		req := connect.NewRequest(&nazobuv1.UpdateTicketExpenseSettlementRequest{
			ExpenseId: expenseID,
			UserId:   participantID,
			Settled:  true,
		})
		setSessionCookie(t, db, req, participantID)
		_, err := svc.UpdateTicketExpenseSettlement(ctx, req)
		if got := connectCode(t, err); got != connect.CodeFailedPrecondition {
			t.Errorf("code = %v, want %v", got, connect.CodeFailedPrecondition)
		}
	})

	t.Run("対象者でない user への精算トグルは NotFound", func(t *testing.T) {
		req := connect.NewRequest(&nazobuv1.UpdateTicketExpenseSettlementRequest{
			ExpenseId: expenseID,
			UserId:   outsiderID,
			Settled:  true,
		})
		setSessionCookie(t, db, req, participantID)
		_, err := svc.UpdateTicketExpenseSettlement(ctx, req)
		if got := connectCode(t, err); got != connect.CodeNotFound {
			t.Errorf("code = %v, want %v", got, connect.CodeNotFound)
		}
	})

	t.Run("存在しない expense は NotFound", func(t *testing.T) {
		req := connect.NewRequest(&nazobuv1.DeleteTicketExpenseRequest{ExpenseId: "00000000-0000-0000-0000-000000000000"})
		setSessionCookie(t, db, req, adminID)
		_, err := svc.DeleteTicketExpense(ctx, req)
		if got := connectCode(t, err); got != connect.CodeNotFound {
			t.Errorf("code = %v, want %v", got, connect.CodeNotFound)
		}
	})
}

func TestIntegrationTicketExpenseMyPage(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newTicketService(db)

	// 基準時刻を 2026-07-15 12:00 JST に固定する（対象チケットは 7/10 開催 = 開演済み）
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, jst)
	mypage := &myPageService{db: db, q: queries.New(db), now: func() time.Time { return now }}

	adminID := createTestUser(t, db, "admin-user", auth.RoleAdmin)       // チケット立替者
	organizerID := createTestUser(t, db, "organizer-user", auth.RoleMember) // 飲み会の幹事
	memberID := createTestUser(t, db, "member-user", auth.RoleMember)
	eventID := createTestEvent(t, db, "テスト公演")
	ticketID := mustCreateTicket(t, db, adminID, eventID, "2026-07-10T14:00:00+09:00", []string{adminID, organizerID, memberID})

	// まずチケット代を全員精算済みにして、チケット代由来の未精算を消しておく
	for _, uid := range []string{organizerID, memberID} {
		req := connect.NewRequest(&nazobuv1.UpdateTicketParticipantSettlementRequest{
			TicketId: ticketID,
			UserId:   uid,
			Settled:  true,
		})
		setSessionCookie(t, db, req, adminID)
		if _, err := svc.UpdateTicketParticipantSettlement(ctx, req); err != nil {
			t.Fatalf("チケット代の精算に失敗: %v", err)
		}
	}

	listUnsettled := func(t *testing.T, userID string) []string {
		t.Helper()
		req := connect.NewRequest(&nazobuv1.ListMyUnsettledTicketsRequest{})
		setSessionCookie(t, db, req, userID)
		res, err := mypage.ListMyUnsettledTickets(ctx, req)
		if err != nil {
			t.Fatalf("ListMyUnsettledTickets に失敗: %v", err)
		}
		return ticketIDs(res.Msg.Tickets)
	}
	listReceivables := func(t *testing.T, userID string) []string {
		t.Helper()
		req := connect.NewRequest(&nazobuv1.ListMyUnsettledReceivablesRequest{})
		setSessionCookie(t, db, req, userID)
		res, err := mypage.ListMyUnsettledReceivables(ctx, req)
		if err != nil {
			t.Fatalf("ListMyUnsettledReceivables に失敗: %v", err)
		}
		return ticketIDs(res.Msg.Tickets)
	}
	monthlySettled := func(t *testing.T, userID string) bool {
		t.Helper()
		req := connect.NewRequest(&nazobuv1.ListMonthlyTicketsRequest{Year: 2026, Month: 7})
		setSessionCookie(t, db, req, userID)
		res, err := mypage.ListMonthlyTickets(ctx, req)
		if err != nil {
			t.Fatalf("ListMonthlyTickets に失敗: %v", err)
		}
		if len(res.Msg.Monthly) != 1 {
			t.Fatalf("月次履歴 = %d 件, want 1", len(res.Msg.Monthly))
		}
		return res.Msg.Monthly[0].Settled
	}

	// チケット代精算済みの状態では、未精算・未回収ともに空
	if got := listUnsettled(t, memberID); len(got) != 0 {
		t.Errorf("expense 登録前の member の未精算 = %v, want 空", got)
	}
	if got := listReceivables(t, adminID); len(got) != 0 {
		t.Errorf("expense 登録前の admin の未回収 = %v, want 空", got)
	}
	if !monthlySettled(t, memberID) {
		t.Error("expense 登録前の月次履歴が未精算になっている")
	}

	// 幹事が飲み会代を登録（member だけが未精算対象）
	createReq := connect.NewRequest(&nazobuv1.CreateTicketExpenseRequest{
		TicketId: ticketID,
		Title:    "打ち上げ飲み会",
		Participants: []*nazobuv1.TicketExpenseParticipantInput{
			{UserId: organizerID, Amount: 3000},
			{UserId: memberID, Amount: 4500},
		},
	})
	setSessionCookie(t, db, createReq, organizerID)
	if _, err := svc.CreateTicketExpense(ctx, createReq); err != nil {
		t.Fatalf("CreateTicketExpense に失敗: %v", err)
	}
	expenseID := mustGetTicket(t, db, ticketID, organizerID).Expenses[0].Id

	// member の未精算・幹事の未回収にチケットが出る。月次履歴の settled も落ちる。
	// チケット立替者（admin）の未回収と幹事自身の未精算には出ない。
	if got := listUnsettled(t, memberID); !equalIDs(got, []string{ticketID}) {
		t.Errorf("member の未精算 = %v, want [%s]", got, ticketID)
	}
	if got := listReceivables(t, organizerID); !equalIDs(got, []string{ticketID}) {
		t.Errorf("幹事の未回収 = %v, want [%s]", got, ticketID)
	}
	if got := listUnsettled(t, organizerID); len(got) != 0 {
		t.Errorf("幹事（expense 立替者）の未精算 = %v, want 空", got)
	}
	if got := listReceivables(t, adminID); len(got) != 0 {
		t.Errorf("admin の未回収 = %v, want 空", got)
	}
	if monthlySettled(t, memberID) {
		t.Error("expense 未精算なのに月次履歴が精算済みになっている")
	}

	// member が精算されると全リストから消え、月次履歴も精算済みに戻る
	settleReq := connect.NewRequest(&nazobuv1.UpdateTicketExpenseSettlementRequest{
		ExpenseId: expenseID,
		UserId:   memberID,
		Settled:  true,
	})
	setSessionCookie(t, db, settleReq, organizerID)
	if _, err := svc.UpdateTicketExpenseSettlement(ctx, settleReq); err != nil {
		t.Fatalf("UpdateTicketExpenseSettlement に失敗: %v", err)
	}
	if got := listUnsettled(t, memberID); len(got) != 0 {
		t.Errorf("精算後の member の未精算 = %v, want 空", got)
	}
	if got := listReceivables(t, organizerID); len(got) != 0 {
		t.Errorf("精算後の幹事の未回収 = %v, want 空", got)
	}
	if !monthlySettled(t, memberID) {
		t.Error("精算後の月次履歴が未精算のまま")
	}
}
