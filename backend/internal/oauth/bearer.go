package oauth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aruma256/nazobu/backend/internal/auth"
)

var errInvalidAccessToken = errors.New("アクセストークンが無効か期限切れ")

// Middleware は MCP エンドポイント用の Bearer 認証。
// 検証に成功したら auth.User を context に注入して次のハンドラへ渡す。
// 失敗時は 401 + WWW-Authenticate を返す。Claude はこのヘッダの resource_metadata から
// protected resource metadata を発見して OAuth フローを開始する（lazy authentication）。
func (s *Server) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearerToken(r)
		if !ok {
			s.writeUnauthorized(w, "")
			return
		}
		user, scope, err := s.lookupAccessToken(r.Context(), raw)
		if err != nil {
			s.writeUnauthorized(w, "invalid_token")
			return
		}
		ctx := auth.WithUser(r.Context(), user)
		ctx = withGrantedScope(ctx, scope)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type grantedScopeContextKey struct{}

// withGrantedScope はアクセストークンに紐づく scope（空白区切り）を context に載せる。
func withGrantedScope(ctx context.Context, scope string) context.Context {
	return context.WithValue(ctx, grantedScopeContextKey{}, scope)
}

// HasScope は Bearer 認証済み context の granted scope に scope が含まれるかを返す。
// MCP の書き込みツールが ScopeWrite の有無を確認するのに使う。
func HasScope(ctx context.Context, scope string) bool {
	granted, _ := ctx.Value(grantedScopeContextKey{}).(string)
	return scopeContains(granted, scope)
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	scheme, token, found := strings.Cut(h, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

func (s *Server) lookupAccessToken(ctx context.Context, raw string) (*auth.User, string, error) {
	row, err := s.q.GetOAuthTokenUserByAccessTokenHash(ctx, hashToken(raw))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", errInvalidAccessToken
	}
	if err != nil {
		return nil, "", err
	}
	if s.now().After(row.AccessTokenExpiresAt) {
		return nil, "", errInvalidAccessToken
	}
	return &auth.User{
		ID:          row.ID,
		DisplayName: row.DisplayName,
		AvatarURL:   row.AvatarUrl,
		Role:        row.Role,
	}, row.Scope, nil
}

func (s *Server) writeUnauthorized(w http.ResponseWriter, errorCode string) {
	challenge := fmt.Sprintf("Bearer resource_metadata=%q", s.protectedResourceMetadataURL())
	if errorCode != "" {
		challenge += fmt.Sprintf(", error=%q", errorCode)
	}
	w.Header().Set("WWW-Authenticate", challenge)
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
}
