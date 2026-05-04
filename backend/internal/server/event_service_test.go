package server

import (
	"database/sql"
	"testing"

	"connectrpc.com/connect"
)

func ptrInt32(v int32) *int32 { return &v }

func TestValidateMinutesBefore(t *testing.T) {
	t.Run("nil なら NULL", func(t *testing.T) {
		got, err := validateMinutesBefore(nil, "field")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if got.Valid {
			t.Errorf("Valid = true, want false")
		}
	})

	t.Run("0 以上は valid", func(t *testing.T) {
		got, err := validateMinutesBefore(ptrInt32(30), "field")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !got.Valid || got.Int32 != 30 {
			t.Errorf("got = %+v, want {30 true}", got)
		}
	})

	t.Run("0 もそのまま受け付ける", func(t *testing.T) {
		got, err := validateMinutesBefore(ptrInt32(0), "field")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !got.Valid || got.Int32 != 0 {
			t.Errorf("got = %+v, want {0 true}", got)
		}
	})

	t.Run("負の値は InvalidArgument", func(t *testing.T) {
		_, err := validateMinutesBefore(ptrInt32(-1), "doors_open_minutes_before")
		assertConnectCode(t, err, connect.CodeInvalidArgument)
	})
}

func TestNullInt32ToPtr(t *testing.T) {
	if got := nullInt32ToPtr(sql.NullInt32{}); got != nil {
		t.Errorf("Invalid からは nil 想定だが %v", *got)
	}
	got := nullInt32ToPtr(sql.NullInt32{Int32: 42, Valid: true})
	if got == nil || *got != 42 {
		t.Errorf("got = %v, want *int32(42)", got)
	}
}

func TestNullStringToString(t *testing.T) {
	if got := nullStringToString(sql.NullString{}); got != "" {
		t.Errorf("Invalid からは空文字想定だが %q", got)
	}
	if got := nullStringToString(sql.NullString{String: "abc", Valid: true}); got != "abc" {
		t.Errorf("got = %q", got)
	}
}

func TestStringToNullString(t *testing.T) {
	if got := stringToNullString(""); got.Valid {
		t.Errorf("空文字は Invalid 想定だが Valid=true")
	}
	got := stringToNullString("xyz")
	if !got.Valid || got.String != "xyz" {
		t.Errorf("got = %+v, want {xyz true}", got)
	}
}
