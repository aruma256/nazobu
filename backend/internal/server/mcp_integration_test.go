package server

// MCP エンドポイントの統合テスト。実 MySQL + go-sdk のクライアントで、
// Bearer 認証 → ツール呼び出し → 既存 RPC の in-process 再利用までを一気通貫で検証する。
// OAuth フロー（authorize / token）自体は internal/oauth の統合テストで担保する。

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aruma256/nazobu/backend/internal/auth"
	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1/nazobuv1connect"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/aruma256/nazobu/backend/internal/id"
	"github.com/aruma256/nazobu/backend/internal/oauth"
	"github.com/aruma256/nazobu/backend/internal/testdb"
)

// bearerTransport は全リクエストに Authorization: Bearer を付ける RoundTripper。
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(r)
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// setupMCPTestData は user / event / ticket / 参加者と有効なアクセストークンを仕込む。
func setupMCPTestData(t *testing.T, db *sql.DB) (userID, ticketID, accessToken string) {
	t.Helper()
	ctx := context.Background()
	q := queries.New(db)

	userID = id.New()
	if err := q.CreateUser(ctx, queries.CreateUserParams{ID: userID, DisplayName: "mcp-test-user"}); err != nil {
		t.Fatalf("user 作成に失敗: %v", err)
	}
	eventID := id.New()
	if err := q.CreateEvent(ctx, queries.CreateEventParams{
		ID:                         eventID,
		Title:                      "テスト脱出公演",
		Url:                        "https://example.com/event",
		Catchphrase:                "きみは謎を解けるか",
		ExpectedDurationMinutes:    120,
		DoorsOpenMinutesBefore:     sql.NullInt32{Int32: 15, Valid: true},
		EntryDeadlineMinutesBefore: sql.NullInt32{Int32: 5, Valid: true},
	}); err != nil {
		t.Fatalf("event 作成に失敗: %v", err)
	}
	ticketID = id.New()
	if err := q.CreateTicket(ctx, queries.CreateTicketParams{
		ID:              ticketID,
		EventID:         eventID,
		StartAt:         time.Now().Add(48 * time.Hour),
		PricePerPerson:  4500,
		MaxParticipants: 4,
		PurchasedBy:     userID,
		MeetingPlace:    "渋谷駅ハチ公前",
	}); err != nil {
		t.Fatalf("ticket 作成に失敗: %v", err)
	}
	if err := q.CreateTicketParticipant(ctx, queries.CreateTicketParticipantParams{
		TicketID: ticketID,
		UserID:   userID,
	}); err != nil {
		t.Fatalf("参加者作成に失敗: %v", err)
	}

	accessToken = issueMCPAccessToken(t, db, userID, "read")
	return userID, ticketID, accessToken
}

// issueMCPAccessToken は指定 scope のアクセストークンを DB に直接発行して raw 値を返す
// （OAuth フローは oauth 側のテストで担保）。
func issueMCPAccessToken(t *testing.T, db *sql.DB, userID, scope string) string {
	t.Helper()
	accessToken := "test-mcp-access-token-" + id.New()
	if err := queries.New(db).CreateOAuthToken(context.Background(), queries.CreateOAuthTokenParams{
		ID:                    id.New(),
		UserID:                userID,
		ClientID:              "https://claude.ai/test-client-metadata",
		Scope:                 scope,
		AccessTokenHash:       sha256Hex(accessToken),
		AccessTokenExpiresAt:  time.Now().Add(time.Hour),
		RefreshTokenHash:      sha256Hex(accessToken + "-refresh"),
		RefreshTokenExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("アクセストークン発行に失敗: %v", err)
	}
	return accessToken
}

func newMCPTestServer(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()
	oauthSrv := oauth.NewServer(db, http.DefaultClient, "https://nazobu.example.com", false)
	handler := newMCPHandler(newMyPageService(db), newTicketService(db), newEventService(db), newUserService(db), newExpenseService(db))
	ts := httptest.NewServer(oauthSrv.Middleware(handler))
	t.Cleanup(ts.Close)
	return ts
}

// newMCPTestSession は Bearer トークン付きで MCP セッションを確立する。
func newMCPTestSession(t *testing.T, endpoint, accessToken string) *mcp.ClientSession {
	t.Helper()
	transport := &mcp.StreamableClientTransport{
		Endpoint: endpoint,
		HTTPClient: &http.Client{
			Transport: &bearerTransport{token: accessToken, base: http.DefaultTransport},
		},
		// stateless サーバに対する GET（SSE ストリーム確立）は不要なので張らない。
		DisableStandaloneSSE: true,
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("MCP 接続に失敗: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// unmarshalStructuredContent は CallTool の StructuredContent を out に詰め直す。
func unmarshalStructuredContent(t *testing.T, res *mcp.CallToolResult, out any) {
	t.Helper()
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("StructuredContent の marshal に失敗: %v", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("StructuredContent の unmarshal に失敗: %v", err)
	}
}

func TestIntegrationMCPListMyUpcomingTickets(t *testing.T) {
	db := testdb.Open(t)
	_, _, accessToken := setupMCPTestData(t, db)
	ts := newMCPTestServer(t, db)
	ctx := context.Background()
	session := newMCPTestSession(t, ts.URL, accessToken)

	// ツールが公開されていること
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools に失敗: %v", err)
	}
	published := map[string]bool{}
	for _, tool := range tools.Tools {
		published[tool.Name] = true
	}
	for _, name := range []string{"list_my_upcoming_tickets", "list_tickets", "get_ticket", "list_users", "create_ticket_with_event", "update_ticket_with_event"} {
		if !published[name] {
			t.Fatalf("%s が公開されていない: %v", name, tools.Tools)
		}
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_my_upcoming_tickets",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool に失敗: %v", err)
	}
	if res.IsError {
		t.Fatalf("ツールがエラーを返した: %+v", res.Content)
	}

	var out struct {
		Tickets []struct {
			EventTitle       string   `json:"event_title"`
			PricePerPerson   int32    `json:"price_per_person"`
			MeetingPlace     string   `json:"meeting_place"`
			PurchaserName    string   `json:"purchaser_name"`
			ParticipantNames []string `json:"participant_names"`
		} `json:"tickets"`
	}
	unmarshalStructuredContent(t, res, &out)
	if len(out.Tickets) != 1 {
		t.Fatalf("tickets = %d 件, want 1 件: %+v", len(out.Tickets), out.Tickets)
	}
	got := out.Tickets[0]
	if got.EventTitle != "テスト脱出公演" {
		t.Errorf("event_title = %q", got.EventTitle)
	}
	if got.PricePerPerson != 4500 {
		t.Errorf("price_per_person = %d", got.PricePerPerson)
	}
	if got.MeetingPlace != "渋谷駅ハチ公前" {
		t.Errorf("meeting_place = %q", got.MeetingPlace)
	}
	if got.PurchaserName != "mcp-test-user" {
		t.Errorf("purchaser_name = %q", got.PurchaserName)
	}
	if len(got.ParticipantNames) != 1 || got.ParticipantNames[0] != "mcp-test-user" {
		t.Errorf("participant_names = %v", got.ParticipantNames)
	}
}

func TestIntegrationMCPListTickets(t *testing.T) {
	db := testdb.Open(t)
	_, ticketID, accessToken := setupMCPTestData(t, db)
	ts := newMCPTestServer(t, db)

	session := newMCPTestSession(t, ts.URL, accessToken)
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "list_tickets",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool に失敗: %v", err)
	}
	if res.IsError {
		t.Fatalf("ツールがエラーを返した: %+v", res.Content)
	}

	var out struct {
		Tickets []struct {
			TicketID         string   `json:"ticket_id"`
			EventTitle       string   `json:"event_title"`
			PricePerPerson   int32    `json:"price_per_person"`
			PurchaserName    string   `json:"purchaser_name"`
			ParticipantNames []string `json:"participant_names"`
		} `json:"tickets"`
	}
	unmarshalStructuredContent(t, res, &out)
	if len(out.Tickets) != 1 {
		t.Fatalf("tickets = %d 件, want 1 件: %+v", len(out.Tickets), out.Tickets)
	}
	got := out.Tickets[0]
	if got.TicketID != ticketID {
		t.Errorf("ticket_id = %q, want %q", got.TicketID, ticketID)
	}
	if got.EventTitle != "テスト脱出公演" {
		t.Errorf("event_title = %q", got.EventTitle)
	}
	if got.PricePerPerson != 4500 {
		t.Errorf("price_per_person = %d", got.PricePerPerson)
	}
	if got.PurchaserName != "mcp-test-user" {
		t.Errorf("purchaser_name = %q", got.PurchaserName)
	}
	if len(got.ParticipantNames) != 1 || got.ParticipantNames[0] != "mcp-test-user" {
		t.Errorf("participant_names = %v", got.ParticipantNames)
	}
}

func TestIntegrationMCPGetTicket(t *testing.T) {
	db := testdb.Open(t)
	userID, ticketID, accessToken := setupMCPTestData(t, db)
	ts := newMCPTestServer(t, db)
	ctx := context.Background()
	session := newMCPTestSession(t, ts.URL, accessToken)

	t.Run("参加者の精算状況を含む詳細が取得できる", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_ticket",
			Arguments: map[string]any{"ticket_id": ticketID},
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if res.IsError {
			t.Fatalf("ツールがエラーを返した: %+v", res.Content)
		}

		var out struct {
			Ticket struct {
				TicketID     string `json:"ticket_id"`
				EventTitle   string `json:"event_title"`
				MeetingPlace string `json:"meeting_place"`
			} `json:"ticket"`
			MaxParticipants int32 `json:"max_participants"`
			Participants    []struct {
				UserID      string `json:"user_id"`
				Name        string `json:"name"`
				Settled     bool   `json:"settled"`
				IsPurchaser bool   `json:"is_purchaser"`
			} `json:"participants"`
		}
		unmarshalStructuredContent(t, res, &out)
		if out.Ticket.TicketID != ticketID {
			t.Errorf("ticket_id = %q, want %q", out.Ticket.TicketID, ticketID)
		}
		if out.Ticket.EventTitle != "テスト脱出公演" {
			t.Errorf("event_title = %q", out.Ticket.EventTitle)
		}
		if out.Ticket.MeetingPlace != "渋谷駅ハチ公前" {
			t.Errorf("meeting_place = %q", out.Ticket.MeetingPlace)
		}
		if out.MaxParticipants != 4 {
			t.Errorf("max_participants = %d", out.MaxParticipants)
		}
		if len(out.Participants) != 1 {
			t.Fatalf("participants = %d 件, want 1 件: %+v", len(out.Participants), out.Participants)
		}
		p := out.Participants[0]
		if p.UserID != userID || p.Name != "mcp-test-user" {
			t.Errorf("participants[0] = %+v", p)
		}
		// 立替者本人は常に精算済み扱い
		if !p.IsPurchaser || !p.Settled {
			t.Errorf("is_purchaser = %v, settled = %v, want どちらも true", p.IsPurchaser, p.Settled)
		}
	})

	t.Run("存在しない ticket_id はエラー", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_ticket",
			Arguments: map[string]any{"ticket_id": id.New()},
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if !res.IsError {
			t.Fatal("存在しない ticket_id で成功してしまった")
		}
	})
}

func TestIntegrationMCPListUsers(t *testing.T) {
	db := testdb.Open(t)
	userID, _, accessToken := setupMCPTestData(t, db)
	ts := newMCPTestServer(t, db)

	session := newMCPTestSession(t, ts.URL, accessToken)
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "list_users",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool に失敗: %v", err)
	}
	if res.IsError {
		t.Fatalf("ツールがエラーを返した: %+v", res.Content)
	}

	var out struct {
		Users []struct {
			UserID      string `json:"user_id"`
			DisplayName string `json:"display_name"`
		} `json:"users"`
	}
	unmarshalStructuredContent(t, res, &out)
	if len(out.Users) != 1 {
		t.Fatalf("users = %d 件, want 1 件: %+v", len(out.Users), out.Users)
	}
	if out.Users[0].UserID != userID || out.Users[0].DisplayName != "mcp-test-user" {
		t.Errorf("users[0] = %+v", out.Users[0])
	}
}

func TestIntegrationMCPCreateTicketWithEvent(t *testing.T) {
	db := testdb.Open(t)
	adminID := createTestUser(t, db, "mcp-admin", auth.RoleAdmin)
	memberID := createTestUser(t, db, "mcp-member", auth.RoleMember)
	ts := newMCPTestServer(t, db)
	ctx := context.Background()

	// event_url の host は OG 取得の allowlist 外なので、テスト中に外部アクセスは発生しない。
	createArgs := func() map[string]any {
		return map[string]any{
			"event_title":                     "新作脱出公演",
			"event_url":                       "https://example.com/new-event",
			"event_expected_duration_minutes": 90,
			"start_at":                        "2026-09-01T14:00:00+09:00",
			"meeting_at":                      "2026-09-01T13:30:00+09:00",
			"meeting_place":                   "新宿駅東口",
			"price_per_person":                3800,
			"max_participants":                4,
			"unregistered_participants_count": 1,
			"participant_user_ids":            []string{adminID, memberID},
		}
	}

	t.Run("write スコープの admin は event と ticket を同時登録できる", func(t *testing.T) {
		session := newMCPTestSession(t, ts.URL, issueMCPAccessToken(t, db, adminID, "read write"))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "create_ticket_with_event",
			Arguments: createArgs(),
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if res.IsError {
			t.Fatalf("ツールがエラーを返した: %+v", res.Content)
		}

		var out struct {
			EventID string `json:"event_id"`
			Ticket  struct {
				TicketID         string   `json:"ticket_id"`
				EventTitle       string   `json:"event_title"`
				StartAt          string   `json:"start_at"`
				MeetingPlace     string   `json:"meeting_place"`
				PricePerPerson   int32    `json:"price_per_person"`
				PurchaserName    string   `json:"purchaser_name"`
				ParticipantNames []string `json:"participant_names"`
			} `json:"ticket"`
			MaxParticipants int32 `json:"max_participants"`
		}
		unmarshalStructuredContent(t, res, &out)
		if out.EventID == "" || out.Ticket.TicketID == "" {
			t.Fatalf("event_id / ticket_id が空: %+v", out)
		}
		if out.Ticket.EventTitle != "新作脱出公演" {
			t.Errorf("event_title = %q", out.Ticket.EventTitle)
		}
		if out.Ticket.StartAt != "2026-09-01T14:00:00+09:00" {
			t.Errorf("start_at = %q", out.Ticket.StartAt)
		}
		if out.Ticket.PricePerPerson != 3800 {
			t.Errorf("price_per_person = %d", out.Ticket.PricePerPerson)
		}
		// 立替者は Bearer トークンのユーザー自身になること
		if out.Ticket.PurchaserName != "mcp-admin" {
			t.Errorf("purchaser_name = %q", out.Ticket.PurchaserName)
		}
		if len(out.Ticket.ParticipantNames) != 2 {
			t.Errorf("participant_names = %v", out.Ticket.ParticipantNames)
		}
		if out.MaxParticipants != 4 {
			t.Errorf("max_participants = %d", out.MaxParticipants)
		}

		// DB にも登録されていること
		q := queries.New(db)
		if _, err := q.GetEventByID(ctx, out.EventID); err != nil {
			t.Errorf("登録後の event が取得できない: %v", err)
		}
		if _, err := q.GetTicketByID(ctx, out.Ticket.TicketID); err != nil {
			t.Errorf("登録後の ticket が取得できない: %v", err)
		}
	})

	t.Run("read のみのトークンでは write スコープ不足でエラー", func(t *testing.T) {
		session := newMCPTestSession(t, ts.URL, issueMCPAccessToken(t, db, adminID, "read"))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "create_ticket_with_event",
			Arguments: createArgs(),
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if !res.IsError {
			t.Fatal("write スコープ無しで成功してしまった")
		}
	})

	t.Run("member ロールでは権限エラー", func(t *testing.T) {
		session := newMCPTestSession(t, ts.URL, issueMCPAccessToken(t, db, memberID, "read write"))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "create_ticket_with_event",
			Arguments: createArgs(),
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if !res.IsError {
			t.Fatal("member ロールで成功してしまった")
		}
	})
}

func TestIntegrationMCPUpdateTicketWithEvent(t *testing.T) {
	db := testdb.Open(t)
	userID, ticketID, readOnlyToken := setupMCPTestData(t, db)
	ts := newMCPTestServer(t, db)
	ctx := context.Background()
	q := queries.New(db)

	eventRow, err := q.GetTicketByID(ctx, ticketID)
	if err != nil {
		t.Fatalf("ticket の取得に失敗: %v", err)
	}
	eventID := eventRow.EventID

	session := newMCPTestSession(t, ts.URL, issueMCPAccessToken(t, db, userID, "read write"))

	t.Run("指定したフィールドだけ更新され、省略したフィールドは維持される", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "update_ticket_with_event",
			Arguments: map[string]any{
				"ticket_id":        ticketID,
				"meeting_place":    "池袋駅東口",
				"price_per_person": 5000,
			},
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if res.IsError {
			t.Fatalf("ツールがエラーを返した: %+v", res.Content)
		}

		var out struct {
			Ticket struct {
				EventTitle     string `json:"event_title"`
				MeetingPlace   string `json:"meeting_place"`
				PricePerPerson int32  `json:"price_per_person"`
				PurchaserName  string `json:"purchaser_name"`
			} `json:"ticket"`
			MaxParticipants int32 `json:"max_participants"`
		}
		unmarshalStructuredContent(t, res, &out)
		if out.Ticket.MeetingPlace != "池袋駅東口" {
			t.Errorf("meeting_place = %q", out.Ticket.MeetingPlace)
		}
		if out.Ticket.PricePerPerson != 5000 {
			t.Errorf("price_per_person = %d", out.Ticket.PricePerPerson)
		}
		// 省略したフィールドが維持されていること
		if out.Ticket.EventTitle != "テスト脱出公演" {
			t.Errorf("event_title が変わってしまった: %q", out.Ticket.EventTitle)
		}
		if out.Ticket.PurchaserName != "mcp-test-user" {
			t.Errorf("purchaser_name が変わってしまった: %q", out.Ticket.PurchaserName)
		}
		if out.MaxParticipants != 4 {
			t.Errorf("max_participants が変わってしまった: %d", out.MaxParticipants)
		}
		// Ticket メッセージに乗らない event フィールドも維持されていること（DB で確認）
		ev, err := q.GetEventByID(ctx, eventID)
		if err != nil {
			t.Fatalf("event の取得に失敗: %v", err)
		}
		if ev.Catchphrase != "きみは謎を解けるか" {
			t.Errorf("catchphrase が変わってしまった: %q", ev.Catchphrase)
		}
		if !ev.DoorsOpenMinutesBefore.Valid || ev.DoorsOpenMinutesBefore.Int32 != 15 {
			t.Errorf("doors_open_minutes_before が変わってしまった: %+v", ev.DoorsOpenMinutesBefore)
		}
		if !ev.EntryDeadlineMinutesBefore.Valid || ev.EntryDeadlineMinutesBefore.Int32 != 5 {
			t.Errorf("entry_deadline_minutes_before が変わってしまった: %+v", ev.EntryDeadlineMinutesBefore)
		}
	})

	t.Run("空文字と -1 で未設定に戻せる", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "update_ticket_with_event",
			Arguments: map[string]any{
				"ticket_id":                           ticketID,
				"meeting_place":                       "",
				"event_entry_deadline_minutes_before": -1,
			},
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if res.IsError {
			t.Fatalf("ツールがエラーを返した: %+v", res.Content)
		}

		var out struct {
			Ticket struct {
				MeetingPlace string `json:"meeting_place"`
			} `json:"ticket"`
		}
		unmarshalStructuredContent(t, res, &out)
		if out.Ticket.MeetingPlace != "" {
			t.Errorf("meeting_place = %q, want 空", out.Ticket.MeetingPlace)
		}
		ev, err := q.GetEventByID(ctx, eventID)
		if err != nil {
			t.Fatalf("event の取得に失敗: %v", err)
		}
		if ev.EntryDeadlineMinutesBefore.Valid {
			t.Errorf("entry_deadline_minutes_before が未設定に戻っていない: %+v", ev.EntryDeadlineMinutesBefore)
		}
	})

	t.Run("read のみのトークンでは write スコープ不足でエラー", func(t *testing.T) {
		readSession := newMCPTestSession(t, ts.URL, readOnlyToken)
		res, err := readSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "update_ticket_with_event",
			Arguments: map[string]any{"ticket_id": ticketID, "meeting_place": "新宿"},
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if !res.IsError {
			t.Fatal("write スコープ無しで成功してしまった")
		}
	})

	t.Run("立替者でも admin でもない member はエラー", func(t *testing.T) {
		otherID := createTestUser(t, db, "mcp-other", auth.RoleMember)
		otherSession := newMCPTestSession(t, ts.URL, issueMCPAccessToken(t, db, otherID, "read write"))
		res, err := otherSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "update_ticket_with_event",
			Arguments: map[string]any{"ticket_id": ticketID, "meeting_place": "新宿"},
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if !res.IsError {
			t.Fatal("編集権限の無い member で成功してしまった")
		}
	})
}

func TestIntegrationMCPUnauthorized(t *testing.T) {
	db := testdb.Open(t)
	ts := newMCPTestServer(t, db)

	// トークン無しの素の POST は 401 + WWW-Authenticate が返ること
	// （Claude はこのハンドシェイクから OAuth フローを開始する）
	resp, err := http.Post(ts.URL, "application/json", nil)
	if err != nil {
		t.Fatalf("POST に失敗: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); got == "" {
		t.Error("WWW-Authenticate ヘッダが無い")
	}
}

// -----------------------------------------------------------------------------
// expense 系ツールの統合テスト。
// フィクスチャ（既存 expense）の作成には expense_service_integration_test.go の
// ヘルパー（mustCreateExpense / createTestTicket）を再利用し、
// 検証は MCP ツール越し（list_expenses / get_expense / create_expense / update_expense /
// update_expense_participant_settlement）で行う。
// -----------------------------------------------------------------------------

// mcpResultText は CallTool 結果の Content を文字列化する（エラーメッセージの検証用）。
func mcpResultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	raw, err := json.Marshal(res.Content)
	if err != nil {
		t.Fatalf("Content の marshal に失敗: %v", err)
	}
	return string(raw)
}

// mustSettleExpenseParticipant は sessionUserID のセッションで参加者の精算状態を切り替える（成功前提）。
func mustSettleExpenseParticipant(t *testing.T, ctx context.Context, svc nazobuv1connect.ExpenseServiceHandler, db *sql.DB, expenseID, userID, sessionUserID string, settled bool) {
	t.Helper()
	req := connect.NewRequest(&nazobuv1.UpdateExpenseParticipantSettlementRequest{
		ExpenseId: expenseID, UserId: userID, Settled: settled,
	})
	setSessionCookie(t, db, req, sessionUserID)
	if _, err := svc.UpdateExpenseParticipantSettlement(ctx, req); err != nil {
		t.Fatalf("精算状態の更新に失敗: %v", err)
	}
}

func TestIntegrationMCPListExpenses(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newExpenseService(db)

	payerID := createTestUser(t, db, "mcp-expense-payer", auth.RoleMember)
	friendAID := createTestUser(t, db, "mcp-friend-a", auth.RoleMember)
	friendBID := createTestUser(t, db, "mcp-friend-b", auth.RoleMember)
	eventID := createTestEvent(t, db, "テスト公演")
	ticketID := createTestTicket(t, db, eventID, payerID)

	// occurred_on が異なる 3 件を、作成順と降順が一致しないように作る。
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
	mustSettleExpenseParticipant(t, ctx, svc, db, middle.Id, friendAID, payerID, true)

	ts := newMCPTestServer(t, db)
	session := newMCPTestSession(t, ts.URL, issueMCPAccessToken(t, db, payerID, "read"))

	// expense 系ツールが公開されていること。
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools に失敗: %v", err)
	}
	published := map[string]bool{}
	for _, tool := range tools.Tools {
		published[tool.Name] = true
	}
	for _, name := range []string{"list_expenses", "get_expense", "create_expense", "update_expense", "update_expense_participant_settlement"} {
		if !published[name] {
			t.Fatalf("%s が公開されていない: %v", name, tools.Tools)
		}
	}

	type expenseOut struct {
		ExpenseID        string   `json:"expense_id"`
		Title            string   `json:"title"`
		OccurredOn       string   `json:"occurred_on"`
		TicketID         string   `json:"ticket_id"`
		EventTitle       string   `json:"event_title"`
		PayerName        string   `json:"payer_name"`
		TotalAmount      int32    `json:"total_amount"`
		ParticipantCount int32    `json:"participant_count"`
		SettledCount     int32    `json:"settled_count"`
		ParticipantNames []string `json:"participant_names"`
	}

	t.Run("ticket_id 未指定は全件を occurred_on 降順で返す", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "list_expenses",
			Arguments: map[string]any{},
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if res.IsError {
			t.Fatalf("ツールがエラーを返した: %+v", res.Content)
		}
		var out struct {
			Expenses []expenseOut `json:"expenses"`
		}
		unmarshalStructuredContent(t, res, &out)

		gotOrder := make([]string, 0, len(out.Expenses))
		for _, e := range out.Expenses {
			gotOrder = append(gotOrder, e.ExpenseID)
		}
		wantOrder := []string{newest.Id, middle.Id, older.Id}
		if len(gotOrder) != len(wantOrder) {
			t.Fatalf("expenses = %d 件, want %d 件: %+v", len(gotOrder), len(wantOrder), out.Expenses)
		}
		for i := range wantOrder {
			if gotOrder[i] != wantOrder[i] {
				t.Fatalf("並び順 = %v, want %v（occurred_on 降順）", gotOrder, wantOrder)
			}
		}

		// middle の集計（合計金額 / 精算済み数 / 参加者名 / 公演名）を検証する。
		var mid *expenseOut
		for i := range out.Expenses {
			if out.Expenses[i].ExpenseID == middle.Id {
				mid = &out.Expenses[i]
			}
		}
		if mid == nil {
			t.Fatal("一覧に middle がいない")
		}
		if mid.TotalAmount != 3000 {
			t.Errorf("total_amount = %d, want 3000", mid.TotalAmount)
		}
		if mid.ParticipantCount != 2 {
			t.Errorf("participant_count = %d, want 2", mid.ParticipantCount)
		}
		if mid.SettledCount != 1 {
			t.Errorf("settled_count = %d, want 1", mid.SettledCount)
		}
		if mid.TicketID != ticketID {
			t.Errorf("ticket_id = %q, want %q", mid.TicketID, ticketID)
		}
		if mid.EventTitle != "テスト公演" {
			t.Errorf("event_title = %q, want テスト公演", mid.EventTitle)
		}
		if mid.PayerName != "mcp-expense-payer" {
			t.Errorf("payer_name = %q, want mcp-expense-payer", mid.PayerName)
		}
		for _, want := range []string{"mcp-friend-a", "mcp-friend-b"} {
			found := false
			for _, name := range mid.ParticipantNames {
				if name == want {
					found = true
				}
			}
			if !found {
				t.Errorf("participant_names に %q がいない: %v", want, mid.ParticipantNames)
			}
		}
	})

	t.Run("ticket_id 指定はその ticket に紐づく expense のみ返す", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "list_expenses",
			Arguments: map[string]any{"ticket_id": ticketID},
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if res.IsError {
			t.Fatalf("ツールがエラーを返した: %+v", res.Content)
		}
		var out struct {
			Expenses []expenseOut `json:"expenses"`
		}
		unmarshalStructuredContent(t, res, &out)
		if len(out.Expenses) != 1 || out.Expenses[0].ExpenseID != middle.Id {
			t.Fatalf("ticket 絞り込み結果 = %+v, middle のみのはず", out.Expenses)
		}
	})
}

func TestIntegrationMCPGetExpense(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newExpenseService(db)

	payerID := createTestUser(t, db, "mcp-expense-payer", auth.RoleMember)
	friendAID := createTestUser(t, db, "mcp-friend-a", auth.RoleMember)
	friendBID := createTestUser(t, db, "mcp-friend-b", auth.RoleMember)

	created := mustCreateExpense(t, ctx, svc, db, payerID, &nazobuv1.CreateExpenseRequest{
		Title: "打ち上げ", OccurredOn: "2026-07-20",
		Participants: []*nazobuv1.ExpenseParticipantInput{
			{UserId: friendAID, Amount: 1000},
			{UserId: friendBID, Amount: 2000},
		},
	})
	// friendA を精算済みにしておく。
	mustSettleExpenseParticipant(t, ctx, svc, db, created.Id, friendAID, payerID, true)

	ts := newMCPTestServer(t, db)
	session := newMCPTestSession(t, ts.URL, issueMCPAccessToken(t, db, payerID, "read"))

	t.Run("参加者の負担額・精算状況を含む詳細が取得できる", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_expense",
			Arguments: map[string]any{"expense_id": created.Id},
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if res.IsError {
			t.Fatalf("ツールがエラーを返した: %+v", res.Content)
		}

		var out struct {
			Expense struct {
				ExpenseID   string `json:"expense_id"`
				Title       string `json:"title"`
				PayerName   string `json:"payer_name"`
				TotalAmount int32  `json:"total_amount"`
			} `json:"expense"`
			Participants []struct {
				UserID  string `json:"user_id"`
				Name    string `json:"name"`
				Amount  int32  `json:"amount"`
				Settled bool   `json:"settled"`
			} `json:"participants"`
		}
		unmarshalStructuredContent(t, res, &out)
		if out.Expense.ExpenseID != created.Id {
			t.Errorf("expense_id = %q, want %q", out.Expense.ExpenseID, created.Id)
		}
		if out.Expense.Title != "打ち上げ" {
			t.Errorf("title = %q", out.Expense.Title)
		}
		if out.Expense.PayerName != "mcp-expense-payer" {
			t.Errorf("payer_name = %q", out.Expense.PayerName)
		}
		if len(out.Participants) != 2 {
			t.Fatalf("participants = %d 件, want 2 件: %+v", len(out.Participants), out.Participants)
		}
		byUser := map[string]struct {
			amount  int32
			settled bool
			name    string
		}{}
		for _, p := range out.Participants {
			byUser[p.UserID] = struct {
				amount  int32
				settled bool
				name    string
			}{p.Amount, p.Settled, p.Name}
		}
		if pa := byUser[friendAID]; pa.amount != 1000 || !pa.settled || pa.name != "mcp-friend-a" {
			t.Errorf("friendA = %+v, want {1000 true mcp-friend-a}", pa)
		}
		if pb := byUser[friendBID]; pb.amount != 2000 || pb.settled || pb.name != "mcp-friend-b" {
			t.Errorf("friendB = %+v, want {2000 false mcp-friend-b}", pb)
		}
	})

	t.Run("存在しない expense_id はエラー", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_expense",
			Arguments: map[string]any{"expense_id": id.New()},
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if !res.IsError {
			t.Fatal("存在しない expense_id で成功してしまった")
		}
	})
}

func TestIntegrationMCPCreateExpense(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	memberID := createTestUser(t, db, "mcp-expense-member", auth.RoleMember)
	friendAID := createTestUser(t, db, "mcp-friend-a", auth.RoleMember)
	friendBID := createTestUser(t, db, "mcp-friend-b", auth.RoleMember)
	ts := newMCPTestServer(t, db)

	// 立替者（自分 = memberID）は participants に含めない。
	createArgs := func() map[string]any {
		return map[string]any{
			"title":       "打ち上げ @ 〇〇",
			"occurred_on": "2026-07-20",
			"participants": []map[string]any{
				{"user_id": friendAID, "amount": 1000},
				{"user_id": friendBID, "amount": 2000},
			},
		}
	}

	t.Run("member ロール + write スコープで登録でき、立替者は自分になる", func(t *testing.T) {
		session := newMCPTestSession(t, ts.URL, issueMCPAccessToken(t, db, memberID, "read write"))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "create_expense",
			Arguments: createArgs(),
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if res.IsError {
			t.Fatalf("ツールがエラーを返した: %+v", res.Content)
		}

		var out struct {
			Expense struct {
				ExpenseID        string   `json:"expense_id"`
				Title            string   `json:"title"`
				OccurredOn       string   `json:"occurred_on"`
				PayerName        string   `json:"payer_name"`
				TotalAmount      int32    `json:"total_amount"`
				ParticipantCount int32    `json:"participant_count"`
				SettledCount     int32    `json:"settled_count"`
				ParticipantNames []string `json:"participant_names"`
			} `json:"expense"`
		}
		unmarshalStructuredContent(t, res, &out)
		if out.Expense.ExpenseID == "" {
			t.Fatalf("expense_id が空: %+v", out)
		}
		if out.Expense.Title != "打ち上げ @ 〇〇" {
			t.Errorf("title = %q", out.Expense.Title)
		}
		if out.Expense.OccurredOn != "2026-07-20" {
			t.Errorf("occurred_on = %q", out.Expense.OccurredOn)
		}
		// 立替者は Bearer トークンのユーザー自身になること。
		if out.Expense.PayerName != "mcp-expense-member" {
			t.Errorf("payer_name = %q, want mcp-expense-member", out.Expense.PayerName)
		}
		if out.Expense.TotalAmount != 3000 {
			t.Errorf("total_amount = %d, want 3000", out.Expense.TotalAmount)
		}
		if out.Expense.ParticipantCount != 2 {
			t.Errorf("participant_count = %d, want 2", out.Expense.ParticipantCount)
		}
		if out.Expense.SettledCount != 0 {
			t.Errorf("settled_count = %d, want 0", out.Expense.SettledCount)
		}

		// DB にも登録されていること。
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM expenses WHERE id = ? AND paid_by = ?", out.Expense.ExpenseID, memberID).Scan(&count); err != nil {
			t.Fatalf("expenses の件数取得に失敗: %v", err)
		}
		if count != 1 {
			t.Errorf("登録後の expenses = %d 件, want 1（paid_by = 自分）", count)
		}
	})

	t.Run("read のみのトークンでは write スコープ不足でエラー", func(t *testing.T) {
		session := newMCPTestSession(t, ts.URL, issueMCPAccessToken(t, db, memberID, "read"))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "create_expense",
			Arguments: createArgs(),
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if !res.IsError {
			t.Fatal("write スコープ無しで成功してしまった")
		}
		if text := mcpResultText(t, res); !strings.Contains(text, "write") {
			t.Errorf("エラーメッセージに write スコープの言及が無い: %s", text)
		}
	})

	t.Run("参加者に自分を含めるとエラー", func(t *testing.T) {
		session := newMCPTestSession(t, ts.URL, issueMCPAccessToken(t, db, memberID, "read write"))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "create_expense",
			Arguments: map[string]any{
				"title":       "自分入り",
				"occurred_on": "2026-07-20",
				"participants": []map[string]any{
					{"user_id": memberID, "amount": 1000},
				},
			},
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if !res.IsError {
			t.Fatal("立替者自身を参加者に含めて成功してしまった")
		}
	})
}

func TestIntegrationMCPUpdateExpense(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newExpenseService(db)

	payerID := createTestUser(t, db, "mcp-expense-payer", auth.RoleMember)
	friendAID := createTestUser(t, db, "mcp-friend-a", auth.RoleMember)
	friendBID := createTestUser(t, db, "mcp-friend-b", auth.RoleMember)
	friendCID := createTestUser(t, db, "mcp-friend-c", auth.RoleMember)
	outsiderID := createTestUser(t, db, "mcp-outsider", auth.RoleMember)
	eventID := createTestEvent(t, db, "テスト公演")
	ticketID := createTestTicket(t, db, eventID, payerID)

	ts := newMCPTestServer(t, db)
	session := newMCPTestSession(t, ts.URL, issueMCPAccessToken(t, db, payerID, "read write"))

	// getExpenseParticipants は payer セッションで get_expense を呼び、user_id → {amount, settled} を返す。
	getExpenseParticipants := func(t *testing.T, expenseID string) map[string]struct {
		amount  int32
		settled bool
	} {
		t.Helper()
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_expense",
			Arguments: map[string]any{"expense_id": expenseID},
		})
		if err != nil {
			t.Fatalf("get_expense に失敗: %v", err)
		}
		if res.IsError {
			t.Fatalf("get_expense がエラーを返した: %+v", res.Content)
		}
		var out struct {
			Participants []struct {
				UserID  string `json:"user_id"`
				Amount  int32  `json:"amount"`
				Settled bool   `json:"settled"`
			} `json:"participants"`
		}
		unmarshalStructuredContent(t, res, &out)
		m := map[string]struct {
			amount  int32
			settled bool
		}{}
		for _, p := range out.Participants {
			m[p.UserID] = struct {
				amount  int32
				settled bool
			}{p.Amount, p.Settled}
		}
		return m
	}

	t.Run("title だけ指定で他フィールドと参加者（settled 含む）が維持される", func(t *testing.T) {
		e := mustCreateExpense(t, ctx, svc, db, payerID, &nazobuv1.CreateExpenseRequest{
			Title: "打ち上げ", OccurredOn: "2026-07-20",
			Participants: []*nazobuv1.ExpenseParticipantInput{
				{UserId: friendAID, Amount: 1000},
				{UserId: friendBID, Amount: 2000},
			},
		})
		mustSettleExpenseParticipant(t, ctx, svc, db, e.Id, friendAID, payerID, true)

		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "update_expense",
			Arguments: map[string]any{"expense_id": e.Id, "title": "打ち上げ（改題）"},
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if res.IsError {
			t.Fatalf("ツールがエラーを返した: %+v", res.Content)
		}

		var out struct {
			Expense struct {
				Title            string `json:"title"`
				OccurredOn       string `json:"occurred_on"`
				PayerName        string `json:"payer_name"`
				TotalAmount      int32  `json:"total_amount"`
				ParticipantCount int32  `json:"participant_count"`
				SettledCount     int32  `json:"settled_count"`
			} `json:"expense"`
		}
		unmarshalStructuredContent(t, res, &out)
		if out.Expense.Title != "打ち上げ（改題）" {
			t.Errorf("title = %q", out.Expense.Title)
		}
		// 省略したフィールドが維持されていること。
		if out.Expense.OccurredOn != "2026-07-20" {
			t.Errorf("occurred_on が変わってしまった: %q", out.Expense.OccurredOn)
		}
		if out.Expense.PayerName != "mcp-expense-payer" {
			t.Errorf("payer_name が変わってしまった: %q", out.Expense.PayerName)
		}
		if out.Expense.TotalAmount != 3000 {
			t.Errorf("total_amount が変わってしまった: %d", out.Expense.TotalAmount)
		}
		if out.Expense.ParticipantCount != 2 {
			t.Errorf("participant_count が変わってしまった: %d", out.Expense.ParticipantCount)
		}
		// friendA の精算済みが維持されていること。
		if out.Expense.SettledCount != 1 {
			t.Errorf("settled_count が変わってしまった: %d, want 1", out.Expense.SettledCount)
		}
		parts := getExpenseParticipants(t, e.Id)
		if pa := parts[friendAID]; pa.amount != 1000 || !pa.settled {
			t.Errorf("friendA = %+v, want {1000 true}", pa)
		}
		if pb := parts[friendBID]; pb.amount != 2000 || pb.settled {
			t.Errorf("friendB = %+v, want {2000 false}", pb)
		}
	})

	t.Run("participants 指定は全量置換で残存参加者の settled を維持する", func(t *testing.T) {
		e := mustCreateExpense(t, ctx, svc, db, payerID, &nazobuv1.CreateExpenseRequest{
			Title: "全量置換テスト", OccurredOn: "2026-07-20",
			Participants: []*nazobuv1.ExpenseParticipantInput{
				{UserId: friendAID, Amount: 1000},
				{UserId: friendBID, Amount: 2000},
			},
		})
		mustSettleExpenseParticipant(t, ctx, svc, db, e.Id, friendAID, payerID, true)

		// friendA は残す（amount 変更）、friendB は外す、friendC は新規追加。
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "update_expense",
			Arguments: map[string]any{
				"expense_id": e.Id,
				"participants": []map[string]any{
					{"user_id": friendAID, "amount": 1500},
					{"user_id": friendCID, "amount": 3000},
				},
			},
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if res.IsError {
			t.Fatalf("ツールがエラーを返した: %+v", res.Content)
		}

		parts := getExpenseParticipants(t, e.Id)
		if len(parts) != 2 {
			t.Fatalf("更新後の参加者数 = %d, want 2: %+v", len(parts), parts)
		}
		// 残った friendA は amount が更新され、settled が保持されていること。
		if pa := parts[friendAID]; pa.amount != 1500 || !pa.settled {
			t.Errorf("friendA = %+v, want {1500 true（settled 保持）}", pa)
		}
		// 新規 friendC は追加され、未精算であること。
		if pc := parts[friendCID]; pc.amount != 3000 || pc.settled {
			t.Errorf("friendC = %+v, want {3000 false}", pc)
		}
		// 外した friendB は消えていること。
		if _, ok := parts[friendBID]; ok {
			t.Error("外したはずの friendB が残っている")
		}
	})

	t.Run("ticket_id='' で紐付けを解除できる", func(t *testing.T) {
		e := mustCreateExpense(t, ctx, svc, db, payerID, &nazobuv1.CreateExpenseRequest{
			TicketId: ticketID, Title: "公演後の飲み会", OccurredOn: "2026-07-20",
			Participants: []*nazobuv1.ExpenseParticipantInput{{UserId: friendAID, Amount: 1000}},
		})
		if e.TicketId != ticketID {
			t.Fatalf("登録直後の ticket_id = %q, want %q", e.TicketId, ticketID)
		}

		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "update_expense",
			Arguments: map[string]any{"expense_id": e.Id, "ticket_id": ""},
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if res.IsError {
			t.Fatalf("ツールがエラーを返した: %+v", res.Content)
		}
		var out struct {
			Expense struct {
				TicketID   string `json:"ticket_id"`
				EventTitle string `json:"event_title"`
			} `json:"expense"`
		}
		unmarshalStructuredContent(t, res, &out)
		if out.Expense.TicketID != "" {
			t.Errorf("ticket_id = %q, want 空（紐付け解除）", out.Expense.TicketID)
		}
		if out.Expense.EventTitle != "" {
			t.Errorf("event_title = %q, want 空", out.Expense.EventTitle)
		}
	})

	t.Run("立替者でも admin でもない member はエラー", func(t *testing.T) {
		e := mustCreateExpense(t, ctx, svc, db, payerID, &nazobuv1.CreateExpenseRequest{
			Title: "権限テスト", OccurredOn: "2026-07-20",
			Participants: []*nazobuv1.ExpenseParticipantInput{{UserId: friendAID, Amount: 1000}},
		})
		otherSession := newMCPTestSession(t, ts.URL, issueMCPAccessToken(t, db, outsiderID, "read write"))
		res, err := otherSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "update_expense",
			Arguments: map[string]any{"expense_id": e.Id, "title": "勝手に更新"},
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if !res.IsError {
			t.Fatal("編集権限の無い member で成功してしまった")
		}
	})

	t.Run("read のみのトークンでは write スコープ不足でエラー", func(t *testing.T) {
		e := mustCreateExpense(t, ctx, svc, db, payerID, &nazobuv1.CreateExpenseRequest{
			Title: "スコープテスト", OccurredOn: "2026-07-20",
			Participants: []*nazobuv1.ExpenseParticipantInput{{UserId: friendAID, Amount: 1000}},
		})
		readSession := newMCPTestSession(t, ts.URL, issueMCPAccessToken(t, db, payerID, "read"))
		res, err := readSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "update_expense",
			Arguments: map[string]any{"expense_id": e.Id, "title": "改題"},
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if !res.IsError {
			t.Fatal("write スコープ無しで成功してしまった")
		}
		if text := mcpResultText(t, res); !strings.Contains(text, "write") {
			t.Errorf("エラーメッセージに write スコープの言及が無い: %s", text)
		}
	})
}

func TestIntegrationMCPUpdateExpenseParticipantSettlement(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newExpenseService(db)

	payerID := createTestUser(t, db, "mcp-expense-payer", auth.RoleMember)
	friendID := createTestUser(t, db, "mcp-friend", auth.RoleMember)

	created := mustCreateExpense(t, ctx, svc, db, payerID, &nazobuv1.CreateExpenseRequest{
		Title: "打ち上げ", OccurredOn: "2026-07-20",
		Participants: []*nazobuv1.ExpenseParticipantInput{{UserId: friendID, Amount: 1000}},
	})

	ts := newMCPTestServer(t, db)

	// settledCountOf は payer セッションで get_expense を呼び、精算済み人数を返す。
	writeSession := newMCPTestSession(t, ts.URL, issueMCPAccessToken(t, db, payerID, "read write"))
	settledCountOf := func(t *testing.T) int32 {
		t.Helper()
		res, err := writeSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_expense",
			Arguments: map[string]any{"expense_id": created.Id},
		})
		if err != nil {
			t.Fatalf("get_expense に失敗: %v", err)
		}
		var out struct {
			Expense struct {
				SettledCount int32 `json:"settled_count"`
			} `json:"expense"`
		}
		unmarshalStructuredContent(t, res, &out)
		return out.Expense.SettledCount
	}

	t.Run("精算済み ⇔ 未精算をトグルできる", func(t *testing.T) {
		// 未精算 → 精算済み。
		res, err := writeSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "update_expense_participant_settlement",
			Arguments: map[string]any{"expense_id": created.Id, "user_id": friendID, "settled": true},
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if res.IsError {
			t.Fatalf("ツールがエラーを返した: %+v", res.Content)
		}
		var out struct {
			Expense struct {
				SettledCount int32 `json:"settled_count"`
			} `json:"expense"`
		}
		unmarshalStructuredContent(t, res, &out)
		if out.Expense.SettledCount != 1 {
			t.Errorf("精算後の settled_count = %d, want 1", out.Expense.SettledCount)
		}
		if got := settledCountOf(t); got != 1 {
			t.Errorf("get_expense の settled_count = %d, want 1", got)
		}

		// 精算済み → 未精算。
		res, err = writeSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "update_expense_participant_settlement",
			Arguments: map[string]any{"expense_id": created.Id, "user_id": friendID, "settled": false},
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if res.IsError {
			t.Fatalf("ツールがエラーを返した: %+v", res.Content)
		}
		if got := settledCountOf(t); got != 0 {
			t.Errorf("未精算に戻した後の settled_count = %d, want 0", got)
		}
	})

	t.Run("read のみのトークンでは write スコープ不足でエラー", func(t *testing.T) {
		readSession := newMCPTestSession(t, ts.URL, issueMCPAccessToken(t, db, payerID, "read"))
		res, err := readSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "update_expense_participant_settlement",
			Arguments: map[string]any{"expense_id": created.Id, "user_id": friendID, "settled": true},
		})
		if err != nil {
			t.Fatalf("CallTool に失敗: %v", err)
		}
		if !res.IsError {
			t.Fatal("write スコープ無しで成功してしまった")
		}
		if text := mcpResultText(t, res); !strings.Contains(text, "write") {
			t.Errorf("エラーメッセージに write スコープの言及が無い: %s", text)
		}
	})
}
