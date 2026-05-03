package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// OG 画像取得対象のドメイン allowlist。
// SSRF 対策の本丸。リダイレクト先もこの allowlist で再検査するため、
// allowlist 外への bounce で内部ホストに到達することはない。
var ogImageAllowedHosts = map[string]struct{}{
	"realdgame.jp": {},
	"escape.id":    {},
}

const (
	ogFetchTimeout      = 5 * time.Second
	ogFetchMaxBodyBytes = 1 << 20 // 1 MiB。HTML head までで十分。
	ogImageURLMaxLen    = 2048
)

// fetchOGImageURL は eventURL のページから og:image を 1 件取り出して返す。
// 失敗（allowlist 外 / ネットワーク / パース失敗 / og:image 無し）時は ("", nil)。
// caller は失敗を区別せず NULL として保存する仕様なので error は返さない。
func fetchOGImageURL(ctx context.Context, client *http.Client, eventURL string) string {
	parsed, err := url.Parse(eventURL)
	if err != nil || parsed.Scheme != "https" {
		return ""
	}
	if !isAllowedOGHost(parsed.Host) {
		return ""
	}

	ctx, cancel := context.WithTimeout(ctx, ogFetchTimeout)
	defer cancel()

	// リダイレクトは毎回 allowlist で再検査する。
	// http.Client が default で nil の場合は up to 10 回フォローするため、CheckRedirect を上書きする。
	c := *client
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("リダイレクト回数上限")
		}
		if req.URL.Scheme != "https" || !isAllowedOGHost(req.URL.Host) {
			return errors.New("リダイレクト先が allowlist 外")
		}
		return nil
	}
	c.Timeout = ogFetchTimeout

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, eventURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "nazobu-og-fetcher/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := c.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return ""
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(ct), "text/html") {
		return ""
	}

	limited := io.LimitReader(resp.Body, ogFetchMaxBodyBytes)
	imgURL := extractOGImage(limited)
	if imgURL == "" {
		return ""
	}

	// 相対 URL を絶対化し、画像 URL も https のみ許容。
	abs, err := parsed.Parse(imgURL)
	if err != nil {
		return ""
	}
	if abs.Scheme != "https" {
		return ""
	}
	s := abs.String()
	if len(s) > ogImageURLMaxLen {
		return ""
	}
	return s
}

func isAllowedOGHost(host string) bool {
	// :port が付いていれば落とす。allowlist は ASCII ホストのみなので case-fold で揃える。
	h := strings.ToLower(host)
	if i := strings.IndexByte(h, ':'); i >= 0 {
		h = h[:i]
	}
	_, ok := ogImageAllowedHosts[h]
	return ok
}

// extractOGImage は HTML を読み <meta property="og:image" content="..."> を探す。
// 見つからなければ空文字。<body> に入った時点で打ち切り（OG タグは <head> 内なので）。
func extractOGImage(r io.Reader) string {
	z := html.NewTokenizer(r)
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return ""
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			if tag == "body" {
				return ""
			}
			if tag != "meta" || !hasAttr {
				continue
			}
			var prop, content string
			for {
				k, v, more := z.TagAttr()
				switch strings.ToLower(string(k)) {
				case "property", "name":
					if prop == "" {
						prop = string(v)
					}
				case "content":
					content = string(v)
				}
				if !more {
					break
				}
			}
			if strings.EqualFold(prop, "og:image") && content != "" {
				return content
			}
		}
	}
}
