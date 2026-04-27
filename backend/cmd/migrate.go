package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/aruma256/nazobu/backend/internal/config"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "schema.sql を sqldef で DB に適用する（宣言型マイグレーション）",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Load()

		// mysqldef は PATH に居る前提（Dockerfile で go install 済み）
		mysqldef := exec.Command(
			"mysqldef",
			"--host="+cfg.DB.Host,
			"--port="+cfg.DB.Port,
			"--user="+cfg.DB.User,
			"--password="+cfg.DB.Password,
			"--file="+cfg.SchemaPath,
			"--apply",
			cfg.DB.Name,
		)
		mysqldef.Stdout = os.Stdout
		mysqldef.Stderr = os.Stderr
		if err := mysqldef.Run(); err != nil {
			return fmt.Errorf("mysqldef 実行失敗: %w", err)
		}
		return nil
	},
}
