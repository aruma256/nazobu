package cmd

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/aruma256/nazobu/backend/internal/auth"
	"github.com/aruma256/nazobu/backend/internal/config"
	"github.com/aruma256/nazobu/backend/internal/db"
	"github.com/spf13/cobra"
)

var (
	addUserDiscordUserID string
	addUserUsername      string
	addUserDisplayName   string
)

var addUserCmd = &cobra.Command{
	Use:   "add-user",
	Short: "Discord ユーザーを手動で先行登録する（OIDC ログイン前にメンション可能にするため）",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Load()
		conn, err := db.Open(cfg.DB)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()

		ctx := cmd.Context()

		var existingUserID string
		err = conn.QueryRowContext(ctx, `
			SELECT user_id FROM discord_identities WHERE discord_user_id = ?
		`, addUserDiscordUserID).Scan(&existingUserID)
		isNew := errors.Is(err, sql.ErrNoRows)
		if err != nil && !isNew {
			return err
		}

		user, err := auth.UpsertUserWithDiscord(ctx, conn, &auth.DiscordUser{
			ID:          addUserDiscordUserID,
			Username:    addUserUsername,
			DisplayName: addUserDisplayName,
		})
		if err != nil {
			return fmt.Errorf("ユーザー登録に失敗: %w", err)
		}

		action := "新規登録"
		if !isNew {
			action = "既存ユーザーを更新"
		}
		fmt.Printf("%s: user_id=%s discord_user_id=%s\n", action, user.ID, user.Discord.DiscordUserID)
		return nil
	},
}

func init() {
	addUserCmd.Flags().StringVar(&addUserDiscordUserID, "discord-user-id", "", "Discord ユーザー ID（Snowflake）")
	addUserCmd.Flags().StringVar(&addUserUsername, "username", "", "Discord のハンドル名（@xxx の xxx）")
	addUserCmd.Flags().StringVar(&addUserDisplayName, "display-name", "", "Discord の表示名（任意）")
	_ = addUserCmd.MarkFlagRequired("discord-user-id")
	_ = addUserCmd.MarkFlagRequired("username")
}
