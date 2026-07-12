// Package oauth は Claude connector（remote MCP server）連携のための
// OAuth 2.1 認可サーバを提供する。
//
// クライアント登録は CIMD（Client ID Metadata Document）方式のみ対応する。
// client_id はクライアント（Claude）がホストする HTTPS URL で、認可時に
// その URL からメタデータ（redirect_uris 等）を取得して検証する。
// 事前のクライアント登録（DCR）は行わない。
//
// クライアントは public client（token_endpoint_auth_methods_supported: ["none"]）
// として扱い、PKCE S256 を必須とする。
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/aruma256/nazobu/backend/internal/gen/queries"
)

const (
	// 認可コードの有効期限。単回使用なので短くてよい。
	authorizationCodeTTL = 5 * time.Minute
	// アクセストークンの有効期限。切れたら Claude が refresh grant で更新する。
	accessTokenTTL = 1 * time.Hour
	// リフレッシュトークンの有効期限。refresh のたびにローテーション + 期限延長する。
	refreshTokenTTL = 30 * 24 * time.Hour

	// 提供する scope。read は参照系 MCP ツール、write は登録系 MCP ツールに対応する。
	// MCP ツール側が HasScope で write の有無を確認する。
	ScopeRead  = "read"
	ScopeWrite = "write"

	// MCP エンドポイントの公開パス。protected resource metadata の resource と一致させる。
	mcpPath = "/mcp"
)

// Server は CIMD 方式の OAuth 2.1 認可サーバ。
// baseURL（= issuer）は外部公開 origin（例 https://nazobu.aruma256.dev）。
// frontend の rewrites 経由で同一 origin として見えるため FRONTEND_URL を流用する。
type Server struct {
	db           *sql.DB
	q            *queries.Queries
	baseURL      string
	cookieSecure bool
	httpClient   *http.Client
	cimdCache    *cimdCache
	// now はテスト容易性のため差し替え可能にする。本番は time.Now。
	now func() time.Time
	// resolveClient は client_id（URL）から CIMD を取得・検証する処理。
	// 実体は resolveClientMetadata。統合テストでは外部 HTTPS への依存を切るため差し替える。
	resolveClient func(ctx context.Context, clientID string) (*ClientMetadata, error)
}

func NewServer(db *sql.DB, httpClient *http.Client, baseURL string, cookieSecure bool) *Server {
	s := &Server{
		db:           db,
		q:            queries.New(db),
		baseURL:      baseURL,
		cookieSecure: cookieSecure,
		httpClient:   httpClient,
		cimdCache:    newCIMDCache(),
		now:          time.Now,
	}
	s.resolveClient = s.resolveClientMetadata
	return s
}

// ResourceURL は MCP エンドポイントの公開 URL（RFC 8707 の resource 識別子）。
func (s *Server) ResourceURL() string {
	return s.baseURL + mcpPath
}

func (s *Server) protectedResourceMetadataURL() string {
	return s.baseURL + "/.well-known/oauth-protected-resource"
}

// generateToken は認可コード / トークン用の乱数文字列を生成する。
// sessions と同じ 32 byte / base64url。
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken は raw 値の SHA-256 hex を返す。DB には hash のみ保存する。
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// scopeContains は空白区切りの scope 文字列に target が含まれるかを返す。
func scopeContains(scope, target string) bool {
	return slices.Contains(strings.Fields(scope), target)
}
