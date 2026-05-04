package server

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/aruma256/nazobu/backend/internal/auth"
)

func TestCanEditTicket(t *testing.T) {
	admin := &auth.User{ID: "u-admin", Role: auth.RoleAdmin}
	member := &auth.User{ID: "u-member", Role: auth.RoleMember}
	other := &auth.User{ID: "u-other", Role: auth.RoleMember}

	if !canEditTicket(admin, "anyone") {
		t.Errorf("admin はいつでも編集可")
	}
	if !canEditTicket(member, member.ID) {
		t.Errorf("立替者本人は編集可")
	}
	if canEditTicket(other, member.ID) {
		t.Errorf("admin でもなく立替者でもないユーザは編集不可")
	}
}

func TestFormatJSTDateTime(t *testing.T) {
	// UTC 2026-05-04T00:00:00Z は JST 2026-05-04T09:00:00+09:00
	in := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	got := formatJSTDateTime(in)
	want := "2026-05-04T09:00:00+09:00"
	if got != want {
		t.Errorf("formatJSTDateTime = %q, want %q", got, want)
	}
}

func TestFormatNullableJSTDateTime(t *testing.T) {
	if got := formatNullableJSTDateTime(sql.NullTime{}); got != "" {
		t.Errorf("Invalid なら空文字想定だが %q", got)
	}
	in := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	got := formatNullableJSTDateTime(sql.NullTime{Time: in, Valid: true})
	want := "2026-05-04T21:00:00+09:00"
	if got != want {
		t.Errorf("formatNullableJSTDateTime = %q, want %q", got, want)
	}
}

func TestParseRequiredJSTDateTime(t *testing.T) {
	t.Run("RFC3339 を JST に正規化する", func(t *testing.T) {
		got, err := parseRequiredJSTDateTime("2026-05-04T00:00:00Z", "start_at")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		want := time.Date(2026, 5, 4, 9, 0, 0, 0, jst)
		if !got.Equal(want) {
			t.Errorf("got = %v, want %v", got, want)
		}
		if got.Location().String() != jst.String() {
			t.Errorf("location = %v, want JST", got.Location())
		}
	})

	t.Run("空文字は InvalidArgument", func(t *testing.T) {
		_, err := parseRequiredJSTDateTime("   ", "start_at")
		assertConnectCode(t, err, connect.CodeInvalidArgument)
	})

	t.Run("RFC3339 でなければ InvalidArgument", func(t *testing.T) {
		_, err := parseRequiredJSTDateTime("not a date", "start_at")
		assertConnectCode(t, err, connect.CodeInvalidArgument)
	})
}

func TestParseNullableJSTDateTime(t *testing.T) {
	t.Run("空文字は NULL", func(t *testing.T) {
		got, err := parseNullableJSTDateTime("  ", "meeting_at")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if got.Valid {
			t.Errorf("Valid = true, want false")
		}
	})

	t.Run("RFC3339 を JST で valid に", func(t *testing.T) {
		got, err := parseNullableJSTDateTime("2026-05-04T01:23:45+09:00", "meeting_at")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !got.Valid {
			t.Fatalf("Valid = false")
		}
		want := time.Date(2026, 5, 4, 1, 23, 45, 0, jst)
		if !got.Time.Equal(want) {
			t.Errorf("got = %v, want %v", got.Time, want)
		}
	})

	t.Run("不正値は InvalidArgument", func(t *testing.T) {
		_, err := parseNullableJSTDateTime("xxx", "meeting_at")
		assertConnectCode(t, err, connect.CodeInvalidArgument)
	})
}

func TestDedupeStrings(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"空入力は空", nil, []string{}},
		{"重複を除く（最初の出現順を保つ）", []string{"a", "b", "a", "c"}, []string{"a", "b", "c"}},
		{"前後空白は trim、空文字は捨てる", []string{" a ", "", "  ", "a"}, []string{"a"}},
		{"全て同じなら 1 件", []string{"x", "x", "x"}, []string{"x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := dedupeStrings(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("dedupeStrings(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func assertConnectCode(t *testing.T, err error, want connect.Code) {
	t.Helper()
	if err == nil {
		t.Fatal("err = nil, want connect error")
	}
	var cerr *connect.Error
	if !errors.As(err, &cerr) {
		t.Fatalf("err is not *connect.Error: %T (%v)", err, err)
	}
	if cerr.Code() != want {
		t.Errorf("code = %v, want %v", cerr.Code(), want)
	}
}
