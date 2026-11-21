package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	discordIssuer      = "https://discord.com"
	discordUserInfoURL = "https://discord.com/api/users/@me"
	discordCDNBase     = "https://cdn.discordapp.com"

	ProviderDiscord = "discord"
)

// NewDiscordProvider は OIDC discovery で Discord の provider 情報を取得する。
// client は discovery と、以降の JWKS 取得の両方に使われる。
func NewDiscordProvider(ctx context.Context, client *http.Client) (*oidc.Provider, error) {
	return oidc.NewProvider(oidc.ClientContext(ctx, client), discordIssuer)
}

func DiscordOAuthConfig(provider *oidc.Provider, clientID, clientSecret, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  redirectURL,
		Scopes:       []string{oidc.ScopeOpenID, "identify"},
	}
}

func DiscordIDTokenVerifier(provider *oidc.Provider, clientID string) *oidc.IDTokenVerifier {
	return provider.Verifier(&oidc.Config{ClientID: clientID})
}

// VerifyDiscordUser は token 応答の ID token を検証したうえで /users/@me を取得し、
// 両者の subject が一致することを確認する。
func VerifyDiscordUser(ctx context.Context, client *http.Client, verifier *oidc.IDTokenVerifier, token *oauth2.Token) (*DiscordUser, error) {
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, errors.New("token 応答に id_token が無い")
	}
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("id_token 検証失敗: %w", err)
	}
	du, err := FetchDiscordUser(ctx, client, token)
	if err != nil {
		return nil, err
	}
	if du.ID != idToken.Subject {
		return nil, errors.New("id_token の sub と /users/@me の id が一致しない")
	}
	return du, nil
}

type DiscordUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"global_name"`
	Avatar      string `json:"avatar"`
}

func (du *DiscordUser) ToProfile() UserProfile {
	displayName := du.DisplayName
	if displayName == "" {
		displayName = du.Username
	}
	p := UserProfile{
		DisplayName: displayName,
	}
	if du.Avatar != "" {
		ext := "png"
		// "a_" 接頭辞付きハッシュはアニメーションアバター。
		if strings.HasPrefix(du.Avatar, "a_") {
			ext = "gif"
		}
		p.AvatarURL = fmt.Sprintf("%s/avatars/%s/%s.%s", discordCDNBase, du.ID, du.Avatar, ext)
	}
	return p
}

func FetchDiscordUser(ctx context.Context, client *http.Client, token *oauth2.Token) (*DiscordUser, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discordUserInfoURL, nil)
	if err != nil {
		return nil, err
	}
	token.SetAuthHeader(req)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discord /users/@me: status %d", resp.StatusCode)
	}
	var u DiscordUser
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, fmt.Errorf("discord /users/@me: JSON decode: %w", err)
	}
	if u.ID == "" {
		return nil, fmt.Errorf("discord /users/@me: id が空")
	}
	return &u, nil
}
