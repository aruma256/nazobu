package cmd

import (
	"github.com/aruma256/nazobu/backend/internal/config"
	"github.com/aruma256/nazobu/backend/internal/db"
	"github.com/aruma256/nazobu/backend/internal/server"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "HTTP サーバを起動する",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Load()

		conn, err := db.Open(cfg.DB)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()

		return server.Run(cmd.Context(), cfg, conn)
	},
}
