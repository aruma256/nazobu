package server

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/aruma256/nazobu/backend/internal/auth"
)

const (
	stateCookieName = "nazobu_oauth_state"
	stateCookieTTL  = 10 * time.Minute
	// ログイン完了後にユーザーを戻すパスを保持する一時 cookie。
	nextCookieName = "nazobu_oauth_next"
	nextCookieTTL  = 10 * time.Minute
)

// sanitizeNextPath は ?next= で受け取った遷移先パスを open redirect の
// 踏み台にされないように検証する。許容するのは `/` 始まりかつ `//` で
// 始まらない（プロトコル相対 URL を弾く）内部パスのみ。
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

	if next := sanitizeNextPath(r.URL.Query().Get("next")); next != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     nextCookieName,
			Value:    next,
			Path:     "/",
			MaxAge:   int(nextCookieTTL.Seconds()),
			HttpOnly: true,
			Secure:   s.cfg.CookieSecure,
			SameSite: http.SameSiteLaxMode,
		})
	}

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

	user, err := auth.UpsertUserFromIdentity(ctx, s.db, auth.ProviderDiscord, du.ID, du.ToProfile())
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
		_ = auth.DeleteSession(r.Context(), s.db, c.Value)
	}
	clearCookie(w, auth.SessionCookieName, s.cfg.CookieSecure)
	// POST → GET にしてログイン画面へ。
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
