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
	"realdgame.jp":          {},
	"escape.id":             {},
	"www.scrapmagazine.com": {},
}

const (
	ogFetchTimeout      = 5 * time.Second
	ogFetchMaxBodyBytes = 1 << 20 // 1 MiB。HTML head までで十分。
	ogImageURLMaxLen    = 2048
)

// escape.id のトップページ等が返す汎用サイト説明。
// 個別公演ページではないため、これをキャッチコピーとして採用してはいけない。
const escapeIDGenericDescription = "閉じ込められたいあなたのための脱出・謎解きポータルサイト"

// ogTags は対象ページから取り出せた og:image / og:description を保持する。
// 取得できなかった項目は空文字。
type ogTags struct {
	// 絶対化済みの https URL。長さオーバーや非 https の場合は空文字。
	Image string
	// raw な og:description（前後空白は trim 済み）。
	Description string
}

// fetchOGTags は eventURL のページから og:image / og:description を 1 リクエストで取り出す。
// 失敗（allowlist 外 / ネットワーク / パース失敗 / 該当タグ無し）時は対応フィールドが空文字。
// caller は失敗を区別せず空のまま保存する仕様なので error は返さない。
func fetchOGTags(ctx context.Context, client *http.Client, eventURL string) ogTags {
	parsed, err := url.Parse(eventURL)
	if err != nil || parsed.Scheme != "https" {
		return ogTags{}
	}
	if !isAllowedOGHost(parsed.Host) {
		return ogTags{}
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
		return ogTags{}
	}
	req.Header.Set("User-Agent", "nazobu-og-fetcher/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := c.Do(req)
	if err != nil {
		return ogTags{}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return ogTags{}
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(ct), "text/html") {
		return ogTags{}
	}

	limited := io.LimitReader(resp.Body, ogFetchMaxBodyBytes)
	tags := extractOGTags(limited)

	if tags.Image != "" {
		// 相対 URL を絶対化し、画像 URL も https のみ許容。長さオーバーや非 https は弾く。
		abs, err := parsed.Parse(tags.Image)
		if err != nil || abs.Scheme != "https" {
			tags.Image = ""
		} else if s := abs.String(); len(s) > ogImageURLMaxLen {
			tags.Image = ""
		} else {
			tags.Image = s
		}
	}
	return tags
}

// shouldUseOGDescriptionAsCatchphrase は og:description を event のキャッチコピーに採用するかを判定する。
// 公演単位の固有説明が取れるのは現状 escape.id のみのため、ホストを escape.id に限定する。
// またトップページ等が返す汎用サイト説明は採用しない。
func shouldUseOGDescriptionAsCatchphrase(host, description string) bool {
	if description == "" {
		return false
	}
	h := strings.ToLower(host)
	if i := strings.IndexByte(h, ':'); i >= 0 {
		h = h[:i]
	}
	if h != "escape.id" {
		return false
	}
	return description != escapeIDGenericDescription
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

// extractOGTags は HTML を読み <meta property="og:image" / "og:description" content="..."> を探す。
// 見つからない項目は空文字のまま。<body> に入った時点で打ち切り（OG タグは <head> 内なので）。
// og:description の前後空白は trim する。
func extractOGTags(r io.Reader) ogTags {
	var tags ogTags
	z := html.NewTokenizer(r)
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return tags
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			if tag == "body" {
				return tags
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
			if content == "" {
				continue
			}
			switch {
			case strings.EqualFold(prop, "og:image") && tags.Image == "":
				tags.Image = content
			case strings.EqualFold(prop, "og:description") && tags.Description == "":
				tags.Description = strings.TrimSpace(content)
			}
			if tags.Image != "" && tags.Description != "" {
				return tags
			}
		}
	}
}
