package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	discordUserInfoURL = "https://discord.com/api/users/@me"
	discordCDNBase     = "https://cdn.discordapp.com"

	// ProviderDiscord は user_identities.provider に保存する識別子。
	ProviderDiscord = "discord"
)

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
	ID       string `json:"id"`
	Username string `json:"username"`
	// Discord 公式ドキュメント上の名称は "Display name" だが、API レスポンスの
	// JSON フィールド名は歴史的経緯から global_name のまま。
	DisplayName string `json:"global_name"`
	Avatar      string `json:"avatar"`
}

// ToProfile は Discord の identity を IdP 非依存の UserProfile に変換する。
// avatar はハッシュなので Discord CDN の URL を組み立てる。
// "a_" 接頭辞付きハッシュはアニメーションアバターなので gif、それ以外は png。
func (du *DiscordUser) ToProfile() UserProfile {
	p := UserProfile{
		Username:    du.Username,
		DisplayName: du.DisplayName,
	}
	if du.Avatar != "" {
		ext := "png"
		if strings.HasPrefix(du.Avatar, "a_") {
			ext = "gif"
		}
		p.AvatarURL = fmt.Sprintf("%s/avatars/%s/%s.%s", discordCDNBase, du.ID, du.Avatar, ext)
	}
	return p
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
