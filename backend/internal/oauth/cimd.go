package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// CIMD 取得のタイムアウト。Claude 側は認可フロー全体で 10 秒程度しか待たないため短めにする。
	cimdFetchTimeout = 5 * time.Second
	// CIMD ドキュメントのサイズ上限。正常なメタデータは数 KB で収まる。
	cimdMaxBodySize = 64 * 1024
	// 取得済みメタデータのキャッシュ期間。GET → POST（同意）で二度引くのを避ける。
	cimdCacheTTL = 5 * time.Minute
)

// ClientMetadata は CIMD（Client ID Metadata Document）の必要フィールドのみを表す。
// 例: Claude Code は https://claude.ai/oauth/claude-code-client-metadata を client_id として名乗る。
type ClientMetadata struct {
	ClientID     string   `json:"client_id"`
	ClientName   string   `json:"client_name"`
	ClientURI    string   `json:"client_uri"`
	RedirectURIs []string `json:"redirect_uris"`
}

// parseClientIDURL は client_id が CIMD として妥当な URL かを検証する。
// HTTPS のみ許可し、fragment 付きや、ループバック / プライベートアドレスへの
// 取得（SSRF の踏み台化）を弾く。
func parseClientIDURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("client_id が URL として不正: %w", err)
	}
	if u.Scheme != "https" {
		return nil, errors.New("client_id は https URL のみ")
	}
	if u.Host == "" {
		return nil, errors.New("client_id に host が無い")
	}
	if u.Fragment != "" {
		return nil, errors.New("client_id に fragment は使えない")
	}
	host := u.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".local") {
		return nil, errors.New("client_id にローカルホストは使えない")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
		return nil, errors.New("client_id にプライベートアドレスは使えない")
	}
	return u, nil
}

// fetchClientMetadata は client_id URL から CIMD を取得して検証する。
func fetchClientMetadata(ctx context.Context, client *http.Client, clientID string) (*ClientMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, cimdFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("CIMD の取得に失敗: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CIMD の取得に失敗: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, cimdMaxBodySize))
	if err != nil {
		return nil, fmt.Errorf("CIMD の読み込みに失敗: %w", err)
	}

	var meta ClientMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, fmt.Errorf("CIMD が JSON として不正: %w", err)
	}
	// ドキュメント側の client_id は自身の URL と一致していなければならない（なりすまし防止）。
	if meta.ClientID != "" && meta.ClientID != clientID {
		return nil, errors.New("CIMD の client_id が URL と一致しない")
	}
	if len(meta.RedirectURIs) == 0 {
		return nil, errors.New("CIMD に redirect_uris が無い")
	}
	meta.ClientID = clientID
	return &meta, nil
}

// resolveClientMetadata は検証済み client_id の CIMD をキャッシュ経由で取得する。
func (s *Server) resolveClientMetadata(ctx context.Context, clientID string) (*ClientMetadata, error) {
	if _, err := parseClientIDURL(clientID); err != nil {
		return nil, err
	}
	if meta := s.cimdCache.get(clientID, s.now()); meta != nil {
		return meta, nil
	}
	meta, err := fetchClientMetadata(ctx, s.httpClient, clientID)
	if err != nil {
		return nil, err
	}
	s.cimdCache.put(clientID, meta, s.now())
	return meta, nil
}

// redirectURIAllowed は要求された redirect_uri が CIMD の redirect_uris に含まれるかを判定する。
// 原則は完全一致。例外として、ループバック（http://localhost / http://127.0.0.1）は
// RFC 8252 7.3 に従い port を無視して比較する（Claude Code はセッションごとに ephemeral port を使う）。
func redirectURIAllowed(meta *ClientMetadata, requested string) bool {
	for _, registered := range meta.RedirectURIs {
		if redirectURIMatches(registered, requested) {
			return true
		}
	}
	return false
}

func redirectURIMatches(registered, requested string) bool {
	if registered == requested {
		return true
	}
	ru, err := url.Parse(registered)
	if err != nil {
		return false
	}
	qu, err := url.Parse(requested)
	if err != nil {
		return false
	}
	if ru.Scheme != "http" || qu.Scheme != "http" {
		return false
	}
	if !isLoopbackHost(ru.Hostname()) || ru.Hostname() != qu.Hostname() {
		return false
	}
	return ru.Path == qu.Path && ru.RawQuery == qu.RawQuery
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1"
}

// isLoopbackRedirectURI は同意画面での警告表示用（MCP 仕様の推奨事項）。
func isLoopbackRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "http" && isLoopbackHost(u.Hostname())
}

type cimdCacheEntry struct {
	meta      *ClientMetadata
	fetchedAt time.Time
}

type cimdCache struct {
	mu      sync.Mutex
	entries map[string]cimdCacheEntry
}

func newCIMDCache() *cimdCache {
	return &cimdCache{entries: map[string]cimdCacheEntry{}}
}

func (c *cimdCache) get(clientID string, now time.Time) *ClientMetadata {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[clientID]
	if !ok || now.Sub(e.fetchedAt) > cimdCacheTTL {
		return nil
	}
	return e.meta
}

func (c *cimdCache) put(clientID string, meta *ClientMetadata, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[clientID] = cimdCacheEntry{meta: meta, fetchedAt: now}
}
