package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
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

func TestIntegrationJoinEventSpoilerChannel(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	q := queries.New(db)
	member := createTestUser(t, db, "参加記録のない一般ユーザー", auth.RoleMember)
	other := createTestUser(t, db, "別のユーザー", auth.RoleMember)
	createTestDiscordIdentity(t, db, other, "discord-other")
	event := createTestEvent(t, db, "過去の公演")
	manager := &fakeExistingChannelManager{fakeDiscordSpoilerChannelManager: fakeDiscordSpoilerChannelManager{configured: true}}
	svc := &eventService{db: db, q: q, spoilerChannelManager: manager}
	join := func(actor, eventID string) (*connect.Response[nazobuv1.JoinEventSpoilerChannelResponse], error) {
		req := connect.NewRequest(&nazobuv1.JoinEventSpoilerChannelRequest{EventId: eventID})
		if actor != "" {
			setSessionCookie(t, db, req, actor)
		}
		return svc.JoinEventSpoilerChannel(ctx, req)
	}
	for _, tt := range []struct {
		actor, eventID string
		code           connect.Code
	}{
		{"", event, connect.CodeUnauthenticated},
		{member, "  ", connect.CodeInvalidArgument},
		{member, "missing", connect.CodeNotFound},
		{member, event, connect.CodeFailedPrecondition},
	} {
		_, err := join(tt.actor, tt.eventID)
		assertConnectCode(t, err, tt.code)
	}
	if _, err := q.SetEventDiscordSpoilerChannelID(ctx, queries.SetEventDiscordSpoilerChannelIDParams{ID: event, DiscordSpoilerChannelID: sql.NullString{String: "456", Valid: true}}); err != nil {
		t.Fatal(err)
	}
	_, err := join(member, event)
	assertConnectCode(t, err, connect.CodeFailedPrecondition)
	createTestDiscordIdentity(t, db, member, "discord-self")
	manager.configured = false
	_, err = join(member, event)
	assertConnectCode(t, err, connect.CodeFailedPrecondition)
	manager.configured = true
	if len(manager.grantChannels) != 0 {
		t.Fatal("失敗時に権限を付与した")
	}
	// 本人だけに付与され、再実行可能。チケットを作成する必要もない。
	for range 2 {
		res, err := join(member, " "+event+" ")
		if err != nil || res.Msg.DiscordChannelUrl != manager.ChannelURL("456") {
			t.Fatalf("参加失敗: %v, %v", res, err)
		}
	}
	if !slices.Equal(manager.grantChannels, []string{"456", "456"}) {
		t.Fatalf("対象チャンネル: %v", manager.grantChannels)
	}
	for _, members := range manager.grantMembers {
		if !slices.Equal(members, []string{"discord-self"}) {
			t.Fatalf("本人以外への権限付与: %v", members)
		}
	}
	if len(manager.createNames) != 0 || len(manager.deletedIDs) != 0 {
		t.Fatal("チャンネルを作成・削除した")
	}
	listReq := connect.NewRequest(&nazobuv1.ListEventsRequest{})
	setSessionCookie(t, db, listReq, member)
	list, err := svc.ListEvents(ctx, listReq)
	if err != nil || len(list.Msg.Events) != 1 || !list.Msg.Events[0].HasSpoilerChannel {
		t.Fatalf("一覧にチャンネルが反映されていない: %v, %v", list, err)
	}
	manager.grantErr = errors.New("Discord API failure")
	_, err = join(member, event)
	assertConnectCode(t, err, connect.CodeUnavailable)
}
