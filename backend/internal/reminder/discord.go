package reminder

import (
	"context"

	"github.com/aruma256/nazobu/backend/internal/discord"
)

// poster は整形済み本文とメンション対象を Discord に投稿する。テストで差し替える。
type poster interface {
	post(ctx context.Context, content string, mentionUserIDs []string) error
}

// discordBotPoster は投稿先チャンネルを束縛し、共通 Discord client を poster として使う。
type discordBotPoster struct {
	client    *discord.Client
	channelID string
}

func (d *discordBotPoster) post(ctx context.Context, content string, mentionUserIDs []string) error {
	return d.client.SendMessage(ctx, d.channelID, content, mentionUserIDs)
}
