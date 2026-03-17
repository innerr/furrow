package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "fio-bench",
	Short: "Disk I/O performance benchmark tool",
	Long: `A progressive, adaptive disk I/O benchmark tool that uses fio
to measure storage performance and generate detailed reports.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(runCmd)
}
