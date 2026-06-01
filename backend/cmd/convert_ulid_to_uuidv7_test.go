package cmd

import (
	"testing"

	"github.com/google/uuid"
	"github.com/oklog/ulid/v2"
)

func TestUlidStringToUUIDv7(t *testing.T) {
	src := ulid.Make()

	got, err := ulidStringToUUIDv7(src.String())
	if err != nil {
		t.Fatalf("変換に失敗: %v", err)
	}

	parsed, err := uuid.Parse(got)
	if err != nil {
		t.Fatalf("変換結果が UUID として不正: %v", err)
	}

	// valid な UUIDv7 であること（version=7, variant=RFC4122）。
	if parsed.Version() != 7 {
		t.Errorf("version が 7 でない: %d", parsed.Version())
	}
	if parsed.Variant() != uuid.RFC4122 {
		t.Errorf("variant が RFC4122 でない: %v", parsed.Variant())
	}

	// 先頭 48bit のミリ秒タイムスタンプが ULID と一致すること。
	srcBytes := [16]byte(src)
	gotBytes := [16]byte(parsed)
	for i := 0; i < 6; i++ {
		if srcBytes[i] != gotBytes[i] {
			t.Errorf("timestamp byte[%d] が不一致: ulid=%#x uuid=%#x", i, srcBytes[i], gotBytes[i])
		}
	}

	// 決定論的であること（再変換で同じ値）。
	again, err := ulidStringToUUIDv7(src.String())
	if err != nil {
		t.Fatalf("再変換に失敗: %v", err)
	}
	if again != got {
		t.Errorf("決定論的でない: %q != %q", again, got)
	}
}

func TestUlidStringToUUIDv7_PreservesSortOrder(t *testing.T) {
	// ULID は辞書順 = 時刻順。変換後の UUIDv7 も同じ順序を保つことを確認する。
	a := ulid.MustParse("01000000000000000000000000")
	b := ulid.MustParse("01000000010000000000000000")

	ua, err := ulidStringToUUIDv7(a.String())
	if err != nil {
		t.Fatal(err)
	}
	ub, err := ulidStringToUUIDv7(b.String())
	if err != nil {
		t.Fatal(err)
	}
	if !(ua < ub) {
		t.Errorf("ソート順が保たれていない: %q >= %q", ua, ub)
	}
}

func TestUlidStringToUUIDv7_InvalidInput(t *testing.T) {
	if _, err := ulidStringToUUIDv7("not-a-ulid"); err == nil {
		t.Error("不正な入力でエラーにならなかった")
	}
}
