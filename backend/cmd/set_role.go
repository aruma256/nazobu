package cmd

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/aruma256/nazobu/backend/internal/auth"
	"github.com/aruma256/nazobu/backend/internal/config"
	"github.com/aruma256/nazobu/backend/internal/db"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/spf13/cobra"
)

var (
	setRoleDiscordUserID string
	setRoleRole          string
)

var setRoleCmd = &cobra.Command{
	Use:   "set-role",
	Short: "既存ユーザーの role を変更する（admin / member）",
	RunE: func(cmd *cobra.Command, args []string) error {
		switch setRoleRole {
		case auth.RoleAdmin, auth.RoleMember:
		default:
			return fmt.Errorf("--role は %q もしくは %q のみ受け付ける（指定値: %q）", auth.RoleAdmin, auth.RoleMember, setRoleRole)
		}

		cfg := config.Load()
		conn, err := db.Open(cfg.DB)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()

		ctx := cmd.Context()
		q := queries.New(conn)

		userID, err := q.GetUserIDByIdentity(ctx, queries.GetUserIDByIdentityParams{
			Provider: auth.ProviderDiscord,
			Subject:  setRoleDiscordUserID,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("Discord user ID %s に対応するユーザーが未登録（先に add-user で登録するか、本人に一度ログインしてもらう必要がある）", setRoleDiscordUserID)
		}
		if err != nil {
			return err
		}

		if err := q.UpdateUserRole(ctx, queries.UpdateUserRoleParams{
			Role: setRoleRole,
			ID:   userID,
		}); err != nil {
			return fmt.Errorf("role の更新に失敗: %w", err)
		}

		fmt.Printf("role を更新: user_id=%s provider=%s subject=%s role=%s\n", userID, auth.ProviderDiscord, setRoleDiscordUserID, setRoleRole)
		return nil
	},
}

func init() {
	setRoleCmd.Flags().StringVar(&setRoleDiscordUserID, "discord-user-id", "", "Discord ユーザー ID（Snowflake）")
	setRoleCmd.Flags().StringVar(&setRoleRole, "role", "", "付与する role（admin もしくは member）")
	_ = setRoleCmd.MarkFlagRequired("discord-user-id")
	_ = setRoleCmd.MarkFlagRequired("role")
}
