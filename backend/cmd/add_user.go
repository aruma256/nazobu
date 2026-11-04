package cmd

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/aruma256/nazobu/backend/internal/auth"
	"github.com/aruma256/nazobu/backend/internal/config"
	"github.com/aruma256/nazobu/backend/internal/db"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/aruma256/nazobu/backend/internal/logging"
	"github.com/spf13/cobra"
)

var (
	addUserDiscordUserID string
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

		_, err = queries.New(conn).GetUserIDByIdentity(ctx, queries.GetUserIDByIdentityParams{
			Provider: auth.ProviderDiscord,
			Subject:  addUserDiscordUserID,
		})
		if err == nil {
			slog.ErrorContext(ctx, "user already registered")
			return fmt.Errorf("指定された Discord user ID %s のユーザーは登録済み", addUserDiscordUserID)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		profile := auth.UserProfile{DisplayName: addUserDisplayName}
		user, err := auth.UpsertUserFromIdentity(ctx, conn, auth.ProviderDiscord, addUserDiscordUserID, profile)
		if err != nil {
			return fmt.Errorf("ユーザー登録に失敗: %w", err)
		}

		slog.InfoContext(ctx, "user added", logging.ID("user_id", user.ID))
		return nil
	},
}

func init() {
	addUserCmd.Flags().StringVar(&addUserDiscordUserID, "discord-user-id", "", "Discord ユーザー ID（Snowflake）")
	// display-name は users.display_name の NOT NULL を満たすため必須。実際にログインしてくれば
	// IdP から取得した値で上書きされるが、先行登録の段階ではここで指定したものが表示される。
	addUserCmd.Flags().StringVar(&addUserDisplayName, "display-name", "", "表示名（必須）")
	_ = addUserCmd.MarkFlagRequired("discord-user-id")
	_ = addUserCmd.MarkFlagRequired("display-name")
}
