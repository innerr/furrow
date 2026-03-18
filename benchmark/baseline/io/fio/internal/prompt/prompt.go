package prompt

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

func SelectFilesystem(filesystems []types.Filesystem) (*types.Filesystem, error) {
	fmt.Println()
	fmt.Println("[Step 1/4] Select Target Filesystem")
	fmt.Println()
	fmt.Printf("  %-3s %-20s %-8s %-10s %-10s %-10s\n", "#", "Path", "Type", "Size", "Free", "Disk Type")
	fmt.Println("  ─── ──────────────────── ──────── ────────── ────────── ──────────")

	for i, f := range filesystems {
		fmt.Printf("  %-3d %-20s %-8s %-10s %-10s %-10s\n",
			i+1,
			truncate(f.Path, 20),
			f.FilesystemType,
			formatBytes(f.TotalBytes),
			formatBytes(f.FreeBytes),
			f.DiskType)
	}
	fmt.Println()

	for {
		fmt.Printf("  Select target [1-%d]: ", len(filesystems))
		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("input closed")
			}
			return nil, fmt.Errorf("failed to read input: %w", err)
		}
		input = strings.TrimSpace(input)

		idx, err := strconv.Atoi(input)
		if err != nil || idx < 1 || idx > len(filesystems) {
			fmt.Println("  Invalid selection, please try again")
			continue
		}

		return &filesystems[idx-1], nil
	}
}

func ConfirmStrategy(strategy *types.TestStrategy) (string, error) {
	fmt.Println()
	fmt.Println("[Step 3/4] Test Plan")
	fmt.Println()
	fmt.Printf("  Tests to run (%d items, ~%d min):\n", len(strategy.TestsPlanned), len(strategy.TestsPlanned))

	for _, test := range strategy.TestsPlanned {
		fmt.Printf("    ✓ %s\n", test)
	}

	if len(strategy.TestsSkipped) > 0 {
		fmt.Println()
		fmt.Printf("  Tests skipped (%d items):\n", len(strategy.TestsSkipped))
		for _, test := range strategy.TestsSkipped {
			reason := strategy.SkipReasons[test]
			if reason == "" {
				reason = "not needed for this disk class"
			}
			fmt.Printf("    ✗ %s (%s)\n", test, reason)
		}
	}

	fmt.Println()
	fmt.Printf("  [P]roceed  [Q]uit: ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		if err == io.EOF {
			return "", fmt.Errorf("input closed")
		}
		return "", fmt.Errorf("failed to read input: %w", err)
	}
	input = strings.ToLower(strings.TrimSpace(input))

	switch input {
	case "p", "proceed", "y", "yes":
		return "proceed", nil
	case "q", "quit", "n", "no":
		return "quit", nil
	default:
		return "proceed", nil
	}
}

func DisplaySamplingResults(sample *types.SampleResult) {
	fmt.Println()
	fmt.Println("[Step 2/4] Quick Sampling")
	fmt.Println()
	fmt.Println("  Results:")
	fmt.Printf("    Sequential Read:  %s\n", formatBandwidth(sample.SeqReadBWMBps))
	fmt.Printf("    Sequential Write: %s\n", formatBandwidth(sample.SeqWriteBWMBps))
	fmt.Printf("    Random Read 4K:   %s IOPS\n", formatIOPS(sample.RandReadIOPS))
	fmt.Printf("    Random Write 4K:  %s IOPS\n", formatIOPS(sample.RandWriteIOPS))
	fmt.Printf("    fsync:            %s IOPS\n", formatIOPS(sample.FsyncIOPS))
	fmt.Println()
	fmt.Printf("  Detected: %s\n", sample.DiskClass)
}

func DisplayTestProgress(current, total int, name string, metrics *types.TestMetrics) {
	fmt.Printf("  [%d/%d] %s...", current, total, name)
	if metrics != nil {
		if metrics.BandwidthMBps > 0 {
			fmt.Printf(" %s", formatBandwidth(uint64(metrics.BandwidthMBps)))
		} else if metrics.IOPS > 0 {
			fmt.Printf(" %s IOPS", formatIOPS(uint64(metrics.IOPS)))
		}
	}
	fmt.Println()
}

func DisplayCompletion(reportPath string, successCount, totalCount int) {
	fmt.Println()
	if successCount == totalCount {
		fmt.Println("  ✓ All tests completed successfully")
	} else {
		fmt.Printf("  %d/%d tests completed successfully\n", successCount, totalCount)
	}
	fmt.Println()
	fmt.Println("  Reports generated:")
	fmt.Printf("    %s.md\n", reportPath)
	fmt.Printf("    %s.json\n", reportPath)
	fmt.Println()
}

func DisplayError(msg string) {
	fmt.Printf("\n  ✗ Error: %s\n\n", msg)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return "..." + s[len(s)-maxLen+3:]
}

func formatBandwidth(mbps uint64) string {
	if mbps >= 1000 {
		return fmt.Sprintf("%.1f GB/s", float64(mbps)/1000)
	}
	return fmt.Sprintf("%d MB/s", mbps)
}

func formatIOPS(iops uint64) string {
	if iops >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(iops)/1000000)
	} else if iops >= 1000 {
		return fmt.Sprintf("%.0fK", float64(iops)/1000)
	}
	return fmt.Sprintf("%d", iops)
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
