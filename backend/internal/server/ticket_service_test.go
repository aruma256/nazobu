package server

import (
	"reflect"
	"testing"

	"github.com/aruma256/nazobu/backend/internal/auth"
)

func TestCanEditTicket(t *testing.T) {
	cases := []struct {
		name        string
		user        *auth.User
		purchasedBy string
		want        bool
	}{
		{
			name:        "admin は他人の ticket でも編集可",
			user:        &auth.User{ID: "u1", Role: auth.RoleAdmin},
			purchasedBy: "other",
			want:        true,
		},
		{
			name:        "立替者本人 (member) は編集可",
			user:        &auth.User{ID: "u1", Role: auth.RoleMember},
			purchasedBy: "u1",
			want:        true,
		},
		{
			name:        "他人の member は編集不可",
			user:        &auth.User{ID: "u1", Role: auth.RoleMember},
			purchasedBy: "u2",
			want:        false,
		},
		{
			name:        "admin かつ立替者本人なら当然編集可",
			user:        &auth.User{ID: "u1", Role: auth.RoleAdmin},
			purchasedBy: "u1",
			want:        true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := canEditTicket(c.user, c.purchasedBy); got != c.want {
				t.Errorf("canEditTicket = %v, want %v", got, c.want)
			}
		})
	}
}

func TestFormatMeetingTime(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"MySQL の HH:MM:SS は HH:MM に丸める", "10:30:00", "10:30"},
		{"秒が 0 以外でも丸める", "09:05:30", "09:05"},
		{"23:59:59 の境界", "23:59:59", "23:59"},
		{"既に HH:MM ならそのまま", "10:30", "10:30"},
		{"短すぎる入力はフォールバックでそのまま", "1:2", "1:2"},
		{"空文字はそのまま", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatMeetingTime(c.in); got != c.want {
				t.Errorf("formatMeetingTime(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestDedupeStrings(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil は空 slice", nil, []string{}},
		{"空 slice は空 slice", []string{}, []string{}},
		{"重複なしは順序保持", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"重複は除去（先勝ち）", []string{"a", "b", "a", "c", "b"}, []string{"a", "b", "c"}},
		{"前後空白は trim", []string{" a ", "b "}, []string{"a", "b"}},
		{"空文字 / 空白のみは除去", []string{"", "  ", "a"}, []string{"a"}},
		{"trim 後に重複したものも除去", []string{"a", " a "}, []string{"a"}},
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
