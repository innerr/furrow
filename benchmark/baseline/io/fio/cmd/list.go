package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/fs"
)

var listFormat string

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List available filesystems",
	Long:  `List all mountable filesystems that can be benchmarked.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		detector := fs.NewDetector()
		filesystems, err := detector.List()
		if err != nil {
			return fmt.Errorf("failed to list filesystems: %w", err)
		}

		if listFormat == "json" {
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(filesystems)
		}

		fmt.Printf("%-3s %-30s %-8s %-12s %-12s %-10s\n", "#", "Path", "Type", "Size", "Free", "Disk Type")
		fmt.Println("─── ──────────────────────────── ──────── ──────────── ──────────── ──────────")

		for i, f := range filesystems {
			fmt.Printf("%-3d %-30s %-8s %-12s %-12s %-10s\n",
				i+1,
				f.Path,
				f.FilesystemType,
				formatBytes(f.TotalBytes),
				formatBytes(f.FreeBytes),
				f.DiskType)
		}

		return nil
	},
}

func init() {
	listCmd.Flags().StringVarP(&listFormat, "format", "f", "table", "Output format (table, json)")
}

func formatBytes(bytes uint64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)

	if bytes >= TB {
		return fmt.Sprintf("%.1f TB", float64(bytes)/float64(TB))
	}
	if bytes >= GB {
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	}
	if bytes >= MB {
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	}
	if bytes >= KB {
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	}
	return fmt.Sprintf("%d B", bytes)
}
