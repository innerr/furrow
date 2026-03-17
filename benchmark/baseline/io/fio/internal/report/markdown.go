package report

import (
	"fmt"
	"strings"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

func GenerateMarkdown(report *types.Report) ([]byte, error) {
	var sb strings.Builder

	writeMetadata(&sb, &report.Metadata)
	writeHost(&sb, &report.Metadata.Host)
	writeTarget(&sb, &report.Metadata.Target)
	writeTestConfig(&sb, &report.Metadata.Test)
	writePerformance(&sb, report)
	writeLatency(&sb, report)
	writeRecommendations(&sb, &report.Summary)

	return []byte(sb.String()), nil
}

func writeMetadata(sb *strings.Builder, meta *types.ReportMetadata) {
	sb.WriteString("## Metadata\n\n")
	sb.WriteString("| | |\n")
	sb.WriteString("|---|---|\n")
	fmt.Fprintf(sb, "| **Report ID** | %s |\n", meta.ReportID)
	fmt.Fprintf(sb, "| **Generated** | %s |\n", meta.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintf(sb, "| **Tool Version** | %s |\n", meta.ToolVersion)
	sb.WriteString("\n")
}

func writeHost(sb *strings.Builder, host *types.HostInfo) {
	sb.WriteString("## Host\n\n")
	sb.WriteString("| | |\n")
	sb.WriteString("|---|---|\n")
	fmt.Fprintf(sb, "| **Hostname** | %s |\n", host.Hostname)
	if host.FQDN != "" {
		fmt.Fprintf(sb, "| **FQDN** | %s |\n", host.FQDN)
	}
	if len(host.IPAddresses) > 0 {
		fmt.Fprintf(sb, "| **IP** | %s |\n", strings.Join(host.IPAddresses, ", "))
	}
	fmt.Fprintf(sb, "| **Platform** | %s/%s |\n", host.Platform, host.Arch)
	if host.OS != "" {
		fmt.Fprintf(sb, "| **OS** | %s", host.OS)
		if host.OSVersion != "" {
			fmt.Fprintf(sb, " %s", host.OSVersion)
		}
		fmt.Fprintf(sb, " |\n")
	}
	sb.WriteString("\n")
}

func writeTarget(sb *strings.Builder, target *types.Filesystem) {
	sb.WriteString("## Target\n\n")
	sb.WriteString("| | |\n")
	sb.WriteString("|---|---|\n")
	fmt.Fprintf(sb, "| **Mount Point** | %s (%s) |\n", target.MountPoint, target.FilesystemType)
	fmt.Fprintf(sb, "| **Device** | %s |\n", target.DevicePath)
	if target.DeviceModel != "" {
		fmt.Fprintf(sb, "| **Model** | %s |\n", target.DeviceModel)
	}
	fmt.Fprintf(sb, "| **Type** | %s |\n", target.DiskClass)
	fmt.Fprintf(sb, "| **Size** | %s (%s free) |\n", formatBytes(target.TotalBytes), formatBytes(target.FreeBytes))
	sb.WriteString("\n")
}

func writeTestConfig(sb *strings.Builder, test *types.TestInfo) {
	sb.WriteString("## Test Configuration\n\n")
	sb.WriteString("| | |\n")
	sb.WriteString("|---|---|\n")
	fmt.Fprintf(sb, "| **Mode** | %s |\n", test.Mode)
	fmt.Fprintf(sb, "| **Test File Size** | %s |\n", formatBytes(test.TestFileSizeBytes))
	fmt.Fprintf(sb, "| **IO Depth** | %d |\n", test.IODepth)
	fmt.Fprintf(sb, "| **Num Jobs** | %d |\n", test.NumJobs)
	sb.WriteString("\n")
}

func writePerformance(sb *strings.Builder, report *types.Report) {
	sb.WriteString("## Performance\n\n")
	sb.WriteString("| Metric | Value | Rating |\n")
	sb.WriteString("|--------|-------|--------|\n")

	if r, ok := report.Phase3Results["seq_read_async_direct"]; ok {
		score := report.Summary.Scores["seq_read"]
		fmt.Fprintf(sb, "| Sequential Read | %s | %s |\n", FormatBandwidth(r.Metrics.BandwidthMBps), ScoreToStars(score))
	}
	if r, ok := report.Phase3Results["seq_write_async_direct"]; ok {
		score := report.Summary.Scores["seq_write"]
		fmt.Fprintf(sb, "| Sequential Write | %s | %s |\n", FormatBandwidth(r.Metrics.BandwidthMBps), ScoreToStars(score))
	}
	if r, ok := report.Phase3Results["rand_read_4k_async_direct"]; ok {
		score := report.Summary.Scores["rand_read"]
		fmt.Fprintf(sb, "| Random Read 4K | %s IOPS | %s |\n", FormatIOPS(r.Metrics.IOPS), ScoreToStars(score))
	}
	if r, ok := report.Phase3Results["rand_write_4k_async_direct"]; ok {
		score := report.Summary.Scores["rand_write"]
		fmt.Fprintf(sb, "| Random Write 4K | %s IOPS | %s |\n", FormatIOPS(r.Metrics.IOPS), ScoreToStars(score))
	}
	if r, ok := report.Phase3Results["mixed_70_30"]; ok {
		score := report.Summary.Scores["mixed"]
		fmt.Fprintf(sb, "| Mixed 70/30 | %s IOPS | %s |\n", FormatIOPS(r.Metrics.IOPS), ScoreToStars(score))
	}
	if r, ok := report.Phase3Results["fsync_limit"]; ok {
		score := report.Summary.Scores["fsync"]
		fmt.Fprintf(sb, "| fsync | %s IOPS | %s |\n", FormatIOPS(r.Metrics.IOPS), ScoreToStars(score))
	}

	sb.WriteString("\n")
	fmt.Fprintf(sb, "**Overall Score: %d/100**\n\n", report.Summary.OverallScore)
}

func writeLatency(sb *strings.Builder, report *types.Report) {
	sb.WriteString("## Latency (P99)\n\n")
	sb.WriteString("| Operation | Latency |\n")
	sb.WriteString("|-----------|---------|\n")

	if r, ok := report.Phase3Results["rand_read_4k_async_direct"]; ok {
		if p99, ok := r.Metrics.LatencyPercentiles["p99"]; ok {
			fmt.Fprintf(sb, "| Random Read 4K | %s |\n", FormatLatency(p99))
		}
	}
	if r, ok := report.Phase3Results["rand_write_4k_async_direct"]; ok {
		if p99, ok := r.Metrics.LatencyPercentiles["p99"]; ok {
			fmt.Fprintf(sb, "| Random Write 4K | %s |\n", FormatLatency(p99))
		}
	}
	if r, ok := report.Phase3Results["fsync_limit"]; ok {
		if p99, ok := r.Metrics.LatencyPercentiles["p99"]; ok {
			fmt.Fprintf(sb, "| fsync | %s |\n", FormatLatency(p99))
		}
	}

	sb.WriteString("\n")
}

func writeRecommendations(sb *strings.Builder, summary *types.ReportSummary) {
	if len(summary.Recommendations) == 0 {
		return
	}

	sb.WriteString("## Recommendations\n\n")
	for _, rec := range summary.Recommendations {
		fmt.Fprintf(sb, "- %s\n", rec)
	}
	sb.WriteString("\n")
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
