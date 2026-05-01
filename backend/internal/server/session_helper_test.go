package server

import (
	"net/http"
	"testing"

	"github.com/aruma256/nazobu/backend/internal/auth"
)

func TestSessionTokenFromHeader(t *testing.T) {
	cases := []struct {
		name      string
		cookieHdr string
		want      string
	}{
		{
			name:      "Cookie ヘッダ無しなら空文字",
			cookieHdr: "",
			want:      "",
		},
		{
			name:      "session cookie 単独",
			cookieHdr: auth.SessionCookieName + "=tok123",
			want:      "tok123",
		},
		{
			name:      "他の cookie と並んでいても拾える",
			cookieHdr: "foo=bar; " + auth.SessionCookieName + "=tok456; baz=qux",
			want:      "tok456",
		},
		{
			name:      "対象 cookie が無いなら空文字",
			cookieHdr: "foo=bar; baz=qux",
			want:      "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := http.Header{}
			if c.cookieHdr != "" {
				h.Set("Cookie", c.cookieHdr)
			}
			if got := sessionTokenFromHeader(h); got != c.want {
				t.Errorf("sessionTokenFromHeader = %q, want %q", got, c.want)
			}
		})
	}
}
