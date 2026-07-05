package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"

	"github.com/aruma256/nazobu/backend/internal/auth"
	"github.com/aruma256/nazobu/backend/internal/config"
)

func TestSanitizeNextPath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"/events", "/events"},
		{"/events?id=1", "/events?id=1"},
		{"//evil.example.com/", ""},        // プロトコル相対 URL の弾き
		{"https://evil.example.com/", ""},  // 絶対 URL の弾き
		{"events", ""},                     // / 始まりでない
		{"javascript:alert(1)", ""},        // スキーマ付き
		{"/", "/"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := sanitizeNextPath(c.in); got != c.want {
				t.Errorf("sanitizeNextPath(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestGenerateRandomString(t *testing.T) {
	t.Run("base64 raw url 形式で指定 byte 数を符号化", func(t *testing.T) {
		s, err := generateRandomString(32)
		if err != nil {
			t.Fatalf("generateRandomString: %v", err)
		}
		// 32 bytes -> base64 raw url で 43 文字
		if len(s) != 43 {
			t.Errorf("len = %d, want 43", len(s))
		}
		if strings.ContainsAny(s, "+/=") {
			t.Errorf("raw url base64 に + / = が含まれてはならない: %q", s)
		}
	})

	t.Run("呼び出すたびに異なる値", func(t *testing.T) {
		seen := map[string]struct{}{}
		for i := 0; i < 16; i++ {
			s, err := generateRandomString(16)
			if err != nil {
				t.Fatalf("generateRandomString: %v", err)
			}
			if _, ok := seen[s]; ok {
				t.Errorf("衝突: %q", s)
			}
			seen[s] = struct{}{}
		}
	})
}

// newOAuthTestServer は DB を使わないハンドラテスト向けの Server を組み立てる。
func newOAuthTestServer() *Server {
	return &Server{
		cfg: config.Config{
			FrontendURL: "http://localhost:3000",
			Discord:     config.DiscordConfig{ClientID: "cid"},
		},
		discordOAuth: &oauth2.Config{
			ClientID:    "cid",
			Endpoint:    oauth2.Endpoint{AuthURL: "https://discord.com/api/oauth2/authorize"},
			RedirectURL: "http://localhost:3000/auth/discord/callback",
			Scopes:      []string{"identify"},
		},
	}
}

// findCookie は Set-Cookie から name の cookie を探す（無ければ nil）。
func findCookie(t *testing.T, res *http.Response, name string) *http.Cookie {
	t.Helper()
	for _, c := range res.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestHandleDiscordLogin(t *testing.T) {
	t.Run("client_id 未設定なら 503", func(t *testing.T) {
		srv := &Server{cfg: config.Config{}}
		rec := httptest.NewRecorder()
		srv.handleDiscordLogin(rec, httptest.NewRequest(http.MethodGet, "/auth/discord/login", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", rec.Code)
		}
	})

	t.Run("state cookie を発行して Discord の認可 URL へリダイレクト", func(t *testing.T) {
		srv := newOAuthTestServer()
		rec := httptest.NewRecorder()
		srv.handleDiscordLogin(rec, httptest.NewRequest(http.MethodGet, "/auth/discord/login", nil))

		res := rec.Result()
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusFound {
			t.Fatalf("status = %d, want 302", res.StatusCode)
		}

		state := findCookie(t, res, stateCookieName)
		if state == nil || state.Value == "" {
			t.Fatalf("state cookie が発行されていない: %+v", res.Cookies())
		}
		if !state.HttpOnly {
			t.Errorf("state cookie は HttpOnly であるべき")
		}

		loc, err := url.Parse(res.Header.Get("Location"))
		if err != nil {
			t.Fatalf("Location のパースに失敗: %v", err)
		}
		if got := loc.Scheme + "://" + loc.Host + loc.Path; got != "https://discord.com/api/oauth2/authorize" {
			t.Errorf("リダイレクト先 = %q", got)
		}
		// CSRF 対策: cookie と同じ state が認可 URL に乗る。
		if loc.Query().Get("state") != state.Value {
			t.Errorf("URL の state = %q, cookie の state = %q", loc.Query().Get("state"), state.Value)
		}
		if loc.Query().Get("client_id") != "cid" {
			t.Errorf("client_id = %q", loc.Query().Get("client_id"))
		}
	})

	t.Run("next 指定があれば next cookie に保持する", func(t *testing.T) {
		srv := newOAuthTestServer()
		rec := httptest.NewRecorder()
		srv.handleDiscordLogin(rec, httptest.NewRequest(http.MethodGet, "/auth/discord/login?next=/tickets/abc", nil))

		res := rec.Result()
		defer func() { _ = res.Body.Close() }()
		next := findCookie(t, res, nextCookieName)
		if next == nil || next.Value != "/tickets/abc" {
			t.Errorf("next cookie = %+v, want /tickets/abc", next)
		}
	})

	t.Run("不正な next（外部 URL）は cookie に保存しない", func(t *testing.T) {
		srv := newOAuthTestServer()
		rec := httptest.NewRecorder()
		srv.handleDiscordLogin(rec, httptest.NewRequest(http.MethodGet, "/auth/discord/login?next=//evil.example.com/", nil))

		res := rec.Result()
		defer func() { _ = res.Body.Close() }()
		if c := findCookie(t, res, nextCookieName); c != nil {
			t.Errorf("open redirect になる next が cookie に保存された: %+v", c)
		}
	})
}

// handleDiscordCallback の入力検証（state / code）は DB もネットワークも触らないのでここで固定する。
// token exchange 以降の正常系は DB が要るため対象外。
func TestHandleDiscordCallbackBadRequest(t *testing.T) {
	t.Run("state cookie が無ければ 400", func(t *testing.T) {
		srv := newOAuthTestServer()
		rec := httptest.NewRecorder()
		srv.handleDiscordCallback(rec, httptest.NewRequest(http.MethodGet, "/auth/discord/callback?state=x&code=c", nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("state が cookie と一致しなければ 400", func(t *testing.T) {
		srv := newOAuthTestServer()
		req := httptest.NewRequest(http.MethodGet, "/auth/discord/callback?state=tampered&code=c", nil)
		req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "original"})
		rec := httptest.NewRecorder()
		srv.handleDiscordCallback(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("code が無ければ 400 で state cookie は破棄", func(t *testing.T) {
		srv := newOAuthTestServer()
		req := httptest.NewRequest(http.MethodGet, "/auth/discord/callback?state=s1", nil)
		req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "s1"})
		rec := httptest.NewRecorder()
		srv.handleDiscordCallback(rec, req)

		res := rec.Result()
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", res.StatusCode)
		}
		state := findCookie(t, res, stateCookieName)
		if state == nil || state.MaxAge != -1 {
			t.Errorf("使用済み state cookie が破棄されていない: %+v", state)
		}
	})
}

func TestHandleLogout(t *testing.T) {
	// session cookie を持たないリクエストは DB を触らずに cookie 破棄とリダイレクトだけ行う。
	srv := newOAuthTestServer()
	rec := httptest.NewRecorder()
	srv.handleLogout(rec, httptest.NewRequest(http.MethodPost, "/auth/logout", nil))

	res := rec.Result()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != "http://localhost:3000/login" {
		t.Errorf("Location = %q, want http://localhost:3000/login", loc)
	}
	c := findCookie(t, res, auth.SessionCookieName)
	if c == nil || c.MaxAge != -1 || c.Value != "" {
		t.Errorf("session cookie が破棄されていない: %+v", c)
	}
}

func TestClearCookie(t *testing.T) {
	for _, secure := range []bool{true, false} {
		t.Run(fmt.Sprintf("secure=%v", secure), func(t *testing.T) {
			rec := httptest.NewRecorder()
			clearCookie(rec, "test_cookie", secure)

			res := rec.Result()
			defer func() { _ = res.Body.Close() }()

			cookies := res.Cookies()
			if len(cookies) != 1 {
				t.Fatalf("Set-Cookie = %d 個, want 1 (%+v)", len(cookies), cookies)
			}
			c := cookies[0]
			if c.Name != "test_cookie" {
				t.Errorf("Name = %q", c.Name)
			}
			if c.Value != "" {
				t.Errorf("Value = %q, want 空", c.Value)
			}
			if c.MaxAge != -1 {
				t.Errorf("MaxAge = %d, want -1", c.MaxAge)
			}
			if !c.HttpOnly {
				t.Errorf("HttpOnly = false, want true")
			}
			if c.Secure != secure {
				t.Errorf("Secure = %v, want %v", c.Secure, secure)
			}
			if c.Path != "/" {
				t.Errorf("Path = %q, want /", c.Path)
			}
			if c.SameSite != http.SameSiteLaxMode {
				t.Errorf("SameSite = %v, want Lax", c.SameSite)
			}
		})
	}
}
