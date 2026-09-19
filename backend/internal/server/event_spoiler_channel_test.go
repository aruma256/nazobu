package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/aruma256/nazobu/backend/internal/auth"
	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/aruma256/nazobu/backend/internal/oauth"
	"github.com/aruma256/nazobu/backend/internal/testdb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestParseDiscordChannel(t *testing.T) {
	for _, raw := range []string{"123456789012345678", " 123456789012345678 ", "https://discord.com/channels/234567890123456789/123456789012345678/"} {
		channel, _, err := parseDiscordChannel(raw)
		if err != nil || channel != "123456789012345678" {
			t.Errorf("%q: %q, %v", raw, channel, err)
		}
	}
	for _, raw := range []string{"", "0", "-1", "+123", "0123", "18446744073709551616", "https://evil.test/channels/1/2", "https://discord.com/channels/@me/2", "https://discord.com/channels/1/2/3", "https://discord.com/channels/1/2?x=1", "https://discord.com/channels/1/%32"} {
		if _, _, err := parseDiscordChannel(raw); err == nil {
			t.Errorf("%q を受け付けた", raw)
		}
	}
}

type fakeExistingChannelManager struct {
	fakeDiscordSpoilerChannelManager
	validationErr  error
	beforeValidate func()
}

func (f *fakeExistingChannelManager) ChannelURL(id string) string {
	return "https://discord.com/channels/123/" + id
}
func (f *fakeExistingChannelManager) ValidateSpoilerChannel(context.Context, string) error {
	if f.beforeValidate != nil {
		f.beforeValidate()
	}
	return f.validationErr
}

func TestIntegrationLinkEventSpoilerChannel(t *testing.T) {
	db := testdb.Open(t)
	q := queries.New(db)
	ctx := context.Background()
	adminID := createTestUser(t, db, "admin", auth.RoleAdmin)
	memberID := createTestUser(t, db, "member", auth.RoleMember)
	eventID := createTestEvent(t, db, "既存公演")
	manager := &fakeExistingChannelManager{fakeDiscordSpoilerChannelManager: fakeDiscordSpoilerChannelManager{configured: true}}
	svc := &eventService{db: db, q: q, spoilerChannelManager: manager}
	link := func(actor, event, channel string) error {
		req := connect.NewRequest(&nazobuv1.LinkEventSpoilerChannelRequest{EventId: event, DiscordChannel: channel})
		if actor != "" {
			setSessionCookie(t, db, req, actor)
		}
		_, err := svc.LinkEventSpoilerChannel(ctx, req)
		return err
	}
	assertConnectCode(t, link("", eventID, "456"), connect.CodeUnauthenticated)
	assertConnectCode(t, link(memberID, eventID, "456"), connect.CodePermissionDenied)
	assertConnectCode(t, link(adminID, "", "456"), connect.CodeInvalidArgument)
	assertConnectCode(t, link(adminID, eventID, "bad"), connect.CodeInvalidArgument)
	assertConnectCode(t, link(adminID, "missing", "456"), connect.CodeNotFound)
	assertConnectCode(t, link(adminID, eventID, "https://discord.com/channels/999/456"), connect.CodeInvalidArgument)
	manager.validationErr = errors.New("チャンネル取得失敗")
	assertConnectCode(t, link(adminID, eventID, "456"), connect.CodeFailedPrecondition)
	row, err := q.GetEventByID(ctx, eventID)
	if err != nil || row.DiscordSpoilerChannelID.Valid {
		t.Fatalf("失敗時に保存された: %+v, %v", row, err)
	}
	manager.validationErr = nil
	for _, channel := range []string{"https://discord.com/channels/123/456", "456"} {
		if err := link(adminID, eventID, channel); err != nil {
			t.Fatal(err)
		}
	}
	assertConnectCode(t, link(adminID, eventID, "789"), connect.CodeAlreadyExists)
	// 過去公演にも紐づけ可能で、同じ公演の全チケットから参照される。
	tickets := newTicketServiceWithDiscord(db, manager)
	for range 2 {
		ticketID := createTestTicket(t, db, eventID, adminID)
		req := connect.NewRequest(&nazobuv1.GetTicketRequest{TicketId: ticketID})
		setSessionCookie(t, db, req, adminID)
		res, err := tickets.GetTicket(ctx, req)
		if err != nil || res.Msg.DiscordSpoilerChannelUrl != manager.ChannelURL("456") {
			t.Fatalf("チケットへの反映失敗: %v, %v", res, err)
		}
	}
	if len(manager.createNames)+len(manager.grantChannels)+len(manager.deletedIDs) != 0 {
		t.Fatal("紐づけ時に Discord を変更した")
	}
	// 新しいチケットで権限付与すると既存チャンネルを再利用する。
	createTestDiscordIdentity(t, db, adminID, "discord-admin")
	ticketID := createTestTicket(t, db, eventID, adminID)
	if _, err := db.ExecContext(ctx, "UPDATE tickets SET start_at = ? WHERE id = ?", time.Date(2026, 9, 1, 0, 0, 0, 0, jst), ticketID); err != nil {
		t.Fatal(err)
	}
	if err := q.CreateTicketParticipant(ctx, queries.CreateTicketParticipantParams{TicketID: ticketID, UserID: adminID}); err != nil {
		t.Fatal(err)
	}
	grant := connect.NewRequest(&nazobuv1.GrantTicketSpoilerChannelAccessRequest{TicketId: ticketID})
	setSessionCookie(t, db, grant, adminID)
	res, err := tickets.GrantTicketSpoilerChannelAccess(ctx, grant)
	if err != nil || res.Msg.ChannelCreated || len(manager.grantChannels) != 1 || manager.grantChannels[0] != "456" {
		t.Fatalf("既存チャンネルの再利用失敗: %v, %v", res, err)
	}
	// 検証中に別の処理が紐づけても上書きしない。
	anotherEvent := createTestEvent(t, db, "競合公演")
	manager.beforeValidate = func() {
		if _, err := db.ExecContext(ctx, "UPDATE events SET discord_spoiler_channel_id = '789' WHERE id = ?", anotherEvent); err != nil {
			t.Fatal(err)
		}
	}
	assertConnectCode(t, link(adminID, anotherEvent, "456"), connect.CodeAborted)
	row, err = q.GetEventByID(ctx, anotherEvent)
	if err != nil || row.DiscordSpoilerChannelID.String != "789" {
		t.Fatalf("競合時に上書きした: %+v, %v", row, err)
	}
}

func TestIntegrationMCPLinkEventSpoilerChannel(t *testing.T) {
	db := testdb.Open(t)
	adminID := createTestUser(t, db, "admin", auth.RoleAdmin)
	memberID := createTestUser(t, db, "member", auth.RoleMember)
	eventID := createTestEvent(t, db, "MCP 公演")
	manager := &fakeExistingChannelManager{fakeDiscordSpoilerChannelManager: fakeDiscordSpoilerChannelManager{configured: true}}
	events := &eventService{db: db, q: queries.New(db), spoilerChannelManager: manager}
	oauthSrv := oauth.NewServer(db, http.DefaultClient, "https://nazobu.example.com", false)
	ts := httptest.NewServer(oauthSrv.Middleware(newMCPHandler(newMyPageService(db), newTicketService(db), events, newUserService(db))))
	defer ts.Close()
	for _, tt := range []struct {
		actor, scope string
		wantError    bool
	}{{adminID, "read", true}, {memberID, "read write", true}, {adminID, "read write", false}} {
		session := newMCPTestSession(t, ts.URL, issueMCPAccessToken(t, db, tt.actor, tt.scope))
		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "link_event_spoiler_channel", Arguments: map[string]any{"event_id": eventID, "discord_channel": "456"}})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError != tt.wantError {
			t.Fatalf("scope=%s error=%v: %+v", tt.scope, res.IsError, res)
		}
		if !tt.wantError {
			var out linkEventSpoilerChannelOutput
			unmarshalStructuredContent(t, res, &out)
			if out.DiscordChannelURL != manager.ChannelURL("456") {
				t.Fatalf("URL=%q", out.DiscordChannelURL)
			}
		}
	}
}
