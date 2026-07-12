package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// testTime はテスト用の固定時刻。
func testTime(t *testing.T) time.Time {
	t.Helper()
	return time.Date(2026, 7, 10, 12, 0, 0, 0, time.FixedZone("Asia/Tokyo", 9*60*60))
}

// testChallenge は verifier に対応する S256 チャレンジを作る。
func testChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

const testVerifier = "test-verifier-test-verifier-test-verifier-1234567890"

func TestVerifyPKCE(t *testing.T) {
	challenge := testChallenge(testVerifier)

	if !verifyPKCE(testVerifier, challenge) {
		t.Error("正しい verifier が検証に失敗")
	}
	if verifyPKCE("wrong-verifier-wrong-verifier-wrong-verifier-123456", challenge) {
		t.Error("誤った verifier が検証を通過")
	}
	if verifyPKCE("short", testChallenge("short")) {
		t.Error("43 文字未満の verifier が検証を通過")
	}
	long := strings.Repeat("a", 129)
	if verifyPKCE(long, testChallenge(long)) {
		t.Error("128 文字超の verifier が検証を通過")
	}
}

func TestParseAuthorizeParams(t *testing.T) {
	const resourceURL = "https://nazobu.example.com/mcp"
	base := url.Values{
		"response_type":         {"code"},
		"code_challenge":        {testChallenge(testVerifier)},
		"code_challenge_method": {"S256"},
		"state":                 {"xyz"},
	}
	clone := func(over map[string]string) url.Values {
		v := url.Values{}
		for k, vs := range base {
			v[k] = append([]string{}, vs...)
		}
		for k, s := range over {
			if s == "" {
				v.Del(k)
			} else {
				v.Set(k, s)
			}
		}
		return v
	}

	t.Run("正常系", func(t *testing.T) {
		p, aerr := parseAuthorizeParams(clone(nil), resourceURL)
		if aerr != nil {
			t.Fatalf("エラー: %+v", aerr)
		}
		if p.scope != ScopeRead+" "+ScopeWrite {
			t.Errorf("scope の既定値が %q（read write になるべき）", p.scope)
		}
		if p.state != "xyz" {
			t.Errorf("state = %q", p.state)
		}
	})

	t.Run("read のみの明示指定は維持される", func(t *testing.T) {
		p, aerr := parseAuthorizeParams(clone(map[string]string{"scope": ScopeRead}), resourceURL)
		if aerr != nil {
			t.Fatalf("エラー: %+v", aerr)
		}
		if p.scope != ScopeRead {
			t.Errorf("scope = %q, want %q", p.scope, ScopeRead)
		}
	})

	t.Run("resource 一致（末尾スラッシュ差は許容）", func(t *testing.T) {
		if _, aerr := parseAuthorizeParams(clone(map[string]string{"resource": resourceURL + "/"}), resourceURL); aerr != nil {
			t.Errorf("末尾スラッシュ付き resource が拒否された: %+v", aerr)
		}
	})

	errCases := map[string]struct {
		over map[string]string
		code string
	}{
		"response_type が code 以外":       {map[string]string{"response_type": "token"}, "unsupported_response_type"},
		"code_challenge 無し":             {map[string]string{"code_challenge": ""}, "invalid_request"},
		"code_challenge が短すぎる":          {map[string]string{"code_challenge": "abc"}, "invalid_request"},
		"code_challenge_method が plain": {map[string]string{"code_challenge_method": "plain"}, "invalid_request"},
		"code_challenge_method 無し":      {map[string]string{"code_challenge_method": ""}, "invalid_request"},
		"未知の scope":                     {map[string]string{"scope": "read admin"}, "invalid_scope"},
		"resource 不一致":                  {map[string]string{"resource": "https://evil.example.com/mcp"}, "invalid_target"},
	}
	for name, tc := range errCases {
		t.Run(name, func(t *testing.T) {
			_, aerr := parseAuthorizeParams(clone(tc.over), resourceURL)
			if aerr == nil {
				t.Fatal("エラーにならない")
			}
			if aerr.code != tc.code {
				t.Errorf("error code = %q, want %q", aerr.code, tc.code)
			}
		})
	}
}

func TestMetadataHandlers(t *testing.T) {
	s := NewServer(nil, http.DefaultClient, "https://nazobu.example.com", true)

	t.Run("authorization server metadata", func(t *testing.T) {
		rec := httptest.NewRecorder()
		s.HandleAuthorizationServerMetadata(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil))

		var m map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatalf("JSON parse: %v", err)
		}
		// Claude が CIMD 方式を選ぶ条件: client_id_metadata_document_supported と
		// token_endpoint_auth_methods_supported の "none" の両方が必要。
		if m["client_id_metadata_document_supported"] != true {
			t.Error("client_id_metadata_document_supported が true でない")
		}
		authMethods, _ := m["token_endpoint_auth_methods_supported"].([]any)
		if len(authMethods) != 1 || authMethods[0] != "none" {
			t.Errorf("token_endpoint_auth_methods_supported = %v", authMethods)
		}
		pkce, _ := m["code_challenge_methods_supported"].([]any)
		if len(pkce) != 1 || pkce[0] != "S256" {
			t.Errorf("code_challenge_methods_supported = %v", pkce)
		}
		if m["issuer"] != "https://nazobu.example.com" {
			t.Errorf("issuer = %v", m["issuer"])
		}
		if m["authorization_endpoint"] != "https://nazobu.example.com/oauth/authorize" {
			t.Errorf("authorization_endpoint = %v", m["authorization_endpoint"])
		}
		if m["token_endpoint"] != "https://nazobu.example.com/oauth/token" {
			t.Errorf("token_endpoint = %v", m["token_endpoint"])
		}
	})

	t.Run("protected resource metadata", func(t *testing.T) {
		rec := httptest.NewRecorder()
		s.HandleProtectedResourceMetadata(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))

		var m map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatalf("JSON parse: %v", err)
		}
		if m["resource"] != "https://nazobu.example.com/mcp" {
			t.Errorf("resource = %v", m["resource"])
		}
		servers, _ := m["authorization_servers"].([]any)
		if len(servers) != 1 || servers[0] != "https://nazobu.example.com" {
			t.Errorf("authorization_servers = %v", servers)
		}
	})
}

func TestBearerToken(t *testing.T) {
	newReq := func(authz string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		if authz != "" {
			r.Header.Set("Authorization", authz)
		}
		return r
	}

	if tok, ok := bearerToken(newReq("Bearer abc123")); !ok || tok != "abc123" {
		t.Errorf("Bearer abc123 → (%q, %v)", tok, ok)
	}
	if tok, ok := bearerToken(newReq("bearer abc123")); !ok || tok != "abc123" {
		t.Errorf("小文字 bearer が拒否された: (%q, %v)", tok, ok)
	}
	if _, ok := bearerToken(newReq("")); ok {
		t.Error("ヘッダ無しで ok になっている")
	}
	if _, ok := bearerToken(newReq("Basic abc123")); ok {
		t.Error("Basic 認証で ok になっている")
	}
	if _, ok := bearerToken(newReq("Bearer ")); ok {
		t.Error("空トークンで ok になっている")
	}
}

func TestBuildRedirectURL(t *testing.T) {
	got := buildRedirectURL("http://localhost:3118/callback?keep=1", url.Values{
		"code":  {"c0de"},
		"state": {"st"},
	})
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := u.Query()
	if q.Get("keep") != "1" || q.Get("code") != "c0de" || q.Get("state") != "st" {
		t.Errorf("buildRedirectURL = %q", got)
	}
}
