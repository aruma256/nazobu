package cmd

import (
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/aruma256/nazobu/backend/internal/config"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "schema.sql を sqldef で DB に適用する（宣言型マイグレーション）",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Load()

		// FK に使われている列の型変更は MySQL の error 1833 で弾かれるため、--before-apply で FOREIGN_KEY_CHECKS=0 を流す。
		mysqldef := exec.Command(
			"mysqldef",
			"--host="+cfg.DB.Host,
			"--port="+cfg.DB.Port,
			"--user="+cfg.DB.User,
			"--password="+cfg.DB.Password,
			"--file="+cfg.SchemaPath,
			"--apply",
			"--enable-drop",
			"--before-apply=SET FOREIGN_KEY_CHECKS=0;",
			cfg.DB.Name,
		)
		slog.InfoContext(cmd.Context(), "migration started")
		if err := mysqldef.Run(); err != nil {
			return fmt.Errorf("mysqldef 実行失敗: %w", err)
		}
		slog.InfoContext(cmd.Context(), "migration completed")
		return nil
	},
}
