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
	registerUserDiscordUserID string
	registerUserDisplayName   string
)

var registerUserCmd = &cobra.Command{
	Use:   "register-user",
	Short: "Discord ユーザーを事前登録する（ログインの許可と、ログイン前のメンションを可能にするため）",
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
			Subject:  registerUserDiscordUserID,
		})
		if err == nil {
			slog.ErrorContext(ctx, "user already registered")
			return fmt.Errorf("指定された Discord user ID %s のユーザーは登録済み", registerUserDiscordUserID)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		profile := auth.UserProfile{DisplayName: registerUserDisplayName}
		user, err := auth.UpsertUserFromIdentity(ctx, conn, auth.ProviderDiscord, registerUserDiscordUserID, profile)
		if err != nil {
			return fmt.Errorf("ユーザー登録に失敗: %w", err)
		}

		slog.InfoContext(ctx, "user registered", logging.ID("user_id", user.ID))
		return nil
	},
}

func init() {
	registerUserCmd.Flags().StringVar(&registerUserDiscordUserID, "discord-user-id", "", "Discord ユーザー ID（Snowflake）")
	// display-name は users.display_name の NOT NULL を満たすため必須。実際にログインしてくれば
	// IdP から取得した値で上書きされるが、事前登録の段階ではここで指定したものが表示される。
	registerUserCmd.Flags().StringVar(&registerUserDisplayName, "display-name", "", "表示名（必須）")
	_ = registerUserCmd.MarkFlagRequired("discord-user-id")
	_ = registerUserCmd.MarkFlagRequired("display-name")
}
