package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const discordUserInfoURL = "https://discord.com/api/users/@me"

// Discord は OIDC discovery エンドポイント（/.well-known/openid-configuration）に
// 対応していないので、Provider 設定を手組みする。
// ID token も実質的に使わず、access token で /users/@me を叩いて identity を取得する方針。
func NewDiscordProvider(ctx context.Context) *oidc.Provider {
	cfg := oidc.ProviderConfig{
		IssuerURL:   "https://discord.com",
		AuthURL:     "https://discord.com/api/oauth2/authorize",
		TokenURL:    "https://discord.com/api/oauth2/token",
		UserInfoURL: discordUserInfoURL,
	}
	return cfg.NewProvider(ctx)
}

func DiscordOAuthConfig(provider *oidc.Provider, clientID, clientSecret, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  redirectURL,
		// Discord で profile 取得に必要な最小 scope。email も取らない。
		Scopes: []string{"identify"},
	}
}

type DiscordUser struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
	Avatar     string `json:"avatar"`
}

// Discord の /users/@me を access token 付きで叩いて identity を取得する。
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
	defer resp.Body.Close()
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
