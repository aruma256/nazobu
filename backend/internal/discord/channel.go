// Package discord は Discord HTTP API との連携を提供する。
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const (
	discordAPIBaseURL = "https://discord.com/api/v10"
	requestTimeout    = 10 * time.Second
	viewChannel       = int64(1 << 10)
)

// Client はネタバレチャンネルの作成とメンバー権限付与に必要な
// Discord API だけを扱う。Bot token はリクエストヘッダー以外に出さない。
type Client struct {
	httpClient *http.Client
	apiBaseURL string
	botToken   string
	guildID    string
	categoryID string
}

// NewClient は本番 Discord API 用の client を返す。
func NewClient(httpClient *http.Client, botToken, guildID, categoryID string) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		httpClient: httpClient,
		apiBaseURL: discordAPIBaseURL,
		botToken:   strings.TrimSpace(botToken),
		guildID:    strings.TrimSpace(guildID),
		categoryID: strings.TrimSpace(categoryID),
	}
}

// Configured はチャンネル操作に必要な設定が揃っているかを返す。
func (c *Client) Configured() bool {
	return c != nil && c.botToken != "" && c.guildID != "" && c.categoryID != ""
}

// ChannelURL は Discord client でチャンネルを開く URL を返す。
func (c *Client) ChannelURL(channelID string) string {
	if !c.Configured() || strings.TrimSpace(channelID) == "" {
		return ""
	}
	return "https://discord.com/channels/" + url.PathEscape(c.guildID) + "/" + url.PathEscape(channelID)
}

type permissionOverwrite struct {
	ID    string `json:"id"`
	Type  int    `json:"type"`
	Allow string `json:"allow"`
	Deny  string `json:"deny"`
}

type createChannelRequest struct {
	Name                 string                `json:"name"`
	Type                 int                   `json:"type"`
	Topic                string                `json:"topic"`
	ParentID             string                `json:"parent_id"`
	PermissionOverwrites []permissionOverwrite `json:"permission_overwrites"`
}

type channelResponse struct {
	ID                   string                `json:"id"`
	PermissionOverwrites []permissionOverwrite `json:"permission_overwrites"`
}

// CreateSpoilerChannel は @everyone から隠し、指定メンバーだけが閲覧できる
// テキストチャンネルを設定済みカテゴリ配下に作成する。
func (c *Client) CreateSpoilerChannel(ctx context.Context, name, topic string, memberIDs []string) (string, error) {
	if !c.Configured() {
		return "", fmt.Errorf("Discord ネタバレチャンネル設定が未完了")
	}
	members := uniqueSorted(memberIDs)
	overwrites := make([]permissionOverwrite, 0, len(members)+1)
	// @everyone ロールの id は guild id と同じ。VIEW_CHANNEL だけを明示的に拒否する。
	overwrites = append(overwrites, permissionOverwrite{
		ID: c.guildID, Type: 0, Allow: "0", Deny: fmt.Sprint(viewChannel),
	})
	for _, memberID := range members {
		overwrites = append(overwrites, permissionOverwrite{
			ID: memberID, Type: 1, Allow: fmt.Sprint(viewChannel), Deny: "0",
		})
	}

	payload := createChannelRequest{
		Name:                 name,
		Type:                 0, // GUILD_TEXT
		Topic:                topic,
		ParentID:             c.categoryID,
		PermissionOverwrites: overwrites,
	}
	var created channelResponse
	if err := c.doJSON(ctx, http.MethodPost, "/guilds/"+url.PathEscape(c.guildID)+"/channels", payload, &created); err != nil {
		return "", err
	}
	if created.ID == "" {
		return "", fmt.Errorf("Discord のチャンネル作成応答に id が無い")
	}
	return created.ID, nil
}

// GrantMembersView は指定メンバーの既存 overwrite を保ったまま VIEW_CHANNEL を追加する。
// 対象外メンバーの overwrite は削除しない。
func (c *Client) GrantMembersView(ctx context.Context, channelID string, memberIDs []string) error {
	if !c.Configured() {
		return fmt.Errorf("Discord ネタバレチャンネル設定が未完了")
	}
	members := uniqueSorted(memberIDs)
	if len(members) == 0 {
		return nil
	}

	var channel channelResponse
	if err := c.doJSON(ctx, http.MethodGet, "/channels/"+url.PathEscape(channelID), nil, &channel); err != nil {
		return err
	}
	existing := make(map[string]permissionOverwrite, len(channel.PermissionOverwrites))
	for _, overwrite := range channel.PermissionOverwrites {
		if overwrite.Type == 1 {
			existing[overwrite.ID] = overwrite
		}
	}

	for _, memberID := range members {
		overwrite := existing[memberID]
		overwrite.ID = memberID
		overwrite.Type = 1
		allow, err := permissionInt(overwrite.Allow)
		if err != nil {
			return fmt.Errorf("Discord allow permission の解析に失敗: %w", err)
		}
		deny, err := permissionInt(overwrite.Deny)
		if err != nil {
			return fmt.Errorf("Discord deny permission の解析に失敗: %w", err)
		}
		view := big.NewInt(viewChannel)
		alreadyAllowed := new(big.Int).And(new(big.Int).Set(allow), view).Sign() != 0
		viewDenied := new(big.Int).And(new(big.Int).Set(deny), view).Sign() != 0
		if alreadyAllowed && !viewDenied {
			continue
		}
		allow.Or(allow, view)
		deny.AndNot(deny, view)

		payload := permissionOverwrite{Type: 1, Allow: allow.String(), Deny: deny.String()}
		path := "/channels/" + url.PathEscape(channelID) + "/permissions/" + url.PathEscape(memberID)
		if err := c.doJSON(ctx, http.MethodPut, path, payload, nil); err != nil {
			return err
		}
	}
	return nil
}

// DeleteChannel は DB 保存前に作成が失敗したチャンネルの補償削除用。
func (c *Client) DeleteChannel(ctx context.Context, channelID string) error {
	if !c.Configured() {
		return fmt.Errorf("Discord ネタバレチャンネル設定が未完了")
	}
	return c.doJSON(ctx, http.MethodDelete, "/channels/"+url.PathEscape(channelID), nil, nil)
}

func (c *Client) doJSON(ctx context.Context, method, path string, payload, out any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("Discord request の JSON 生成に失敗: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, method, strings.TrimRight(c.apiBaseURL, "/")+path, body)
	if err != nil {
		return fmt.Errorf("Discord request の生成に失敗: %w", err)
	}
	req.Header.Set("Authorization", "Bot "+c.botToken)
	req.Header.Set("User-Agent", "nazobu/discord-channel")
	req.Header.Set("X-Audit-Log-Reason", "nazobu spoiler channel")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Discord API request に失敗: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("Discord API が異常応答: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("Discord API response の JSON 解析に失敗: %w", err)
	}
	return nil
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func permissionInt(raw string) (*big.Int, error) {
	if raw == "" {
		return new(big.Int), nil
	}
	value, ok := new(big.Int).SetString(raw, 10)
	if !ok || value.Sign() < 0 {
		return nil, fmt.Errorf("不正な permission 値 %q", raw)
	}
	return value, nil
}
