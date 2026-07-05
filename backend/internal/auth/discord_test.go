package auth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestDiscordUserToProfile(t *testing.T) {
	cases := []struct {
		name     string
		du       DiscordUser
		wantURL  string
		wantDisp string
	}{
		{
			name:     "アバターなしなら avatar_url は空",
			du:       DiscordUser{ID: "111", Username: "alice", DisplayName: "Alice", Avatar: ""},
			wantURL:  "",
			wantDisp: "Alice",
		},
		{
			name:     "通常アバターは png",
			du:       DiscordUser{ID: "222", Username: "bob", Avatar: "abcdef0123"},
			wantURL:  "https://cdn.discordapp.com/avatars/222/abcdef0123.png",
			wantDisp: "bob",
		},
		{
			name:     "a_ 接頭辞ハッシュはアニメーションなので gif",
			du:       DiscordUser{ID: "333", Username: "carol", Avatar: "a_999888"},
			wantURL:  "https://cdn.discordapp.com/avatars/333/a_999888.gif",
			wantDisp: "carol",
		},
		{
			name:     "global_name が空なら username にフォールバック",
			du:       DiscordUser{ID: "444", Username: "dave", DisplayName: "", Avatar: ""},
			wantURL:  "",
			wantDisp: "dave",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := c.du.ToProfile()
			if p.AvatarURL != c.wantURL {
				t.Errorf("AvatarURL = %q, want %q", p.AvatarURL, c.wantURL)
			}
			if p.DisplayName != c.wantDisp {
				t.Errorf("DisplayName = %q, want %q", p.DisplayName, c.wantDisp)
			}
		})
	}
}

// roundTripFunc は Transport を関数で差し替えるためのアダプタ。
// FetchDiscordUser は Discord の実 URL を固定で叩くため、httptest ではなく
// Transport 層で応答を偽装する。
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// fakeDiscordAPI は指定 status / body を返す client を作り、受け取ったリクエストを *got に記録する。
func fakeDiscordAPI(status int, body string, got **http.Request) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got != nil {
			*got = r
		}
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}
}

func TestFetchDiscordUser(t *testing.T) {
	token := &oauth2.Token{AccessToken: "tok", TokenType: "Bearer"}

	t.Run("access token 付きで /users/@me を叩き JSON を DiscordUser に写す", func(t *testing.T) {
		var got *http.Request
		client := fakeDiscordAPI(http.StatusOK,
			`{"id":"111","username":"alice","global_name":"Alice","avatar":"abc123"}`, &got)

		u, err := FetchDiscordUser(context.Background(), client, token)
		if err != nil {
			t.Fatalf("FetchDiscordUser: %v", err)
		}
		if got.URL.String() != discordUserInfoURL {
			t.Errorf("URL = %q, want %q", got.URL.String(), discordUserInfoURL)
		}
		if auth := got.Header.Get("Authorization"); auth != "Bearer tok" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer tok")
		}
		want := DiscordUser{ID: "111", Username: "alice", DisplayName: "Alice", Avatar: "abc123"}
		if *u != want {
			t.Errorf("user = %+v, want %+v", *u, want)
		}
	})

	t.Run("非 200 はエラー", func(t *testing.T) {
		client := fakeDiscordAPI(http.StatusUnauthorized, `{}`, nil)
		if _, err := FetchDiscordUser(context.Background(), client, token); err == nil {
			t.Fatal("err = nil, want error")
		}
	})

	t.Run("id が空ならエラー（identity として信用しない）", func(t *testing.T) {
		client := fakeDiscordAPI(http.StatusOK, `{"username":"alice"}`, nil)
		if _, err := FetchDiscordUser(context.Background(), client, token); err == nil {
			t.Fatal("err = nil, want error")
		}
	})

	t.Run("JSON でない応答はエラー", func(t *testing.T) {
		client := fakeDiscordAPI(http.StatusOK, `<html>maintenance</html>`, nil)
		if _, err := FetchDiscordUser(context.Background(), client, token); err == nil {
			t.Fatal("err = nil, want error")
		}
	})
}
