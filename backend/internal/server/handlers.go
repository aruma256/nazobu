package server

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/aruma256/nazobu/backend/internal/auth"
	"github.com/aruma256/nazobu/backend/internal/logging"
)

const (
	stateCookieName    = "nazobu_oauth_state"
	pkceCookieName     = "nazobu_oauth_pkce"
	nextCookieName     = "nazobu_oauth_next"
	oauthFlowCookieTTL = 10 * time.Minute
)

func sanitizeNextPath(raw string) string {
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(raw, "/") {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return ""
	}
	return raw
}

func (s *Server) handleDiscordLogin(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Discord.ClientID == "" {
		slog.ErrorContext(r.Context(), "login failed", "stage", "configuration")
		http.Error(w, "Discord client_id が未設定", http.StatusServiceUnavailable)
		return
	}
	state, err := generateRandomString(32)
	if err != nil {
		slog.ErrorContext(r.Context(), "login failed", "stage", "generate_state")
		http.Error(w, "state の生成に失敗", http.StatusInternalServerError)
		return
	}
	verifier := oauth2.GenerateVerifier()
	s.setOAuthFlowCookie(w, stateCookieName, state)
	s.setOAuthFlowCookie(w, pkceCookieName, verifier)
	if next := sanitizeNextPath(r.URL.Query().Get("next")); next != "" {
		s.setOAuthFlowCookie(w, nextCookieName, next)
	}

	http.Redirect(w, r, s.discordOAuth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (s *Server) setOAuthFlowCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(oauthFlowCookieTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) handleDiscordCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || stateCookie.Value == "" {
		http.Error(w, "state cookie が無い", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("state") != stateCookie.Value {
		http.Error(w, "state が一致しない", http.StatusBadRequest)
		return
	}
	clearCookie(w, stateCookieName, s.cfg.CookieSecure)

	pkceCookie, err := r.Cookie(pkceCookieName)
	if err != nil || pkceCookie.Value == "" {
		http.Error(w, "PKCE cookie が無い", http.StatusBadRequest)
		return
	}
	clearCookie(w, pkceCookieName, s.cfg.CookieSecure)

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "code が無い", http.StatusBadRequest)
		return
	}

	token, err := s.discordOAuth.Exchange(ctx, code, oauth2.VerifierOption(pkceCookie.Value))
	if err != nil {
		slog.ErrorContext(r.Context(), "login failed", "stage", "exchange_token")
		http.Error(w, "token exchange 失敗: "+err.Error(), http.StatusBadGateway)
		return
	}

	du, err := auth.VerifyDiscordUser(ctx, s.httpClient, s.discordIDTokenVerifier, token)
	if err != nil {
		slog.ErrorContext(r.Context(), "login failed", "stage", "verify_user")
		http.Error(w, "discord user 取得失敗: "+err.Error(), http.StatusBadGateway)
		return
	}

	user, err := auth.LoginWithIdentity(ctx, s.db, auth.ProviderDiscord, du.ID, du.ToProfile())
	if err != nil {
		if errors.Is(err, auth.ErrUserNotRegistered) {
			clearCookie(w, nextCookieName, s.cfg.CookieSecure)
			http.Redirect(w, r, s.cfg.FrontendURL+"/login?error=not_registered", http.StatusFound)
			return
		}
		slog.ErrorContext(r.Context(), "login failed", "stage", "lookup_user")
		http.Error(w, "user lookup 失敗: "+err.Error(), http.StatusInternalServerError)
		return
	}

	rawToken, err := auth.CreateSession(ctx, s.db, user.ID)
	if err != nil {
		slog.ErrorContext(r.Context(), "login failed", "stage", "create_session")
		http.Error(w, "session 作成失敗: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.InfoContext(ctx, "login completed", logging.ID("user_id", user.ID))
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    rawToken,
		Path:     "/",
		MaxAge:   int(auth.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})

	dest := s.cfg.FrontendURL
	if c, err := r.Cookie(nextCookieName); err == nil {
		if next := sanitizeNextPath(c.Value); next != "" {
			dest = s.cfg.FrontendURL + next
		}
		clearCookie(w, nextCookieName, s.cfg.CookieSecure)
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.SessionCookieName); err == nil && c.Value != "" {
		if err := auth.DeleteSession(r.Context(), s.db, c.Value); err != nil {
			slog.ErrorContext(r.Context(), "logout failed", "stage", "delete_session")
		}
	}
	clearCookie(w, auth.SessionCookieName, s.cfg.CookieSecure)
	http.Redirect(w, r, s.cfg.FrontendURL+"/login", http.StatusSeeOther)
}

func clearCookie(w http.ResponseWriter, name string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func generateRandomString(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
