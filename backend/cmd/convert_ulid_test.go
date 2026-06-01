package cmd

import (
	"testing"

	"github.com/google/uuid"
	"github.com/oklog/ulid/v2"
)

func TestUlidToUUIDv7(t *testing.T) {
	src := ulid.Make()
	srcStr := src.String()

	got, err := ulidToUUIDv7(srcStr)
	if err != nil {
		t.Fatalf("変換に失敗: %v", err)
	}

	parsed, err := uuid.Parse(got)
	if err != nil {
		t.Fatalf("結果が UUID として不正: %q: %v", got, err)
	}

	// 正規の UUIDv7 になっていること（version / variant）。
	if parsed.Version() != 7 {
		t.Errorf("version が 7 でない: %d", parsed.Version())
	}
	if parsed.Variant() != uuid.RFC4122 {
		t.Errorf("variant が RFC4122 でない: %v", parsed.Variant())
	}

	// 先頭 48bit（タイムスタンプ）が ULID と一致していること。
	for i := 0; i < 6; i++ {
		if parsed[i] != src[i] {
			t.Errorf("タイムスタンプ %d バイト目が不一致: uuid=%#x ulid=%#x", i, parsed[i], src[i])
		}
	}
}

func TestUlidToUUIDv7_Deterministic(t *testing.T) {
	srcStr := ulid.Make().String()
	a, err := ulidToUUIDv7(srcStr)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ulidToUUIDv7(srcStr)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("同じ ULID から異なる UUID が生成された: %q != %q", a, b)
	}
}

func TestUlidToUUIDv7_Idempotent(t *testing.T) {
	once, err := ulidToUUIDv7(ulid.Make().String())
	if err != nil {
		t.Fatal(err)
	}
	// 既に UUID 化済みの値を渡しても変化しないこと（再実行の安全性）。
	twice, err := ulidToUUIDv7(once)
	if err != nil {
		t.Fatalf("変換済みの値の再変換に失敗: %v", err)
	}
	if once != twice {
		t.Errorf("冪等でない: %q != %q", once, twice)
	}
}

func TestUlidToUUIDv7_Distinct(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 1000; i++ {
		got, err := ulidToUUIDv7(ulid.Make().String())
		if err != nil {
			t.Fatal(err)
		}
		if _, dup := seen[got]; dup {
			t.Fatalf("異なる ULID が同じ UUID に衝突した: %q", got)
		}
		seen[got] = struct{}{}
	}
}

func TestUlidToUUIDv7_Invalid(t *testing.T) {
	if _, err := ulidToUUIDv7("not-a-valid-id"); err == nil {
		t.Error("不正な値でエラーにならなかった")
	}
}
