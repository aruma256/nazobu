package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/aruma256/nazobu/backend/internal/auth"
)

const (
	stateCookieName = "nazobu_oauth_state"
	stateCookieTTL  = 10 * time.Minute
)

func (s *Server) handleDiscordLogin(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Discord.ClientID == "" {
		http.Error(w, "Discord client_id が未設定", http.StatusServiceUnavailable)
		return
	}
	state, err := generateRandomString(32)
	if err != nil {
		http.Error(w, "state の生成に失敗", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   int(stateCookieTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, s.discordOAuth.AuthCodeURL(state), http.StatusFound)
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

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "code が無い", http.StatusBadRequest)
		return
	}

	token, err := s.discordOAuth.Exchange(ctx, code)
	if err != nil {
		http.Error(w, "token exchange 失敗: "+err.Error(), http.StatusBadGateway)
		return
	}

	du, err := auth.FetchDiscordUser(ctx, s.httpClient, token)
	if err != nil {
		http.Error(w, "discord user 取得失敗: "+err.Error(), http.StatusBadGateway)
		return
	}

	user, err := auth.UpsertUserWithDiscord(ctx, s.db, du)
	if err != nil {
		http.Error(w, "user upsert 失敗: "+err.Error(), http.StatusInternalServerError)
		return
	}

	rawToken, err := auth.CreateSession(ctx, s.db, user.ID)
	if err != nil {
		http.Error(w, "session 作成失敗: "+err.Error(), http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    rawToken,
		Path:     "/",
		MaxAge:   int(auth.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, s.cfg.FrontendURL, http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.SessionCookieName); err == nil && c.Value != "" {
		_ = auth.DeleteSession(r.Context(), s.db, c.Value)
	}
	clearCookie(w, auth.SessionCookieName, s.cfg.CookieSecure)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(auth.SessionCookieName)
	if err != nil || c.Value == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	user, err := auth.LookupSession(r.Context(), s.db, c.Value)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type discordJSON struct {
		UserID      string  `json:"user_id"`
		Username    string  `json:"username"`
		DisplayName *string `json:"display_name"`
		Avatar      *string `json:"avatar"`
	}
	type meJSON struct {
		ID      string      `json:"id"`
		Discord discordJSON `json:"discord"`
	}
	out := meJSON{
		ID: user.ID,
		Discord: discordJSON{
			UserID:   user.Discord.DiscordUserID,
			Username: user.Discord.Username,
		},
	}
	if user.Discord.DisplayName.Valid {
		v := user.Discord.DisplayName.String
		out.Discord.DisplayName = &v
	}
	if user.Discord.Avatar.Valid {
		v := user.Discord.Avatar.String
		out.Discord.Avatar = &v
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
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
