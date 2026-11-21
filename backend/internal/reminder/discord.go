package reminder

import (
	"context"

	"github.com/aruma256/nazobu/backend/internal/discord"
)

type poster interface {
	post(ctx context.Context, content string, mentionUserIDs []string) error
}

type discordBotPoster struct {
	client    *discord.Client
	channelID string
}

func (d *discordBotPoster) post(ctx context.Context, content string, mentionUserIDs []string) error {
	return d.client.SendMessage(ctx, d.channelID, content, mentionUserIDs)
}
