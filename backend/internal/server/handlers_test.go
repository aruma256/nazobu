package server

import (
	"strings"
	"testing"
)

func TestSanitizeNextPath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"空文字は空", "", ""},
		{"ルート / は許可", "/", "/"},
		{"通常の内部パスは許可", "/events", "/events"},
		{"クエリ付き内部パスは許可", "/events?id=1", "/events?id=1"},
		{"スラッシュ始まりでないものは弾く", "events", ""},
		{"プロトコル相対 // は弾く（open redirect 対策）", "//evil.com", ""},
		{"プロトコル相対 //path も弾く", "//evil.com/path", ""},
		{"スキーム付き絶対 URL は弾く", "https://evil.com", ""},
		{"http:// も弾く", "http://evil.com", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sanitizeNextPath(c.in); got != c.want {
				t.Errorf("sanitizeNextPath(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestGenerateRandomString(t *testing.T) {
	const byteLen = 32
	seen := make(map[string]struct{}, 16)
	for i := 0; i < 16; i++ {
		s, err := generateRandomString(byteLen)
		if err != nil {
			t.Fatalf("generateRandomString: %v", err)
		}
		// 32 byte を raw url base64 にした長さは 43。
		if len(s) != 43 {
			t.Errorf("len = %d, want 43", len(s))
		}
		// raw url base64 はパディングや + / を含まない。
		if strings.ContainsAny(s, "+/=") {
			t.Errorf("raw url base64 に + / = が含まれる: %q", s)
		}
		if _, dup := seen[s]; dup {
			t.Errorf("値が衝突: %q", s)
		}
		seen[s] = struct{}{}
	}
}

func TestGenerateRandomStringDifferentLengths(t *testing.T) {
	cases := []struct {
		byteLen int
		wantLen int
	}{
		// raw url base64 のエンコード後長: ceil(n*4/3)
		{16, 22},
		{24, 32},
		{32, 43},
	}
	for _, c := range cases {
		s, err := generateRandomString(c.byteLen)
		if err != nil {
			t.Fatalf("byteLen=%d: %v", c.byteLen, err)
		}
		if len(s) != c.wantLen {
			t.Errorf("byteLen=%d: len = %d, want %d", c.byteLen, len(s), c.wantLen)
		}
	}
}
