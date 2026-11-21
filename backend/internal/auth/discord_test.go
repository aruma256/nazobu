package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

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

// signTestIDToken は RS256 で署名した ID token を組み立てる。
func signTestIDToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signingInput := enc(map[string]string{"alg": "RS256", "typ": "JWT"}) + "." + enc(claims)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("rsa.SignPKCS1v15: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestVerifyDiscordUser(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	verifier := oidc.NewVerifier(discordIssuer, &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}}, &oidc.Config{ClientID: "cid"})
	validClaims := func() map[string]any {
		now := time.Now()
		return map[string]any{
			"iss": discordIssuer,
			"aud": "cid",
			"sub": "111",
			"iat": now.Unix(),
			"exp": now.Add(time.Hour).Unix(),
		}
	}
	tokenWith := func(idToken string) *oauth2.Token {
		tok := &oauth2.Token{AccessToken: "tok", TokenType: "Bearer"}
		if idToken == "" {
			return tok
		}
		return tok.WithExtra(map[string]any{"id_token": idToken})
	}
	meAlice := `{"id":"111","username":"alice"}`

	t.Run("ID token が正当で sub が /users/@me の id と一致すれば DiscordUser を返す", func(t *testing.T) {
		client := fakeDiscordAPI(http.StatusOK, meAlice, nil)
		u, err := VerifyDiscordUser(context.Background(), client, verifier, tokenWith(signTestIDToken(t, key, validClaims())))
		if err != nil {
			t.Fatalf("VerifyDiscordUser: %v", err)
		}
		if u.ID != "111" {
			t.Errorf("ID = %q, want 111", u.ID)
		}
	})

	cases := []struct {
		name    string
		idToken func() string
		me      string
	}{
		{
			name:    "id_token が無ければエラー",
			idToken: func() string { return "" },
			me:      meAlice,
		},
		{
			name:    "別の鍵で署名されていればエラー",
			idToken: func() string { return signTestIDToken(t, otherKey, validClaims()) },
			me:      meAlice,
		},
		{
			name: "aud が自分の client_id でなければエラー",
			idToken: func() string {
				c := validClaims()
				c["aud"] = "other-client"
				return signTestIDToken(t, key, c)
			},
			me: meAlice,
		},
		{
			name: "iss が Discord でなければエラー",
			idToken: func() string {
				c := validClaims()
				c["iss"] = "https://evil.example.com"
				return signTestIDToken(t, key, c)
			},
			me: meAlice,
		},
		{
			name: "期限切れならエラー",
			idToken: func() string {
				c := validClaims()
				c["exp"] = time.Now().Add(-time.Minute).Unix()
				return signTestIDToken(t, key, c)
			},
			me: meAlice,
		},
		{
			name:    "sub と /users/@me の id が食い違えばエラー",
			idToken: func() string { return signTestIDToken(t, key, validClaims()) },
			me:      `{"id":"222","username":"bob"}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := fakeDiscordAPI(http.StatusOK, c.me, nil)
			if _, err := VerifyDiscordUser(context.Background(), client, verifier, tokenWith(c.idToken())); err == nil {
				t.Fatal("err = nil, want error")
			}
		})
	}
}
