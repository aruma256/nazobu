package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseClientIDURL(t *testing.T) {
	valid := []string{
		"https://claude.ai/oauth/claude-code-client-metadata",
		"https://example.com/client.json",
	}
	for _, raw := range valid {
		if _, err := parseClientIDURL(raw); err != nil {
			t.Errorf("parseClientIDURL(%q) = %v, 正常な URL を弾いている", raw, err)
		}
	}

	invalid := []string{
		"",
		"http://example.com/client.json",       // https 以外
		"https://example.com/client.json#frag", // fragment 付き
		"https://localhost/client.json",        // ローカルホスト
		"https://127.0.0.1/client.json",        // ループバック IP
		"https://192.168.1.10/client.json",     // プライベート IP
		"https://10.0.0.1/client.json",         // プライベート IP
		"https://169.254.1.1/client.json",      // リンクローカル
		"ftp://example.com/client.json",
	}
	for _, raw := range invalid {
		if _, err := parseClientIDURL(raw); err == nil {
			t.Errorf("parseClientIDURL(%q) がエラーにならない", raw)
		}
	}
}

func TestFetchClientMetadata(t *testing.T) {
	t.Run("正常系", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// client_id はテストサーバの URL に合わせて動的に返す
			_, _ = w.Write([]byte(`{
				"client_id": "http://` + r.Host + `/meta",
				"client_name": "Claude",
				"redirect_uris": ["https://claude.ai/api/mcp/auth_callback"]
			}`))
		}))
		defer srv.Close()

		meta, err := fetchClientMetadata(context.Background(), srv.Client(), srv.URL+"/meta")
		if err != nil {
			t.Fatalf("fetchClientMetadata: %v", err)
		}
		if meta.ClientName != "Claude" {
			t.Errorf("ClientName = %q", meta.ClientName)
		}
		if len(meta.RedirectURIs) != 1 || meta.RedirectURIs[0] != "https://claude.ai/api/mcp/auth_callback" {
			t.Errorf("RedirectURIs = %v", meta.RedirectURIs)
		}
	})

	t.Run("document 内の client_id が URL と不一致なら拒否", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"client_id": "https://evil.example.com/meta", "redirect_uris": ["https://claude.ai/cb"]}`))
		}))
		defer srv.Close()

		if _, err := fetchClientMetadata(context.Background(), srv.Client(), srv.URL+"/meta"); err == nil {
			t.Error("client_id 不一致の CIMD を受け入れている")
		}
	})

	t.Run("redirect_uris 無しは拒否", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"client_name": "x"}`))
		}))
		defer srv.Close()

		if _, err := fetchClientMetadata(context.Background(), srv.Client(), srv.URL+"/meta"); err == nil {
			t.Error("redirect_uris 無しの CIMD を受け入れている")
		}
	})

	t.Run("非 200 は拒否", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		defer srv.Close()

		if _, err := fetchClientMetadata(context.Background(), srv.Client(), srv.URL+"/meta"); err == nil {
			t.Error("404 の CIMD を受け入れている")
		}
	})

	t.Run("JSON でないレスポンスは拒否", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("<html>not json</html>"))
		}))
		defer srv.Close()

		if _, err := fetchClientMetadata(context.Background(), srv.Client(), srv.URL+"/meta"); err == nil {
			t.Error("JSON でない CIMD を受け入れている")
		}
	})
}

func TestRedirectURIAllowed(t *testing.T) {
	meta := &ClientMetadata{
		RedirectURIs: []string{
			"https://claude.ai/api/mcp/auth_callback",
			"http://localhost/callback",
			"http://127.0.0.1/callback",
		},
	}

	allowed := []string{
		"https://claude.ai/api/mcp/auth_callback",
		// ループバックは port を無視して一致（Claude Code の ephemeral port 対応）
		"http://localhost:3118/callback",
		"http://localhost/callback",
		"http://127.0.0.1:49152/callback",
	}
	for _, uri := range allowed {
		if !redirectURIAllowed(meta, uri) {
			t.Errorf("redirectURIAllowed(%q) = false, 許可されるべき", uri)
		}
	}

	denied := []string{
		"https://evil.example.com/callback",
		"https://claude.ai/api/mcp/auth_callback2",
		"https://claude.ai/api/mcp/auth_callback?extra=1", // 完全一致でない
		"http://localhost:3118/other",                     // path 不一致
		"http://192.168.1.5:3118/callback",                // ループバックでない
		"https://localhost:3118/callback",                 // scheme 不一致（登録は http）
	}
	for _, uri := range denied {
		if redirectURIAllowed(meta, uri) {
			t.Errorf("redirectURIAllowed(%q) = true, 拒否されるべき", uri)
		}
	}
}

func TestIsLoopbackRedirectURI(t *testing.T) {
	if !isLoopbackRedirectURI("http://localhost:3118/callback") {
		t.Error("localhost がループバック扱いにならない")
	}
	if !isLoopbackRedirectURI("http://127.0.0.1/callback") {
		t.Error("127.0.0.1 がループバック扱いにならない")
	}
	if isLoopbackRedirectURI("https://claude.ai/api/mcp/auth_callback") {
		t.Error("通常の URL がループバック扱いになっている")
	}
}

func TestCIMDCacheExpiry(t *testing.T) {
	c := newCIMDCache()
	meta := &ClientMetadata{ClientID: "https://example.com/meta"}
	now := testTime(t)

	c.put(meta.ClientID, meta, now)
	if got := c.get(meta.ClientID, now.Add(cimdCacheTTL-1)); got == nil {
		t.Error("TTL 内なのにキャッシュミス")
	}
	if got := c.get(meta.ClientID, now.Add(cimdCacheTTL+1)); got != nil {
		t.Error("TTL 超過なのにキャッシュヒット")
	}
	if got := c.get("https://other.example.com/meta", now); got != nil {
		t.Error("未登録の client_id でキャッシュヒット")
	}
}

func TestFetchClientMetadataSizeLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 上限を超える巨大 JSON（redirect_uris が途中で切れて parse エラーになる）
		_, _ = w.Write([]byte(`{"redirect_uris": ["` + strings.Repeat("a", cimdMaxBodySize) + `"]}`))
	}))
	defer srv.Close()

	if _, err := fetchClientMetadata(context.Background(), srv.Client(), srv.URL+"/meta"); err == nil {
		t.Error("サイズ上限超過の CIMD を受け入れている")
	}
}
