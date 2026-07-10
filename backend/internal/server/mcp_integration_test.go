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
func setupMCPTestData(t *testing.T, db *sql.DB) (userID, accessToken string) {
	t.Helper()
	ctx := context.Background()
	q := queries.New(db)

	userID = id.New()
	if err := q.CreateUser(ctx, queries.CreateUserParams{ID: userID, DisplayName: "mcp-test-user"}); err != nil {
		t.Fatalf("user 作成に失敗: %v", err)
	}
	eventID := id.New()
	if err := q.CreateEvent(ctx, queries.CreateEventParams{
		ID:                      eventID,
		Title:                   "テスト脱出公演",
		Url:                     "https://example.com/event",
		Catchphrase:             "きみは謎を解けるか",
		ExpectedDurationMinutes: 120,
	}); err != nil {
		t.Fatalf("event 作成に失敗: %v", err)
	}
	ticketID := id.New()
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

	// アクセストークンは DB に直接発行する（OAuth フローは oauth 側のテストで担保）。
	accessToken = "test-mcp-access-token"
	if err := q.CreateOAuthToken(ctx, queries.CreateOAuthTokenParams{
		ID:                    id.New(),
		UserID:                userID,
		ClientID:              "https://claude.ai/test-client-metadata",
		Scope:                 "read",
		AccessTokenHash:       sha256Hex(accessToken),
		AccessTokenExpiresAt:  time.Now().Add(time.Hour),
		RefreshTokenHash:      sha256Hex(accessToken + "-refresh"),
		RefreshTokenExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("アクセストークン発行に失敗: %v", err)
	}
	return userID, accessToken
}

func newMCPTestServer(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()
	oauthSrv := oauth.NewServer(db, http.DefaultClient, "https://nazobu.example.com", false)
	ts := httptest.NewServer(oauthSrv.Middleware(newMCPHandler(newMyPageService(db))))
	t.Cleanup(ts.Close)
	return ts
}

func TestIntegrationMCPListMyUpcomingTickets(t *testing.T) {
	db := testdb.Open(t)
	_, accessToken := setupMCPTestData(t, db)
	ts := newMCPTestServer(t, db)
	ctx := context.Background()

	transport := &mcp.StreamableClientTransport{
		Endpoint: ts.URL,
		HTTPClient: &http.Client{
			Transport: &bearerTransport{token: accessToken, base: http.DefaultTransport},
		},
		// stateless サーバに対する GET（SSE ストリーム確立）は不要なので張らない。
		DisableStandaloneSSE: true,
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("MCP 接続に失敗: %v", err)
	}
	defer func() { _ = session.Close() }()

	// ツールが公開されていること
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools に失敗: %v", err)
	}
	found := false
	for _, tool := range tools.Tools {
		if tool.Name == "list_my_upcoming_tickets" {
			found = true
		}
	}
	if !found {
		t.Fatalf("list_my_upcoming_tickets が公開されていない: %v", tools.Tools)
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

	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("StructuredContent の marshal に失敗: %v", err)
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
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("StructuredContent の unmarshal に失敗: %v", err)
	}
	if len(out.Tickets) != 1 {
		t.Fatalf("tickets = %d 件, want 1 件: %s", len(out.Tickets), raw)
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
