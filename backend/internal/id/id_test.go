package id

import (
	"testing"

	"github.com/google/uuid"
)

func TestNew(t *testing.T) {
	s := New()
	// DB の CHAR(36) と同期。ハイフン区切りの標準形式であること。
	if len(s) != 36 {
		t.Errorf("len = %d, want 36: %q", len(s), s)
	}
	u, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("uuid.Parse(%q): %v", s, err)
	}
	if u.Version() != 7 {
		t.Errorf("version = %d, want 7", u.Version())
	}
}

func TestNewIsSortableByTime(t *testing.T) {
	// UUIDv7 の採用理由である「文字列比較 = 採番順」を固定する。
	prev := New()
	for i := 0; i < 100; i++ {
		cur := New()
		if cur <= prev {
			t.Fatalf("採番順に単調増加していない: %q -> %q", prev, cur)
		}
		prev = cur
	}
}
