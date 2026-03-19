package report

import (
	"strings"
	"testing"
	"time"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

func TestGenerateMarkdown(t *testing.T) {
	report := &types.Report{
		Metadata: types.ReportMetadata{
			ReportID:    "20240318_100000_testhost_nvme0n1p1",
			GeneratedAt: time.Date(2024, 3, 18, 10, 0, 0, 0, time.UTC),
			ToolVersion: "1.0.0",
			Host: types.HostInfo{
				Hostname:    "testhost",
				Platform:    "linux",
				Arch:        "amd64",
				OS:          "Ubuntu",
				OSVersion:   "22.04",
				IPAddresses: []string{"192.168.1.100"},
			},
			Target: types.Filesystem{
				MountPoint:     "/mnt/data",
				FilesystemType: "ext4",
				DevicePath:     "/dev/nvme0n1p1",
				DeviceModel:    "Samsung SSD 980 PRO",
				DiskClass:      types.DiskClassNVMeSSD,
				TotalBytes:     500 * 1024 * 1024 * 1024,
				FreeBytes:      200 * 1024 * 1024 * 1024,
			},
			Test: types.TestInfo{
				Mode:              "adaptive",
				TestFileSizeBytes: 4 * 1024 * 1024 * 1024,
				IODepth:           32,
				NumJobs:           4,
			},
		},
		Phase3Results: map[string]types.TestConfigResult{
			"seq_read_async_direct": {
				Metrics: types.TestMetrics{BandwidthMBps: 3500},
			},
			"seq_write_async_direct": {
				Metrics: types.TestMetrics{BandwidthMBps: 3000},
			},
			"rand_read_4k_async_direct": {
				Metrics: types.TestMetrics{IOPS: 600000},
			},
			"rand_write_4k_async_direct": {
				Metrics: types.TestMetrics{IOPS: 500000},
			},
			"mixed_70_30": {
				Metrics: types.TestMetrics{IOPS: 400000},
			},
			"fsync_limit": {
				Metrics: types.TestMetrics{IOPS: 40000},
			},
		},
		Summary: types.ReportSummary{
			Scores: map[string]int{
				"seq_read":   100,
				"seq_write":  95,
				"rand_read":  90,
				"rand_write": 85,
				"mixed":      80,
				"fsync":      75,
			},
			OverallScore:    88,
			Recommendations: []string{"Well-suited for high-performance workloads"},
		},
	}

	md, err := GenerateMarkdown(report)
	if err != nil {
		t.Errorf("GenerateMarkdown() error = %v", err)
		return
	}

	mdStr := string(md)

	requiredSections := []string{
		"## Metadata",
		"## Host",
		"## Target",
		"## Test Configuration",
		"## Performance",
		"## Recommendations",
		"testhost",
		"/mnt/data",
		"Overall Score",
	}

	for _, section := range requiredSections {
		if !strings.Contains(mdStr, section) {
			t.Errorf("GenerateMarkdown() missing section/content: %v", section)
		}
	}
}

func TestGenerateMarkdownMinimal(t *testing.T) {
	report := &types.Report{
		Metadata: types.ReportMetadata{
			ReportID:    "test-report",
			GeneratedAt: time.Now(),
			ToolVersion: "1.0.0",
			Host:        types.HostInfo{Hostname: "minimal"},
			Target: types.Filesystem{
				MountPoint: "/",
				TotalBytes: 100 * 1024 * 1024 * 1024,
				FreeBytes:  50 * 1024 * 1024 * 1024,
			},
			Test: types.TestInfo{
				Mode: "adaptive",
			},
		},
		Phase3Results: map[string]types.TestConfigResult{},
		Summary: types.ReportSummary{
			Scores:          map[string]int{},
			Recommendations: []string{},
		},
	}

	md, err := GenerateMarkdown(report)
	if err != nil {
		t.Errorf("GenerateMarkdown() error = %v", err)
		return
	}

	if len(md) == 0 {
		t.Error("GenerateMarkdown() returned empty markdown")
	}
}

func TestFormatBytesInMarkdown(t *testing.T) {
	tests := []struct {
		bytes    uint64
		expected string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{1024 * 1024 * 1024 * 1024, "1.0 TB"},
		{512 * 1024 * 1024, "512.0 MB"},
		{2 * 1024 * 1024 * 1024, "2.0 GB"},
	}

	for _, tt := range tests {
		result := formatBytes(tt.bytes)
		if result != tt.expected {
			t.Errorf("formatBytes(%v) = %v, want %v", tt.bytes, result, tt.expected)
		}
	}
}

func TestWriteMetadata(t *testing.T) {
	var sb strings.Builder
	meta := &types.ReportMetadata{
		ReportID:    "test-123",
		GeneratedAt: time.Date(2024, 3, 18, 10, 30, 45, 0, time.UTC),
		ToolVersion: "1.0.0",
	}

	writeMetadata(&sb, meta)

	result := sb.String()
	if !strings.Contains(result, "test-123") {
		t.Error("writeMetadata() missing Report ID")
	}
	if !strings.Contains(result, "1.0.0") {
		t.Error("writeMetadata() missing Tool Version")
	}
}

func TestWriteHost(t *testing.T) {
	tests := []struct {
		name     string
		host     *types.HostInfo
		contains []string
	}{
		{
			name: "full host info",
			host: &types.HostInfo{
				Hostname:    "server01",
				FQDN:        "server01.example.com",
				IPAddresses: []string{"192.168.1.1", "10.0.0.1"},
				Platform:    "linux",
				Arch:        "amd64",
				OS:          "Ubuntu",
				OSVersion:   "22.04",
			},
			contains: []string{"server01", "server01.example.com", "192.168.1.1", "Ubuntu 22.04"},
		},
		{
			name: "minimal host info",
			host: &types.HostInfo{
				Hostname: "minimal",
				Platform: "darwin",
				Arch:     "arm64",
			},
			contains: []string{"minimal", "darwin/arm64"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sb strings.Builder
			writeHost(&sb, tt.host)
			result := sb.String()

			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("writeHost() missing expected content: %v", expected)
				}
			}
		})
	}
}

func TestWriteTarget(t *testing.T) {
	tests := []struct {
		name     string
		target   *types.Filesystem
		contains []string
	}{
		{
			name: "full target info",
			target: &types.Filesystem{
				MountPoint:     "/mnt/data",
				FilesystemType: "ext4",
				DevicePath:     "/dev/nvme0n1p1",
				DeviceModel:    "Samsung SSD 980",
				DiskClass:      types.DiskClassNVMeSSD,
				TotalBytes:     500 * 1024 * 1024 * 1024,
				FreeBytes:      200 * 1024 * 1024 * 1024,
			},
			contains: []string{"/mnt/data", "ext4", "Samsung SSD 980", "NVMe_SSD"},
		},
		{
			name: "minimal target info",
			target: &types.Filesystem{
				MountPoint: "/",
				TotalBytes: 100 * 1024 * 1024 * 1024,
				FreeBytes:  50 * 1024 * 1024 * 1024,
			},
			contains: []string{"/"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sb strings.Builder
			writeTarget(&sb, tt.target)
			result := sb.String()

			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("writeTarget() missing expected content: %v", expected)
				}
			}
		})
	}
}

func TestWriteTestConfig(t *testing.T) {
	var sb strings.Builder
	test := &types.TestInfo{
		Mode:              "adaptive",
		TestFileSizeBytes: 4 * 1024 * 1024 * 1024,
		IODepth:           32,
		NumJobs:           4,
	}

	writeTestConfig(&sb, test)
	result := sb.String()

	expectedContent := []string{"adaptive", "32", "4"}
	for _, expected := range expectedContent {
		if !strings.Contains(result, expected) {
			t.Errorf("writeTestConfig() missing expected content: %v", expected)
		}
	}
}

func TestWritePerformance(t *testing.T) {
	report := &types.Report{
		Phase3Results: map[string]types.TestConfigResult{
			"seq_read_async_direct": {
				Metrics: types.TestMetrics{BandwidthMBps: 3500},
			},
			"rand_read_4k_async_direct": {
				Metrics: types.TestMetrics{IOPS: 600000},
			},
		},
		Summary: types.ReportSummary{
			Scores: map[string]int{
				"seq_read":  100,
				"rand_read": 90,
			},
			OverallScore: 95,
		},
	}

	var sb strings.Builder
	writePerformance(&sb, report)
	result := sb.String()

	if !strings.Contains(result, "Sequential Read") {
		t.Error("writePerformance() missing Sequential Read")
	}
	if !strings.Contains(result, "Random Read 4K") {
		t.Error("writePerformance() missing Random Read 4K")
	}
	if !strings.Contains(result, "Overall Score") {
		t.Error("writePerformance() missing Overall Score")
	}
}

func TestWriteLatency(t *testing.T) {
	tests := []struct {
		name     string
		report   *types.Report
		contains []string
	}{
		{
			name: "with latency data",
			report: &types.Report{
				Phase3Results: map[string]types.TestConfigResult{
					"latency_read": {
						Metrics: types.TestMetrics{
							LatencyPercentiles: map[string]float64{"p99": 50.0},
						},
					},
					"fsync_limit": {
						Metrics: types.TestMetrics{
							LatencyPercentiles: map[string]float64{"p99": 100.0},
						},
					},
				},
			},
			contains: []string{"Random Read 4K", "fsync"},
		},
		{
			name: "no latency data",
			report: &types.Report{
				Phase3Results: map[string]types.TestConfigResult{},
			},
			contains: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sb strings.Builder
			writeLatency(&sb, tt.report)
			result := sb.String()

			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("writeLatency() missing expected content: %v", expected)
				}
			}
		})
	}
}

func TestWriteRecommendations(t *testing.T) {
	tests := []struct {
		name          string
		summary       *types.ReportSummary
		expectContent bool
	}{
		{
			name: "with recommendations",
			summary: &types.ReportSummary{
				Recommendations: []string{
					"Well-suited for high-performance workloads",
					"Consider enabling write-back cache",
				},
			},
			expectContent: true,
		},
		{
			name: "no recommendations",
			summary: &types.ReportSummary{
				Recommendations: []string{},
			},
			expectContent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sb strings.Builder
			writeRecommendations(&sb, tt.summary)
			result := sb.String()

			if tt.expectContent && result == "" {
				t.Error("writeRecommendations() expected content but got empty")
			}
			if !tt.expectContent && result != "" {
				t.Errorf("writeRecommendations() expected empty but got: %v", result)
			}
		})
	}
}
