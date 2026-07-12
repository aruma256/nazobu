package oauth

// OAuth 2.1 認可コードフロー全体（authorize → token → Bearer 検証 → refresh）を
// 実 MySQL で検証する統合テスト。CIMD の取得のみ resolveClient の差し替えでスタブする
// （外部 HTTPS への依存を切るため。取得・検証自体は cimd_test.go で担保）。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aruma256/nazobu/backend/internal/auth"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/aruma256/nazobu/backend/internal/id"
	"github.com/aruma256/nazobu/backend/internal/testdb"
)

const (
	testClientID    = "https://claude.ai/test-client-metadata"
	testBaseURL     = "https://nazobu.example.com"
	testRedirectURI = "http://127.0.0.1:3118/callback" // 登録側は port 無しのループバック
	testClientName  = "Test Claude"
	integVerifier   = "integration-verifier-integration-verifier-1234567890"
)

type flowEnv struct {
	srv          *Server
	ts           *httptest.Server
	client       *http.Client
	sessionToken string
	userID       string
}

func newFlowEnv(t *testing.T) *flowEnv {
	t.Helper()
	db := testdb.Open(t)
	ctx := context.Background()

	userID := id.New()
	if err := queries.New(db).CreateUser(ctx, queries.CreateUserParams{
		ID:          userID,
		DisplayName: "oauth-test-user",
	}); err != nil {
		t.Fatalf("user 作成に失敗: %v", err)
	}
	sessionToken, err := auth.CreateSession(ctx, db, userID)
	if err != nil {
		t.Fatalf("session 作成に失敗: %v", err)
	}

	srv := NewServer(db, http.DefaultClient, testBaseURL, false)
	srv.resolveClient = func(_ context.Context, clientID string) (*ClientMetadata, error) {
		if clientID != testClientID {
			return nil, errors.New("未知の client_id")
		}
		return &ClientMetadata{
			ClientID:     clientID,
			ClientName:   testClientName,
			RedirectURIs: []string{"http://127.0.0.1/callback"},
		}, nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /oauth/authorize", srv.HandleAuthorizeGet)
	mux.HandleFunc("POST /oauth/authorize", srv.HandleAuthorizePost)
	mux.HandleFunc("POST /oauth/token", srv.HandleToken)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	client := ts.Client()
	// redirect の Location を検証するため自動追従を止める。
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	return &flowEnv{srv: srv, ts: ts, client: client, sessionToken: sessionToken, userID: userID}
}

func (e *flowEnv) authorizeQuery(state string) url.Values {
	return url.Values{
		"response_type":         {"code"},
		"client_id":             {testClientID},
		"redirect_uri":          {testRedirectURI},
		"state":                 {state},
		"code_challenge":        {testChallenge(integVerifier)},
		"code_challenge_method": {"S256"},
		"resource":              {testBaseURL + "/mcp"},
	}
}

// obtainCode は同意画面の表示 → 承認 POST まで進めて認可コードを取り出す。
func (e *flowEnv) obtainCode(t *testing.T, state string) string {
	t.Helper()

	// GET: 同意画面
	req, _ := http.NewRequest(http.MethodGet, e.ts.URL+"/oauth/authorize?"+e.authorizeQuery(state).Encode(), nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: e.sessionToken})
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("authorize GET に失敗: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorize GET = %d, body: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), testClientName) {
		t.Errorf("同意画面にクライアント名が表示されていない")
	}
	var csrf string
	for _, c := range resp.Cookies() {
		if c.Name == consentCSRFCookieName {
			csrf = c.Value
		}
	}
	if csrf == "" {
		t.Fatal("CSRF cookie が発行されていない")
	}

	// POST: 承認
	form := url.Values{
		"client_id":      {testClientID},
		"redirect_uri":   {testRedirectURI},
		"scope":          {ScopeRead},
		"state":          {state},
		"code_challenge": {testChallenge(integVerifier)},
		"csrf_token":     {csrf},
		"action":         {"approve"},
	}
	req, _ = http.NewRequest(http.MethodPost, e.ts.URL+"/oauth/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: e.sessionToken})
	req.AddCookie(&http.Cookie{Name: consentCSRFCookieName, Value: csrf})
	resp, err = e.client.Do(req)
	if err != nil {
		t.Fatalf("authorize POST に失敗: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("authorize POST = %d, want 302", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("Location の解析に失敗: %v", err)
	}
	if got := loc.Query().Get("state"); got != state {
		t.Errorf("state = %q, want %q", got, state)
	}
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatalf("Location に code が無い: %s", loc)
	}
	return code
}

func (e *flowEnv) postToken(t *testing.T, form url.Values) (*http.Response, map[string]any) {
	t.Helper()
	resp, err := e.client.PostForm(e.ts.URL+"/oauth/token", form)
	if err != nil {
		t.Fatalf("token POST に失敗: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("token レスポンスの JSON 解析に失敗: %v", err)
	}
	return resp, m
}

func (e *flowEnv) exchangeCode(t *testing.T, code string) map[string]any {
	t.Helper()
	resp, m := e.postToken(t, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {testClientID},
		"redirect_uri":  {testRedirectURI},
		"code_verifier": {integVerifier},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token 交換 = %d, body: %v", resp.StatusCode, m)
	}
	return m
}

// bearerUserID は Middleware 越しにアクセストークンを検証し、通れば user ID を返す。
func (e *flowEnv) bearerUserID(t *testing.T, accessToken string) (int, string) {
	t.Helper()
	var gotUserID string
	h := e.srv.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := auth.UserFromContext(r.Context()); u != nil {
			gotUserID = u.ID
		}
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	h.ServeHTTP(rec, req)
	return rec.Code, gotUserID
}

func TestIntegrationOAuthAuthorizationCodeFlow(t *testing.T) {
	e := newFlowEnv(t)

	// 未ログインならログインへリダイレクトされ、next で戻ってこられること
	req, _ := http.NewRequest(http.MethodGet, e.ts.URL+"/oauth/authorize?"+e.authorizeQuery("s1").Encode(), nil)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("authorize GET に失敗: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("未ログインの authorize GET = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, "/auth/discord/login?next=") {
		t.Errorf("未ログイン時の Location = %q", loc)
	}

	// 承認 → code → token
	code := e.obtainCode(t, "state-1")
	tok := e.exchangeCode(t, code)
	accessToken, _ := tok["access_token"].(string)
	refreshToken, _ := tok["refresh_token"].(string)
	if accessToken == "" || refreshToken == "" {
		t.Fatalf("トークンが発行されていない: %v", tok)
	}
	if tok["token_type"] != "Bearer" {
		t.Errorf("token_type = %v", tok["token_type"])
	}
	if tok["scope"] != ScopeRead {
		t.Errorf("scope = %v", tok["scope"])
	}

	// 認可コードの再利用は invalid_grant
	resp2, m := e.postToken(t, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {testClientID},
		"redirect_uri":  {testRedirectURI},
		"code_verifier": {integVerifier},
	})
	if resp2.StatusCode != http.StatusBadRequest || m["error"] != "invalid_grant" {
		t.Errorf("code 再利用 = %d %v, want 400 invalid_grant", resp2.StatusCode, m)
	}

	// アクセストークンで Bearer 認証が通り、user が引けること
	status, gotUserID := e.bearerUserID(t, accessToken)
	if status != http.StatusOK || gotUserID != e.userID {
		t.Errorf("Bearer 検証 = (%d, %q), want (200, %q)", status, gotUserID, e.userID)
	}

	// 不正なトークンは 401 + WWW-Authenticate（resource_metadata 付き）
	{
		h := e.srv.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("不正トークン = %d, want 401", rec.Code)
		}
		challenge := rec.Header().Get("WWW-Authenticate")
		if !strings.Contains(challenge, `resource_metadata="`+testBaseURL+`/.well-known/oauth-protected-resource"`) {
			t.Errorf("WWW-Authenticate = %q", challenge)
		}
	}

	// refresh でトークンがローテーションされ、旧トークンが無効になること
	resp3, m3 := e.postToken(t, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {testClientID},
	})
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("refresh = %d, body: %v", resp3.StatusCode, m3)
	}
	newAccessToken, _ := m3["access_token"].(string)
	newRefreshToken, _ := m3["refresh_token"].(string)
	if newAccessToken == "" || newAccessToken == accessToken || newRefreshToken == "" || newRefreshToken == refreshToken {
		t.Fatalf("トークンがローテーションされていない")
	}
	if status, _ := e.bearerUserID(t, accessToken); status != http.StatusUnauthorized {
		t.Errorf("旧アクセストークンがまだ有効: %d", status)
	}
	if status, gotUserID := e.bearerUserID(t, newAccessToken); status != http.StatusOK || gotUserID != e.userID {
		t.Errorf("新アクセストークンで認証できない: (%d, %q)", status, gotUserID)
	}

	// 旧リフレッシュトークンは invalid_grant
	resp4, m4 := e.postToken(t, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {testClientID},
	})
	if resp4.StatusCode != http.StatusBadRequest || m4["error"] != "invalid_grant" {
		t.Errorf("旧 refresh_token = %d %v, want 400 invalid_grant", resp4.StatusCode, m4)
	}
}

func TestIntegrationOAuthDenyAndPKCEFailure(t *testing.T) {
	e := newFlowEnv(t)

	// 誤った code_verifier は invalid_grant になり、コードは消費されること
	code := e.obtainCode(t, "state-pkce")
	resp, m := e.postToken(t, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {testClientID},
		"redirect_uri":  {testRedirectURI},
		"code_verifier": {"wrong-verifier-wrong-verifier-wrong-verifier-000000"},
	})
	if resp.StatusCode != http.StatusBadRequest || m["error"] != "invalid_grant" {
		t.Errorf("誤 verifier = %d %v, want 400 invalid_grant", resp.StatusCode, m)
	}
	// 消費済みなので正しい verifier でも失敗する
	resp2, m2 := e.postToken(t, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {testClientID},
		"redirect_uri":  {testRedirectURI},
		"code_verifier": {integVerifier},
	})
	if resp2.StatusCode != http.StatusBadRequest || m2["error"] != "invalid_grant" {
		t.Errorf("検証失敗後のコードが再利用できてしまう: %d %v", resp2.StatusCode, m2)
	}

	// 拒否時は access_denied で redirect されること
	req, _ := http.NewRequest(http.MethodGet, e.ts.URL+"/oauth/authorize?"+e.authorizeQuery("state-deny").Encode(), nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: e.sessionToken})
	getResp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("authorize GET に失敗: %v", err)
	}
	_ = getResp.Body.Close()
	var csrf string
	for _, c := range getResp.Cookies() {
		if c.Name == consentCSRFCookieName {
			csrf = c.Value
		}
	}
	form := url.Values{
		"client_id":      {testClientID},
		"redirect_uri":   {testRedirectURI},
		"scope":          {ScopeRead},
		"state":          {"state-deny"},
		"code_challenge": {testChallenge(integVerifier)},
		"csrf_token":     {csrf},
		"action":         {"deny"},
	}
	postReq, _ := http.NewRequest(http.MethodPost, e.ts.URL+"/oauth/authorize", strings.NewReader(form.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: e.sessionToken})
	postReq.AddCookie(&http.Cookie{Name: consentCSRFCookieName, Value: csrf})
	postResp, err := e.client.Do(postReq)
	if err != nil {
		t.Fatalf("authorize POST に失敗: %v", err)
	}
	_ = postResp.Body.Close()
	loc, _ := url.Parse(postResp.Header.Get("Location"))
	if loc.Query().Get("error") != "access_denied" {
		t.Errorf("拒否時の redirect = %q", postResp.Header.Get("Location"))
	}
}

func TestIntegrationOAuthExpiredTokens(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	q := queries.New(db)

	userID := id.New()
	if err := q.CreateUser(ctx, queries.CreateUserParams{ID: userID, DisplayName: "expiry-test-user"}); err != nil {
		t.Fatalf("user 作成に失敗: %v", err)
	}

	srv := NewServer(db, http.DefaultClient, testBaseURL, false)

	// 期限切れアクセストークン + 有効なリフレッシュトークンを直接仕込む
	expiredAccess, _ := generateToken()
	validRefresh, _ := generateToken()
	if err := q.CreateOAuthToken(ctx, queries.CreateOAuthTokenParams{
		ID:                    id.New(),
		UserID:                userID,
		ClientID:              testClientID,
		Scope:                 ScopeRead,
		AccessTokenHash:       hashToken(expiredAccess),
		AccessTokenExpiresAt:  time.Now().Add(-time.Minute),
		RefreshTokenHash:      hashToken(validRefresh),
		RefreshTokenExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("token 作成に失敗: %v", err)
	}

	if _, _, err := srv.lookupAccessToken(ctx, expiredAccess); !errors.Is(err, errInvalidAccessToken) {
		t.Errorf("期限切れアクセストークンの err = %v, want errInvalidAccessToken", err)
	}

	// 期限切れリフレッシュトークンは invalid_grant となり、行ごと削除されること
	expiredRefresh, _ := generateToken()
	tokenID := id.New()
	if err := q.CreateOAuthToken(ctx, queries.CreateOAuthTokenParams{
		ID:                    tokenID,
		UserID:                userID,
		ClientID:              testClientID,
		Scope:                 ScopeRead,
		AccessTokenHash:       hashToken(expiredAccess + "2"),
		AccessTokenExpiresAt:  time.Now().Add(-time.Minute),
		RefreshTokenHash:      hashToken(expiredRefresh),
		RefreshTokenExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("token 作成に失敗: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/token", srv.HandleToken)
	rec := httptest.NewRecorder()
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {expiredRefresh},
		"client_id":     {testClientID},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("期限切れ refresh = %d, want 400", rec.Code)
	}
	if _, err := q.GetOAuthTokenByRefreshTokenHash(ctx, hashToken(expiredRefresh)); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("期限切れ refresh token の行が削除されていない: %v", err)
	}
}
