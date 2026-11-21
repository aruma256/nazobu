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
	authorizationCodeTTL = 5 * time.Minute
	accessTokenTTL       = 1 * time.Hour
	refreshTokenTTL      = 30 * 24 * time.Hour

	ScopeRead  = "read"
	ScopeWrite = "write"

	mcpPath = "/mcp"
)

type Server struct {
	db            *sql.DB
	q             *queries.Queries
	baseURL       string
	cookieSecure  bool
	httpClient    *http.Client
	cimdCache     *cimdCache
	now           func() time.Time
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

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func scopeContains(scope, target string) bool {
	return slices.Contains(strings.Fields(scope), target)
}
