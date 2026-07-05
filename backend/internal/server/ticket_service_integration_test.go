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
