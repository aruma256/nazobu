package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/aruma256/nazobu/backend/internal/id"
)

// tokenResponse は RFC 6749 5.1 のトークンレスポンス。
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// tokenError は RFC 6749 5.2 のエラーレスポンス。
// Claude は refresh 失敗時に invalid_grant であることを期待するため、コードは仕様準拠を守る。
type tokenError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// HandleToken は /oauth/token（POST, application/x-www-form-urlencoded）。
// public client（クライアント認証なし）を前提に authorization_code / refresh_token grant を処理する。
func (s *Server) HandleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeTokenError(w, http.StatusBadRequest, "invalid_request", "フォームの解析に失敗")
		return
	}
	switch r.PostFormValue("grant_type") {
	case "authorization_code":
		s.handleAuthorizationCodeGrant(w, r)
	case "refresh_token":
		s.handleRefreshTokenGrant(w, r)
	default:
		writeTokenError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type は authorization_code か refresh_token")
	}
}

func (s *Server) handleAuthorizationCodeGrant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	code := r.PostFormValue("code")
	clientID := r.PostFormValue("client_id")
	redirectURI := r.PostFormValue("redirect_uri")
	verifier := r.PostFormValue("code_verifier")
	if code == "" || clientID == "" || verifier == "" {
		writeTokenError(w, http.StatusBadRequest, "invalid_request", "code / client_id / code_verifier は必須")
		return
	}

	row, err := s.q.GetOAuthAuthorizationCodeByHash(ctx, hashToken(code))
	if errors.Is(err, sql.ErrNoRows) {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "認可コードが無効")
		return
	}
	if err != nil {
		writeTokenError(w, http.StatusInternalServerError, "server_error", "認可コードの取得に失敗")
		return
	}

	// 単回使用: 検証より先に削除し、並行リクエストによる二重使用を影響行数で弾く。
	deleted, err := s.q.DeleteOAuthAuthorizationCode(ctx, row.ID)
	if err != nil {
		writeTokenError(w, http.StatusInternalServerError, "server_error", "認可コードの消費に失敗")
		return
	}
	if deleted == 0 {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "認可コードは使用済み")
		return
	}

	if s.now().After(row.ExpiresAt) {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "認可コードの期限切れ")
		return
	}
	if row.ClientID != clientID {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "client_id が一致しない")
		return
	}
	// redirect_uri は認可時に使った値との完全一致（RFC 6749 4.1.3）。
	if row.RedirectUri != redirectURI {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri が一致しない")
		return
	}
	if !verifyPKCE(verifier, row.CodeChallenge) {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "code_verifier の検証に失敗")
		return
	}

	accessToken, refreshToken, err := generateTokenPair()
	if err != nil {
		writeTokenError(w, http.StatusInternalServerError, "server_error", "トークンの生成に失敗")
		return
	}
	if err := s.q.CreateOAuthToken(ctx, queries.CreateOAuthTokenParams{
		ID:                    id.New(),
		UserID:                row.UserID,
		ClientID:              clientID,
		Scope:                 row.Scope,
		AccessTokenHash:       hashToken(accessToken),
		AccessTokenExpiresAt:  s.now().Add(accessTokenTTL),
		RefreshTokenHash:      hashToken(refreshToken),
		RefreshTokenExpiresAt: s.now().Add(refreshTokenTTL),
	}); err != nil {
		writeTokenError(w, http.StatusInternalServerError, "server_error", "トークンの保存に失敗")
		return
	}

	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int64(accessTokenTTL.Seconds()),
		RefreshToken: refreshToken,
		Scope:        row.Scope,
	})
}

func (s *Server) handleRefreshTokenGrant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	refreshToken := r.PostFormValue("refresh_token")
	clientID := r.PostFormValue("client_id")
	if refreshToken == "" || clientID == "" {
		writeTokenError(w, http.StatusBadRequest, "invalid_request", "refresh_token / client_id は必須")
		return
	}

	row, err := s.q.GetOAuthTokenByRefreshTokenHash(ctx, hashToken(refreshToken))
	if errors.Is(err, sql.ErrNoRows) {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "refresh_token が無効")
		return
	}
	if err != nil {
		writeTokenError(w, http.StatusInternalServerError, "server_error", "refresh_token の取得に失敗")
		return
	}
	if row.ClientID != clientID {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "client_id が一致しない")
		return
	}
	if s.now().After(row.RefreshTokenExpiresAt) {
		_ = s.q.DeleteOAuthTokenByID(ctx, row.ID)
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "refresh_token の期限切れ")
		return
	}

	// public client のためリフレッシュトークンは毎回ローテーションする（OAuth 2.1）。
	// 旧トークンの無効化と新トークンの発行は同一 UPDATE で行う。
	newAccessToken, newRefreshToken, err := generateTokenPair()
	if err != nil {
		writeTokenError(w, http.StatusInternalServerError, "server_error", "トークンの生成に失敗")
		return
	}
	if err := s.q.RotateOAuthToken(ctx, queries.RotateOAuthTokenParams{
		ID:                    row.ID,
		AccessTokenHash:       hashToken(newAccessToken),
		AccessTokenExpiresAt:  s.now().Add(accessTokenTTL),
		RefreshTokenHash:      hashToken(newRefreshToken),
		RefreshTokenExpiresAt: s.now().Add(refreshTokenTTL),
	}); err != nil {
		writeTokenError(w, http.StatusInternalServerError, "server_error", "トークンの更新に失敗")
		return
	}

	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken:  newAccessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int64(accessTokenTTL.Seconds()),
		RefreshToken: newRefreshToken,
		Scope:        row.Scope,
	})
}

// verifyPKCE は S256 で code_verifier を検証する（RFC 7636）。
func verifyPKCE(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}

func generateTokenPair() (accessToken, refreshToken string, err error) {
	accessToken, err = generateToken()
	if err != nil {
		return "", "", err
	}
	refreshToken, err = generateToken()
	if err != nil {
		return "", "", err
	}
	return accessToken, refreshToken, nil
}

func writeTokenError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(tokenError{Error: code, ErrorDescription: description})
}
