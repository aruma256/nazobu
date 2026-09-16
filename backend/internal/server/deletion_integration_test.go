package server

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/aruma256/nazobu/backend/internal/auth"
	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/aruma256/nazobu/backend/internal/testdb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestIntegrationDeleteTicketAndEvent(t *testing.T) {
	for _, asAdmin := range []bool{false, true} {
		t.Run(map[bool]string{false: "立替者", true: "管理者"}[asAdmin], func(t *testing.T) {
			db := testdb.Open(t)
			ctx := context.Background()
			admin := createTestUser(t, db, "管理者", auth.RoleAdmin)
			payer := createTestUser(t, db, "立替者", auth.RoleMember)
			other := createTestUser(t, db, "参加者", auth.RoleMember)
			event := createTestEvent(t, db, "誤登録公演")
			ticket := createTestTicket(t, db, event, payer)
			q := queries.New(db)
			for _, userID := range []string{payer, other} {
				if err := q.CreateTicketParticipant(ctx, queries.CreateTicketParticipantParams{TicketID: ticket, UserID: userID}); err != nil {
					t.Fatal(err)
				}
			}
			if err := q.MarkTicketParticipantSettled(ctx, queries.MarkTicketParticipantSettledParams{TicketID: ticket, UserID: other}); err != nil {
				t.Fatal(err)
			}
			expense := mustCreateExpense(t, ctx, newExpenseService(db), db, payer, &nazobuv1.CreateExpenseRequest{
				TicketId: ticket, Title: "残す追加精算", OccurredOn: "2026-09-16", Participants: []*nazobuv1.ExpenseParticipantInput{{UserId: other, Amount: 1000}},
			})
			manager := &fakeDiscordSpoilerChannelManager{configured: true}
			tickets := &ticketService{db: db, q: q, spoilerChannelManager: manager}
			events := newEventService(db)
			deleteTicket := func(userID, ticketID string) error {
				req := connect.NewRequest(&nazobuv1.DeleteTicketRequest{TicketId: ticketID})
				if userID != "" {
					setSessionCookie(t, db, req, userID)
				}
				_, err := tickets.DeleteTicket(ctx, req)
				return err
			}
			deleteEvent := func(userID, eventID string) error {
				req := connect.NewRequest(&nazobuv1.DeleteEventRequest{EventId: eventID})
				if userID != "" {
					setSessionCookie(t, db, req, userID)
				}
				_, err := events.DeleteEvent(ctx, req)
				return err
			}
			for _, tc := range []struct {
				err  error
				code connect.Code
			}{
				{deleteTicket("", ticket), connect.CodeUnauthenticated},
				{deleteEvent("", event), connect.CodeUnauthenticated},
				{deleteTicket(other, ticket), connect.CodePermissionDenied},
				{deleteEvent(payer, event), connect.CodePermissionDenied},
				{deleteTicket(payer, " "), connect.CodeInvalidArgument},
				{deleteEvent(admin, " "), connect.CodeInvalidArgument},
				{deleteTicket(payer, "missing"), connect.CodeNotFound},
				{deleteEvent(admin, "missing"), connect.CodeNotFound},
				{deleteEvent(admin, event), connect.CodeFailedPrecondition},
			} {
				if connectCode(t, tc.err) != tc.code {
					t.Fatalf("code = %v, want %v", tc.err, tc.code)
				}
			}
			// DB から直接削除しても、参加・精算記録を連鎖削除しない。
			if _, err := db.Exec("DELETE FROM tickets WHERE id = ?", ticket); err == nil {
				t.Fatal("参加者のあるチケットを直接削除できてしまった")
			}
			actor := payer
			if asAdmin {
				actor = admin
			}
			if err := deleteTicket(actor, ticket); err != nil {
				t.Fatal(err)
			}
			if connectCode(t, deleteTicket(actor, ticket)) != connect.CodeNotFound {
				t.Fatal("再削除は NotFound のはず")
			}
			var count int
			if err := db.QueryRow("SELECT COUNT(*) FROM ticket_participants WHERE ticket_id = ?", ticket).Scan(&count); err != nil || count != 0 {
				t.Fatalf("参加者が残存: %d, %v", count, err)
			}
			got := mustGetExpense(t, ctx, newExpenseService(db), db, expense.Id, payer)
			if got.Expense.TicketId != "" || len(got.Participants) != 1 || got.Participants[0].Amount != 1000 {
				t.Fatalf("追加精算が保持されていない: %v", got)
			}
			if _, err := q.GetEventByID(ctx, event); err != nil {
				t.Fatalf("公演が残っていない: %v", err)
			}
			if err := deleteEvent(admin, event); err != nil {
				t.Fatal(err)
			}
			if connectCode(t, deleteEvent(admin, event)) != connect.CodeNotFound {
				t.Fatal("再削除は NotFound のはず")
			}
			if len(manager.deletedIDs) != 0 {
				t.Fatal("Discord チャンネルを削除してしまった")
			}
		})
	}
}

func TestIntegrationMCPDeletion(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	owner, ticket, readToken := setupMCPTestData(t, db)
	row, err := queries.New(db).GetTicketByID(ctx, ticket)
	if err != nil {
		t.Fatal(err)
	}
	admin := createTestUser(t, db, "管理者", auth.RoleAdmin)
	outsider := createTestUser(t, db, "別メンバー", auth.RoleMember)
	server := newMCPTestServer(t, db)
	read := newMCPTestSession(t, server.URL, readToken)
	write := newMCPTestSession(t, server.URL, issueMCPAccessToken(t, db, owner, "read write"))
	adminSession := newMCPTestSession(t, server.URL, issueMCPAccessToken(t, db, admin, "read write"))
	otherSession := newMCPTestSession(t, server.URL, issueMCPAccessToken(t, db, outsider, "read write"))
	for _, tc := range []struct {
		session       *mcp.ClientSession
		name, key, id string
		fail          bool
	}{
		{read, "delete_ticket", "ticket_id", ticket, true},
		{read, "delete_event", "event_id", row.EventID, true},
		{otherSession, "delete_ticket", "ticket_id", ticket, true},
		{write, "delete_event", "event_id", row.EventID, true},
		{adminSession, "delete_event", "event_id", row.EventID, true},
		{write, "delete_ticket", "ticket_id", ticket, false},
		{adminSession, "delete_event", "event_id", row.EventID, false},
	} {
		res, err := tc.session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: map[string]any{tc.key: tc.id}})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError != tc.fail {
			t.Fatalf("%s: IsError=%v, want %v: %v", tc.name, res.IsError, tc.fail, res.Content)
		}
	}
}

func TestIntegrationDeleteRollback(t *testing.T) {
	for _, kind := range []string{"ticket", "expense"} {
		t.Run(kind, func(t *testing.T) {
			db := testdb.Open(t)
			ctx := context.Background()
			payer := createTestUser(t, db, "立替者", auth.RoleMember)
			other := createTestUser(t, db, "参加者", auth.RoleMember)
			var parentID string
			var deleteParent func() error
			if kind == "ticket" {
				parentID = createTestTicket(t, db, createTestEvent(t, db, "公演"), payer)
				if err := queries.New(db).CreateTicketParticipant(ctx, queries.CreateTicketParticipantParams{TicketID: parentID, UserID: other}); err != nil {
					t.Fatal(err)
				}
				req := connect.NewRequest(&nazobuv1.DeleteTicketRequest{TicketId: parentID})
				setSessionCookie(t, db, req, payer)
				deleteParent = func() error { _, err := newTicketService(db).DeleteTicket(ctx, req); return err }
			} else {
				parentID = mustCreateExpense(t, ctx, newExpenseService(db), db, payer, &nazobuv1.CreateExpenseRequest{Title: "追加精算", OccurredOn: "2026-09-16", Participants: []*nazobuv1.ExpenseParticipantInput{{UserId: other, Amount: 1000}}}).Id
				req := connect.NewRequest(&nazobuv1.DeleteExpenseRequest{ExpenseId: parentID})
				setSessionCookie(t, db, req, payer)
				deleteParent = func() error { _, err := newExpenseService(db).DeleteExpense(ctx, req); return err }
			}
			// 子行を削除した後、親行の削除だけを失敗させ、子行も復元されることを検証する。
			if _, err := db.Exec("CREATE TRIGGER reject_deletion BEFORE DELETE ON " + kind + "s FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = '削除失敗テスト'"); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := db.Exec("DROP TRIGGER reject_deletion"); err != nil {
					t.Error(err)
				}
			}()
			if connectCode(t, deleteParent()) != connect.CodeInternal {
				t.Fatal("削除失敗が返らなかった")
			}
			var count int
			if err := db.QueryRow("SELECT COUNT(*) FROM "+kind+"_participants WHERE "+kind+"_id = ?", parentID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("参加者のロールバック失敗: %d, %v", count, err)
			}
			if err := db.QueryRow("SELECT COUNT(*) FROM "+kind+"s WHERE id = ?", parentID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("親行が残っていない: %d, %v", count, err)
			}
		})
	}
}
