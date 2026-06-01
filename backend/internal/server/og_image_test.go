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
		{"allowlist 内（www 付きサブドメイン）", "www.scrapmagazine.com", true},
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

func TestExtractOGTags_Image(t *testing.T) {
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
			got := extractOGTags(strings.NewReader(c.html)).Image
			if got != c.want {
				t.Errorf("extractOGTags(...).Image = %q, want %q", got, c.want)
			}
		})
	}
}

func TestExtractOGTags_Description(t *testing.T) {
	cases := []struct {
		name string
		html string
		want string
	}{
		{
			name: "property=og:description を拾う",
			html: `<html><head><meta property="og:description" content="ある夜の脱出"></head><body></body></html>`,
			want: "ある夜の脱出",
		},
		{
			name: "前後空白は trim する",
			html: `<html><head><meta property="og:description" content="  脱出  "></head></html>`,
			want: "脱出",
		},
		{
			name: "og:description が無ければ空文字",
			html: `<html><head><meta property="og:title" content="t"></head></html>`,
			want: "",
		},
		{
			name: "og:image と同居していても両方拾える",
			html: `<html><head><meta property="og:image" content="https://example.com/a.png"><meta property="og:description" content="さあ脱出だ"></head></html>`,
			want: "さあ脱出だ",
		},
		{
			name: "<body> に入った時点で打ち切り",
			html: `<html><head></head><body><meta property="og:description" content="無視されるはず"></body></html>`,
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extractOGTags(strings.NewReader(c.html)).Description
			if got != c.want {
				t.Errorf("extractOGTags(...).Description = %q, want %q", got, c.want)
			}
		})
	}
}

func TestShouldUseOGDescriptionAsCatchphrase(t *testing.T) {
	cases := []struct {
		name        string
		host        string
		description string
		want        bool
	}{
		{"escape.id の個別公演説明は採用", "escape.id", "ある夜の脱出", true},
		{"escape.id でもポート付きホストは正規化して採用", "escape.id:443", "ある夜の脱出", true},
		{"escape.id の汎用サイト説明は除外", "escape.id", escapeIDGenericDescription, false},
		{"description が空なら採用しない", "escape.id", "", false},
		{"escape.id 以外は採用しない", "realdgame.jp", "ある夜の脱出", false},
		{"未知ホストは採用しない", "evil.example.com", "ある夜の脱出", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldUseOGDescriptionAsCatchphrase(c.host, c.description); got != c.want {
				t.Errorf("shouldUseOGDescriptionAsCatchphrase(%q, %q) = %v, want %v", c.host, c.description, got, c.want)
			}
		})
	}
}

func TestApplyOGDescriptionFallback(t *testing.T) {
	cases := []struct {
		name        string
		catchphrase string
		host        string
		description string
		want        string
	}{
		{"web 入力があれば og:description より優先", "手入力のコピー", "escape.id", "ある夜の脱出", "手入力のコピー"},
		{"web 入力が空 + 採用条件成立で og:description を採用", "", "escape.id", "ある夜の脱出", "ある夜の脱出"},
		{"汎用サイト説明は採用しない", "", "escape.id", escapeIDGenericDescription, ""},
		{"escape.id 以外は採用しない", "", "realdgame.jp", "ある夜の脱出", ""},
		{"長さオーバーは採用しない", "", "escape.id", strings.Repeat("a", eventCatchphraseMaxLen+1), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := applyOGDescriptionFallback(c.catchphrase, c.host, c.description); got != c.want {
				t.Errorf("applyOGDescriptionFallback(%q, %q, %q) = %q, want %q", c.catchphrase, c.host, c.description, got, c.want)
			}
		})
	}
}

// fetchOGTags は SSRF 対策として allowlist でホストを絞っているので、
// 実際の testserver は allowlist にないホストになる。allowlist を一時的に
// テストサーバ向けに差し替えて検証する。
func TestFetchOGTags_Success(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><meta property="og:image" content="/img/x.png"><meta property="og:description" content="ある夜の脱出"></head></html>`))
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	host := stripPort(u.Host)
	withAllowedHost(t, host)

	got := fetchOGTags(context.Background(), srv.Client(), srv.URL+"/event")
	wantImage := "https://" + u.Host + "/img/x.png"
	if got.Image != wantImage {
		t.Errorf("fetchOGTags(...).Image = %q, want %q", got.Image, wantImage)
	}
	if got.Description != "ある夜の脱出" {
		t.Errorf("fetchOGTags(...).Description = %q, want %q", got.Description, "ある夜の脱出")
	}
}

func TestFetchOGTags_RejectsNonHTTPS(t *testing.T) {
	if got := fetchOGTags(context.Background(), http.DefaultClient, "http://realdgame.jp/x"); got.Image != "" || got.Description != "" {
		t.Errorf("http スキーマは弾く想定だが %+v を返した", got)
	}
}

func TestFetchOGTags_RejectsInvalidURL(t *testing.T) {
	// url.Parse がエラーを返す形（制御文字混入）。
	if got := fetchOGTags(context.Background(), http.DefaultClient, "https://example.com/\x7f"); got.Image != "" || got.Description != "" {
		t.Errorf("不正 URL は弾く想定だが %+v を返した", got)
	}
}

func TestFetchOGTags_RejectsDisallowedHost(t *testing.T) {
	if got := fetchOGTags(context.Background(), http.DefaultClient, "https://evil.example.com/"); got.Image != "" || got.Description != "" {
		t.Errorf("allowlist 外は弾く想定だが %+v を返した", got)
	}
}

func TestFetchOGTags_RejectsNon200(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	withAllowedHost(t, stripPort(u.Host))
	if got := fetchOGTags(context.Background(), srv.Client(), srv.URL); got.Image != "" || got.Description != "" {
		t.Errorf("404 は空のまま返す想定だが %+v", got)
	}
}

func TestFetchOGTags_RejectsNonHTML(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	withAllowedHost(t, stripPort(u.Host))
	if got := fetchOGTags(context.Background(), srv.Client(), srv.URL); got.Image != "" || got.Description != "" {
		t.Errorf("non-HTML は空のまま返す想定だが %+v", got)
	}
}

func TestFetchOGTags_NoOGTags(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head></head><body></body></html>`))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	withAllowedHost(t, stripPort(u.Host))
	if got := fetchOGTags(context.Background(), srv.Client(), srv.URL); got.Image != "" || got.Description != "" {
		t.Errorf("og タグ無しは空のまま返す想定だが %+v", got)
	}
}

func TestFetchOGTags_RejectsHTTPImageURL(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><meta property="og:image" content="http://example.com/x.png"></head></html>`))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	withAllowedHost(t, stripPort(u.Host))
	if got := fetchOGTags(context.Background(), srv.Client(), srv.URL); got.Image != "" {
		t.Errorf("http の画像 URL は弾く想定だが Image=%q", got.Image)
	}
}

func TestFetchOGTags_RejectsTooLongImageURL(t *testing.T) {
	long := "/img/" + strings.Repeat("a", ogImageURLMaxLen)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><meta property="og:image" content="` + long + `"></head></html>`))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	withAllowedHost(t, stripPort(u.Host))
	if got := fetchOGTags(context.Background(), srv.Client(), srv.URL); got.Image != "" {
		t.Errorf("画像 URL 長過大は弾く想定だが Image=%q", got.Image)
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
