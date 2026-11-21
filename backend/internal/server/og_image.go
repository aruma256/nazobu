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

var ogImageAllowedHosts = map[string]struct{}{
	"realdgame.jp":          {},
	"escape.id":             {},
	"www.scrapmagazine.com": {},
}

const (
	ogFetchTimeout      = 5 * time.Second
	ogFetchMaxBodyBytes = 1 << 20
	ogImageURLMaxLen    = 2048
)

const escapeIDGenericDescription = "閉じ込められたいあなたのための脱出・謎解きポータルサイト"

type ogTags struct {
	Image       string
	Description string
}

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
	h := strings.ToLower(host)
	if i := strings.IndexByte(h, ':'); i >= 0 {
		h = h[:i]
	}
	_, ok := ogImageAllowedHosts[h]
	return ok
}

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
