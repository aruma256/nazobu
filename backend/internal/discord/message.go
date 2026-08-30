package discord

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type allowedMentions struct {
	// Parse を空配列にすることで @everyone / @here / ロールメンションを許可しない。
	Parse []string `json:"parse"`
	// Users に列挙した user ID だけを実際の通知対象にする。
	Users []string `json:"users"`
}

type createMessageRequest struct {
	Content         string          `json:"content"`
	AllowedMentions allowedMentions `json:"allowed_mentions"`
}

type messageResponse struct {
	ID string `json:"id"`
}

// MessageConfigured は Bot 認証と投稿先チャンネルの設定が揃っているかを返す。
// guild ID やネタバレカテゴリ ID はメッセージ投稿には不要なので判定に含めない。
func (c *Client) MessageConfigured(channelID string) bool {
	return c != nil && c.botToken != "" && strings.TrimSpace(channelID) != ""
}

// SendMessage は bot として指定チャンネルへメッセージを投稿する。
// ユーザー入力に紛れた意図しないメンションを防ぐため、通知対象は mentionUserIDs に限定する。
func (c *Client) SendMessage(ctx context.Context, channelID, content string, mentionUserIDs []string) error {
	channelID = strings.TrimSpace(channelID)
	if !c.MessageConfigured(channelID) {
		return fmt.Errorf("Discord bot メッセージ投稿設定が未完了")
	}

	payload := createMessageRequest{
		Content: content,
		AllowedMentions: allowedMentions{
			Parse: []string{},
			Users: append([]string{}, mentionUserIDs...),
		},
	}
	var created messageResponse
	path := "/channels/" + url.PathEscape(channelID) + "/messages"
	if err := c.doJSON(ctx, http.MethodPost, path, payload, &created); err != nil {
		return err
	}
	if created.ID == "" {
		return fmt.Errorf("Discord のメッセージ作成応答に id が無い")
	}
	return nil
}
