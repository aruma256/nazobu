package auth

import "testing"

func TestDiscordUserToProfile(t *testing.T) {
	cases := []struct {
		name      string
		du        DiscordUser
		wantURL   string
		wantUser  string
		wantDisp  string
	}{
		{
			name:     "アバターなしなら avatar_url は空",
			du:       DiscordUser{ID: "111", Username: "alice", DisplayName: "Alice", Avatar: ""},
			wantURL:  "",
			wantUser: "alice",
			wantDisp: "Alice",
		},
		{
			name:     "通常アバターは png",
			du:       DiscordUser{ID: "222", Username: "bob", Avatar: "abcdef0123"},
			wantURL:  "https://cdn.discordapp.com/avatars/222/abcdef0123.png",
			wantUser: "bob",
		},
		{
			name:     "a_ 接頭辞ハッシュはアニメーションなので gif",
			du:       DiscordUser{ID: "333", Username: "carol", Avatar: "a_999888"},
			wantURL:  "https://cdn.discordapp.com/avatars/333/a_999888.gif",
			wantUser: "carol",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := c.du.ToProfile()
			if p.AvatarURL != c.wantURL {
				t.Errorf("AvatarURL = %q, want %q", p.AvatarURL, c.wantURL)
			}
			if p.Username != c.wantUser {
				t.Errorf("Username = %q, want %q", p.Username, c.wantUser)
			}
			if p.DisplayName != c.wantDisp {
				t.Errorf("DisplayName = %q, want %q", p.DisplayName, c.wantDisp)
			}
		})
	}
}
