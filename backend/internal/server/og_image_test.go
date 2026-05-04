package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestIsAllowedOGHost(t *testing.T) {
	cases := []struct {
		name string
		host string
		want bool
	}{
		{"allowlist 内（小文字）", "realdgame.jp", true},
		{"allowlist 内（大文字混在）は case-insensitive で許可", "RealDGame.jp", true},
		{"allowlist 内（ポート付き）も許可", "escape.id:443", true},
		{"allowlist 外", "evil.example.com", false},
		{"空文字", "", false},
		{"サブドメインは別ホスト扱いで不許可", "sub.realdgame.jp", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isAllowedOGHost(c.host); got != c.want {
				t.Errorf("isAllowedOGHost(%q) = %v, want %v", c.host, got, c.want)
			}
		})
	}
}

func TestExtractOGImage(t *testing.T) {
	cases := []struct {
		name string
		html string
		want string
	}{
		{
			name: "property=og:image を拾う",
			html: `<html><head><meta property="og:image" content="https://example.com/a.png"></head><body></body></html>`,
			want: "https://example.com/a.png",
		},
		{
			name: "name=og:image でも拾う（Discord 等が出力するパターン）",
			html: `<html><head><meta name="og:image" content="https://example.com/b.png"></head></html>`,
			want: "https://example.com/b.png",
		},
		{
			name: "property の比較は case-insensitive",
			html: `<html><head><meta property="OG:Image" content="https://example.com/c.png"></head></html>`,
			want: "https://example.com/c.png",
		},
		{
			name: "og:image が無ければ空文字",
			html: `<html><head><meta property="og:title" content="t"></head></html>`,
			want: "",
		},
		{
			name: "<body> に入った時点で打ち切り",
			html: `<html><head></head><body><meta property="og:image" content="https://example.com/d.png"></body></html>`,
			want: "",
		},
		{
			name: "self-closing でも拾える",
			html: `<html><head><meta property="og:image" content="https://example.com/e.png"/></head></html>`,
			want: "https://example.com/e.png",
		},
		{
			name: "content が空なら拾わない",
			html: `<html><head><meta property="og:image" content=""></head></html>`,
			want: "",
		},
		{
			name: "属性無しの meta は無視",
			html: `<html><head><meta></head></html>`,
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extractOGImage(strings.NewReader(c.html))
			if got != c.want {
				t.Errorf("extractOGImage = %q, want %q", got, c.want)
			}
		})
	}
}

// fetchOGImageURL は SSRF 対策として allowlist でホストを絞っているので、
// 実際の testserver は allowlist にないホストになる。allowlist を一時的に
// テストサーバ向けに差し替えて検証する。
func TestFetchOGImageURL_Success(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><meta property="og:image" content="/img/x.png"></head></html>`))
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	host := stripPort(u.Host)
	withAllowedHost(t, host)

	got := fetchOGImageURL(context.Background(), srv.Client(), srv.URL+"/event")
	want := "https://" + u.Host + "/img/x.png"
	if got != want {
		t.Errorf("fetchOGImageURL = %q, want %q", got, want)
	}
}

func TestFetchOGImageURL_RejectsNonHTTPS(t *testing.T) {
	if got := fetchOGImageURL(context.Background(), http.DefaultClient, "http://realdgame.jp/x"); got != "" {
		t.Errorf("http スキーマは弾く想定だが %q を返した", got)
	}
}

func TestFetchOGImageURL_RejectsInvalidURL(t *testing.T) {
	// url.Parse がエラーを返す形（制御文字混入）。
	if got := fetchOGImageURL(context.Background(), http.DefaultClient, "https://example.com/\x7f"); got != "" {
		t.Errorf("不正 URL は弾く想定だが %q を返した", got)
	}
}

func TestFetchOGImageURL_RejectsDisallowedHost(t *testing.T) {
	if got := fetchOGImageURL(context.Background(), http.DefaultClient, "https://evil.example.com/"); got != "" {
		t.Errorf("allowlist 外は弾く想定だが %q を返した", got)
	}
}

func TestFetchOGImageURL_RejectsNon200(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	withAllowedHost(t, stripPort(u.Host))
	if got := fetchOGImageURL(context.Background(), srv.Client(), srv.URL); got != "" {
		t.Errorf("404 は空文字を返す想定だが %q", got)
	}
}

func TestFetchOGImageURL_RejectsNonHTML(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	withAllowedHost(t, stripPort(u.Host))
	if got := fetchOGImageURL(context.Background(), srv.Client(), srv.URL); got != "" {
		t.Errorf("non-HTML は空文字を返す想定だが %q", got)
	}
}

func TestFetchOGImageURL_NoOGImage(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head></head><body></body></html>`))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	withAllowedHost(t, stripPort(u.Host))
	if got := fetchOGImageURL(context.Background(), srv.Client(), srv.URL); got != "" {
		t.Errorf("og:image 無しは空文字を返す想定だが %q", got)
	}
}

func TestFetchOGImageURL_RejectsHTTPImageURL(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><meta property="og:image" content="http://example.com/x.png"></head></html>`))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	withAllowedHost(t, stripPort(u.Host))
	if got := fetchOGImageURL(context.Background(), srv.Client(), srv.URL); got != "" {
		t.Errorf("http の画像 URL は弾く想定だが %q", got)
	}
}

func TestFetchOGImageURL_RejectsTooLongURL(t *testing.T) {
	long := "/img/" + strings.Repeat("a", ogImageURLMaxLen)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><meta property="og:image" content="` + long + `"></head></html>`))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	withAllowedHost(t, stripPort(u.Host))
	if got := fetchOGImageURL(context.Background(), srv.Client(), srv.URL); got != "" {
		t.Errorf("URL 長過大は弾く想定だが %q", got)
	}
}

// stripPort は host:port から host だけを返す。
func stripPort(host string) string {
	if i := strings.IndexByte(host, ':'); i >= 0 {
		return host[:i]
	}
	return host
}

// withAllowedHost は test 期間中だけ allowlist にホストを追加する。
func withAllowedHost(t *testing.T, host string) {
	t.Helper()
	host = strings.ToLower(host)
	original := ogImageAllowedHosts
	clone := make(map[string]struct{}, len(original)+1)
	for k, v := range original {
		clone[k] = v
	}
	clone[host] = struct{}{}
	ogImageAllowedHosts = clone
	t.Cleanup(func() { ogImageAllowedHosts = original })
}
