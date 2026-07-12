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
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aruma256/nazobu/backend/internal/auth"
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
	handler := newMCPHandler(newMyPageService(db), newTicketService(db), newEventService(db), newUserService(db))
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
