package cmd

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:           "nazobu",
	Short:         "謎部 backend",
	SilenceErrors: true,
	SilenceUsage:  true,
}

func Execute() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if cmd, err := rootCmd.ExecuteC(); err != nil {
		operation := "nazobu"
		if cmd != nil {
			operation = cmd.Name()
		}
		slog.Error("command failed", "operation", operation)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(addUserCmd)
	rootCmd.AddCommand(setRoleCmd)
}
