package reminder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// webhook POST のタイムアウト。
const postTimeout = 10 * time.Second

// poster は整形済み本文とメンション対象を Discord に投稿する。テストで差し替える。
type poster interface {
	post(ctx context.Context, content string, mentionUserIDs []string) error
}

// discordWebhook は Discord の Incoming Webhook へ POST する poster 実装。
type discordWebhook struct {
	url    string
	client *http.Client
}

type webhookPayload struct {
	Content         string          `json:"content"`
	AllowedMentions allowedMentions `json:"allowed_mentions"`
}

// allowedMentions で実際に通知されるメンションを明示的に絞る。
// Parse を空配列にすることで @everyone / @here / ロールメンションを一切許可せず、
// Users に列挙した user id だけを通知する（公演タイトル等に紛れた文字列での誤爆を防ぐ）。
type allowedMentions struct {
	Parse []string `json:"parse"`
	Users []string `json:"users"`
}

func (d *discordWebhook) post(ctx context.Context, content string, mentionUserIDs []string) error {
	body, err := json.Marshal(webhookPayload{
		Content: content,
		AllowedMentions: allowedMentions{
			Parse: []string{},
			Users: mentionUserIDs,
		},
	})
	if err != nil {
		return fmt.Errorf("payload の marshal に失敗: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, postTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("リクエスト生成に失敗: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook への POST に失敗: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		// エラー本文の先頭だけ拾ってログ向けに返す。
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("webhook が異常応答: status=%d body=%s", resp.StatusCode, string(b))
	}
	// 接続を再利用できるよう本文を読み捨てる（成功時は 204 で空のことが多い）。
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	return nil
}
