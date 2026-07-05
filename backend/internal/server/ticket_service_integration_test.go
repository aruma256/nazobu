package server

// ticketService の統合テスト。実 MySQL（testdb パッケージ経由）に対して
// セッション認証込みで RPC ハンドラを呼び、DB 往復を含む挙動を検証する。
// TEST_DB_HOST 未設定の環境では skip される。

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"

	"connectrpc.com/connect"

	"github.com/aruma256/nazobu/backend/internal/auth"
	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/aruma256/nazobu/backend/internal/id"
	"github.com/aruma256/nazobu/backend/internal/testdb"
)

// createTestUser は user を作成して ID を返す。
func createTestUser(t *testing.T, db *sql.DB, displayName, role string) string {
	t.Helper()
	ctx := context.Background()
	q := queries.New(db)
	userID := id.New()
	if err := q.CreateUser(ctx, queries.CreateUserParams{
		ID:          userID,
		DisplayName: displayName,
	}); err != nil {
		t.Fatalf("user 作成に失敗: %v", err)
	}
	if role != auth.RoleMember {
		if err := q.UpdateUserRole(ctx, queries.UpdateUserRoleParams{Role: role, ID: userID}); err != nil {
			t.Fatalf("role 更新に失敗: %v", err)
		}
	}
	return userID
}

// createTestEvent は event を作成して ID を返す。
func createTestEvent(t *testing.T, db *sql.DB, title string) string {
	t.Helper()
	eventID := id.New()
	if err := queries.New(db).CreateEvent(context.Background(), queries.CreateEventParams{
		ID:                      eventID,
		Title:                   title,
		Url:                     "https://example.com/event",
		ExpectedDurationMinutes: 120,
	}); err != nil {
		t.Fatalf("event 作成に失敗: %v", err)
	}
	return eventID
}

// setSessionCookie は userID の session を DB に作り、その cookie を req に載せる。
func setSessionCookie[T any](t *testing.T, db *sql.DB, req *connect.Request[T], userID string) {
	t.Helper()
	token, err := auth.CreateSession(context.Background(), db, userID)
	if err != nil {
		t.Fatalf("session 作成に失敗: %v", err)
	}
	req.Header().Set("Cookie", auth.SessionCookieName+"="+token)
}

func connectCode(t *testing.T, err error) connect.Code {
	t.Helper()
	var cerr *connect.Error
	if !errors.As(err, &cerr) {
		t.Fatalf("connect.Error を期待したが %v (%T)", err, err)
	}
	return cerr.Code()
}

func TestIntegrationCreateAndGetTicket(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newTicketService(db)

	adminID := createTestUser(t, db, "admin-user", auth.RoleAdmin)
	memberID := createTestUser(t, db, "member-user", auth.RoleMember)
	eventID := createTestEvent(t, db, "テスト公演")

	// admin がグループチケットを登録する
	createReq := connect.NewRequest(&nazobuv1.CreateTicketRequest{
		EventId:            eventID,
		StartAt:            "2026-08-01T14:00:00+09:00",
		MeetingAt:          "2026-08-01T13:30:00+09:00",
		PricePerPerson:     3500,
		MaxParticipants:    2,
		MeetingPlace:       "会場前",
		ParticipantUserIds: []string{adminID, memberID},
	})
	setSessionCookie(t, db, createReq, adminID)
	createRes, err := svc.CreateTicket(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateTicket に失敗: %v", err)
	}
	ticketID := createRes.Msg.Ticket.Id
	if ticketID == "" {
		t.Fatal("作成された ticket の ID が空")
	}

	// 登録した内容が DB 経由でそのまま取れること（JST の日時表現含む）
	getReq := connect.NewRequest(&nazobuv1.GetTicketRequest{TicketId: ticketID})
	setSessionCookie(t, db, getReq, memberID)
	getRes, err := svc.GetTicket(ctx, getReq)
	if err != nil {
		t.Fatalf("GetTicket に失敗: %v", err)
	}
	ticket := getRes.Msg.Ticket
	if ticket.EventId != eventID {
		t.Errorf("EventId = %q, want %q", ticket.EventId, eventID)
	}
	if ticket.EventTitle != "テスト公演" {
		t.Errorf("EventTitle = %q, want %q", ticket.EventTitle, "テスト公演")
	}
	if ticket.StartAt != "2026-08-01T14:00:00+09:00" {
		t.Errorf("StartAt = %q, DB 往復で JST 表現が保たれていない", ticket.StartAt)
	}
	if ticket.PricePerPerson != 3500 {
		t.Errorf("PricePerPerson = %d, want 3500", ticket.PricePerPerson)
	}

	// 参加者 2 名が入っていて、立替者（admin）に印がつくこと
	participants := getRes.Msg.Participants
	if len(participants) != 2 {
		t.Fatalf("参加者数 = %d, want 2", len(participants))
	}
	names := []string{participants[0].Name, participants[1].Name}
	for _, want := range []string{"admin-user", "member-user"} {
		if !slices.Contains(names, want) {
			t.Errorf("参加者に %q がいない: %v", want, names)
		}
	}
	for _, p := range participants {
		if got, want := p.IsPurchaser, p.UserId == adminID; got != want {
			t.Errorf("IsPurchaser(%s) = %v, want %v", p.Name, got, want)
		}
	}

	// member（非立替者）には編集権限がないこと
	if getRes.Msg.CanEdit {
		t.Error("非 admin・非立替者の CanEdit が true になっている")
	}
}

func TestIntegrationCreateTicketAuthorization(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newTicketService(db)

	memberID := createTestUser(t, db, "member-user", auth.RoleMember)
	eventID := createTestEvent(t, db, "テスト公演")

	newCreateReq := func() *connect.Request[nazobuv1.CreateTicketRequest] {
		return connect.NewRequest(&nazobuv1.CreateTicketRequest{
			EventId:            eventID,
			StartAt:            "2026-08-01T14:00:00+09:00",
			PricePerPerson:     1000,
			MaxParticipants:    1,
			ParticipantUserIds: []string{memberID},
		})
	}

	t.Run("session cookie なしは Unauthenticated", func(t *testing.T) {
		_, err := svc.CreateTicket(ctx, newCreateReq())
		if got := connectCode(t, err); got != connect.CodeUnauthenticated {
			t.Errorf("code = %v, want %v", got, connect.CodeUnauthenticated)
		}
	})

	t.Run("member の登録は PermissionDenied", func(t *testing.T) {
		req := newCreateReq()
		setSessionCookie(t, db, req, memberID)
		_, err := svc.CreateTicket(ctx, req)
		if got := connectCode(t, err); got != connect.CodePermissionDenied {
			t.Errorf("code = %v, want %v", got, connect.CodePermissionDenied)
		}
	})
}

func TestIntegrationUpdateTicketMaxParticipantsLowerBound(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newTicketService(db)

	adminID := createTestUser(t, db, "admin-user", auth.RoleAdmin)
	memberID := createTestUser(t, db, "member-user", auth.RoleMember)
	eventID := createTestEvent(t, db, "テスト公演")

	createReq := connect.NewRequest(&nazobuv1.CreateTicketRequest{
		EventId:            eventID,
		StartAt:            "2026-08-01T14:00:00+09:00",
		PricePerPerson:     3000,
		MaxParticipants:    2,
		ParticipantUserIds: []string{adminID, memberID},
	})
	setSessionCookie(t, db, createReq, adminID)
	createRes, err := svc.CreateTicket(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateTicket に失敗: %v", err)
	}
	ticketID := createRes.Msg.Ticket.Id

	// 参加者が 2 名いる状態で max_participants を 1 に下げるのは
	// 現在の参加者数を DB で数えた上で弾かれること
	updateReq := connect.NewRequest(&nazobuv1.UpdateTicketRequest{
		TicketId:          ticketID,
		StartAt:           "2026-08-01T14:00:00+09:00",
		PricePerPerson:    3000,
		MaxParticipants:   1,
		PurchasedByUserId: adminID,
	})
	setSessionCookie(t, db, updateReq, adminID)
	_, err = svc.UpdateTicket(ctx, updateReq)
	if got := connectCode(t, err); got != connect.CodeFailedPrecondition {
		t.Errorf("code = %v, want %v", got, connect.CodeFailedPrecondition)
	}
}

func TestIntegrationTicketParticipantManagement(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newTicketService(db)

	adminID := createTestUser(t, db, "admin-user", auth.RoleAdmin)
	memberID := createTestUser(t, db, "member-user", auth.RoleMember)
	otherID := createTestUser(t, db, "other-user", auth.RoleMember)
	eventID := createTestEvent(t, db, "テスト公演")

	// max 2 のチケットを admin だけで作る（空きは 1）
	createReq := connect.NewRequest(&nazobuv1.CreateTicketRequest{
		EventId:            eventID,
		StartAt:            "2026-08-01T14:00:00+09:00",
		PricePerPerson:     2000,
		MaxParticipants:    2,
		ParticipantUserIds: []string{adminID},
	})
	setSessionCookie(t, db, createReq, adminID)
	createRes, err := svc.CreateTicket(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateTicket に失敗: %v", err)
	}
	ticketID := createRes.Msg.Ticket.Id

	addParticipants := func(t *testing.T, userIDs ...string) error {
		t.Helper()
		req := connect.NewRequest(&nazobuv1.AddTicketParticipantsRequest{
			TicketId: ticketID,
			UserIds:  userIDs,
		})
		setSessionCookie(t, db, req, adminID)
		_, err := svc.AddTicketParticipants(ctx, req)
		return err
	}
	participantCount := func(t *testing.T) int {
		t.Helper()
		req := connect.NewRequest(&nazobuv1.GetTicketRequest{TicketId: ticketID})
		setSessionCookie(t, db, req, adminID)
		res, err := svc.GetTicket(ctx, req)
		if err != nil {
			t.Fatalf("GetTicket に失敗: %v", err)
		}
		return len(res.Msg.Participants)
	}

	// 空きがあるうちは追加できる
	if err := addParticipants(t, memberID); err != nil {
		t.Fatalf("参加者追加に失敗: %v", err)
	}
	if got := participantCount(t); got != 2 {
		t.Fatalf("参加者数 = %d, want 2", got)
	}

	// 満席での追加は DB の現在数を数えた上で弾かれる
	if err := addParticipants(t, otherID); connectCode(t, err) != connect.CodeFailedPrecondition {
		t.Errorf("満席時の追加 code = %v, want %v", connectCode(t, err), connect.CodeFailedPrecondition)
	}

	// 参加済みユーザの再追加は冪等（満席でもエラーにならず、数も増えない）
	if err := addParticipants(t, memberID); err != nil {
		t.Fatalf("参加済みユーザの再追加がエラー: %v", err)
	}
	if got := participantCount(t); got != 2 {
		t.Errorf("再追加後の参加者数 = %d, want 2", got)
	}

	// 立替者本人は削除できない
	removeReq := connect.NewRequest(&nazobuv1.RemoveTicketParticipantRequest{
		TicketId: ticketID,
		UserId:   adminID,
	})
	setSessionCookie(t, db, removeReq, adminID)
	_, err = svc.RemoveTicketParticipant(ctx, removeReq)
	if got := connectCode(t, err); got != connect.CodeFailedPrecondition {
		t.Errorf("立替者削除 code = %v, want %v", got, connect.CodeFailedPrecondition)
	}

	// 立替者以外は削除でき、空いた枠に改めて追加できる
	removeReq = connect.NewRequest(&nazobuv1.RemoveTicketParticipantRequest{
		TicketId: ticketID,
		UserId:   memberID,
	})
	setSessionCookie(t, db, removeReq, adminID)
	if _, err := svc.RemoveTicketParticipant(ctx, removeReq); err != nil {
		t.Fatalf("参加者削除に失敗: %v", err)
	}
	if err := addParticipants(t, otherID); err != nil {
		t.Fatalf("削除後の追加に失敗: %v", err)
	}
	if got := participantCount(t); got != 2 {
		t.Errorf("入れ替え後の参加者数 = %d, want 2", got)
	}
}
