package server

import (
	"net/http"
	"testing"

	"github.com/aruma256/nazobu/backend/internal/auth"
)

func TestSessionTokenFromHeader(t *testing.T) {
	t.Run("Cookie ヘッダから session token を取り出す", func(t *testing.T) {
		h := http.Header{}
		h.Add("Cookie", auth.SessionCookieName+"=abc123; other=ignored")
		if got := sessionTokenFromHeader(h); got != "abc123" {
			t.Errorf("token = %q, want abc123", got)
		}
	})

	t.Run("Cookie ヘッダ無しは空文字", func(t *testing.T) {
		if got := sessionTokenFromHeader(http.Header{}); got != "" {
			t.Errorf("token = %q, want 空", got)
		}
	})

	t.Run("対象 cookie が無ければ空文字", func(t *testing.T) {
		h := http.Header{}
		h.Add("Cookie", "other=val")
		if got := sessionTokenFromHeader(h); got != "" {
			t.Errorf("token = %q, want 空", got)
		}
	})

	t.Run("複数 Cookie ヘッダから対象だけ拾う", func(t *testing.T) {
		h := http.Header{}
		h.Add("Cookie", "other=val")
		h.Add("Cookie", auth.SessionCookieName+"=tok42")
		if got := sessionTokenFromHeader(h); got != "tok42" {
			t.Errorf("token = %q, want tok42", got)
		}
	})
}
