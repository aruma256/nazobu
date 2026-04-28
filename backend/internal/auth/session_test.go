package auth

import (
	"strings"
	"testing"
)

func TestHashTokenIsDeterministic(t *testing.T) {
	h1 := hashToken("hello")
	h2 := hashToken("hello")
	if h1 != h2 {
		t.Errorf("同じ入力で異なるハッシュが返った: %q vs %q", h1, h2)
	}
	if h := hashToken("hellO"); h == h1 {
		t.Errorf("異なる入力で同一ハッシュ: 衝突 or バグ")
	}
	// SHA-256 hex は 64 文字。schema の CHAR(64) に合わせて長さを固定確認。
	if len(h1) != 64 {
		t.Errorf("ハッシュ長 = %d, want 64", len(h1))
	}
}

func TestGenerateTokenLengthAndUniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 64)
	for i := 0; i < 64; i++ {
		tok, err := generateToken()
		if err != nil {
			t.Fatalf("generateToken: %v", err)
		}
		if tok == "" {
			t.Fatal("generateToken が空文字を返した")
		}
		// 32 byte を base64 raw url にした長さは 43。短縮等の事故を検知するために確認。
		if len(tok) != 43 {
			t.Errorf("token 長 = %d, want 43 (32 bytes raw url base64)", len(tok))
		}
		if strings.ContainsAny(tok, "+/=") {
			t.Errorf("raw url base64 に + / = が含まれている: %q", tok)
		}
		if _, dup := seen[tok]; dup {
			t.Errorf("token 衝突: %q", tok)
		}
		seen[tok] = struct{}{}
	}
}

func TestNullString(t *testing.T) {
	if ns := nullString(""); ns.Valid {
		t.Errorf("空文字は Valid=false であるべき")
	}
	ns := nullString("abc")
	if !ns.Valid || ns.String != "abc" {
		t.Errorf("nullString(\"abc\") = %+v, want {abc true}", ns)
	}
}
