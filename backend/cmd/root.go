package cmd

import "github.com/spf13/cobra"

var rootCmd = &cobra.Command{
	Use:   "nazobu",
	Short: "謎部 backend",
}

func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}

func init() {
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(startCmd)
}
