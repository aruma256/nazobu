package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSanitizeNextPath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"/events", "/events"},
		{"/events?id=1", "/events?id=1"},
		{"//evil.example.com/", ""},        // プロトコル相対 URL の弾き
		{"https://evil.example.com/", ""},  // 絶対 URL の弾き
		{"events", ""},                     // / 始まりでない
		{"javascript:alert(1)", ""},        // スキーマ付き
		{"/", "/"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := sanitizeNextPath(c.in); got != c.want {
				t.Errorf("sanitizeNextPath(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestGenerateRandomString(t *testing.T) {
	t.Run("base64 raw url 形式で指定 byte 数を符号化", func(t *testing.T) {
		s, err := generateRandomString(32)
		if err != nil {
			t.Fatalf("generateRandomString: %v", err)
		}
		// 32 bytes -> base64 raw url で 43 文字
		if len(s) != 43 {
			t.Errorf("len = %d, want 43", len(s))
		}
		if strings.ContainsAny(s, "+/=") {
			t.Errorf("raw url base64 に + / = が含まれてはならない: %q", s)
		}
	})

	t.Run("呼び出すたびに異なる値", func(t *testing.T) {
		seen := map[string]struct{}{}
		for i := 0; i < 16; i++ {
			s, err := generateRandomString(16)
			if err != nil {
				t.Fatalf("generateRandomString: %v", err)
			}
			if _, ok := seen[s]; ok {
				t.Errorf("衝突: %q", s)
			}
			seen[s] = struct{}{}
		}
	})
}

func TestClearCookie(t *testing.T) {
	for _, secure := range []bool{true, false} {
		t.Run("", func(t *testing.T) {
			rec := httptest.NewRecorder()
			clearCookie(rec, "test_cookie", secure)

			res := rec.Result()
			defer func() { _ = res.Body.Close() }()

			cookies := res.Cookies()
			if len(cookies) != 1 {
				t.Fatalf("Set-Cookie = %d 個, want 1 (%+v)", len(cookies), cookies)
			}
			c := cookies[0]
			if c.Name != "test_cookie" {
				t.Errorf("Name = %q", c.Name)
			}
			if c.Value != "" {
				t.Errorf("Value = %q, want 空", c.Value)
			}
			if c.MaxAge != -1 {
				t.Errorf("MaxAge = %d, want -1", c.MaxAge)
			}
			if !c.HttpOnly {
				t.Errorf("HttpOnly = false, want true")
			}
			if c.Secure != secure {
				t.Errorf("Secure = %v, want %v", c.Secure, secure)
			}
			if c.Path != "/" {
				t.Errorf("Path = %q, want /", c.Path)
			}
			if c.SameSite != http.SameSiteLaxMode {
				t.Errorf("SameSite = %v, want Lax", c.SameSite)
			}
		})
	}
}
