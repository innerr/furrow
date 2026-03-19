package report

import (
	"testing"
	"time"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

func TestCalculateScore(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		refs     types.ReferenceValues
		minScore int
		maxScore int
	}{
		{
			name:     "excellent performance",
			value:    4000,
			refs:     types.ReferenceValues{Excellent: 3500, Good: 2500, Fair: 1500, Poor: 500},
			minScore: 100,
			maxScore: 100,
		},
		{
			name:     "good performance",
			value:    3000,
			refs:     types.ReferenceValues{Excellent: 3500, Good: 2500, Fair: 1500, Poor: 500},
			minScore: 80,
			maxScore: 99,
		},
		{
			name:     "fair performance",
			value:    2000,
			refs:     types.ReferenceValues{Excellent: 3500, Good: 2500, Fair: 1500, Poor: 500},
			minScore: 60,
			maxScore: 79,
		},
		{
			name:     "poor performance",
			value:    800,
			refs:     types.ReferenceValues{Excellent: 3500, Good: 2500, Fair: 1500, Poor: 500},
			minScore: 40,
			maxScore: 59,
		},
		{
			name:     "very poor performance",
			value:    200,
			refs:     types.ReferenceValues{Excellent: 3500, Good: 2500, Fair: 1500, Poor: 500},
			minScore: 0,
			maxScore: 39,
		},
		{
			name:     "zero value",
			value:    0,
			refs:     types.ReferenceValues{Excellent: 3500, Good: 2500, Fair: 1500, Poor: 500},
			minScore: 0,
			maxScore: 0,
		},
		{
			name:     "exactly at good threshold",
			value:    2500,
			refs:     types.ReferenceValues{Excellent: 3500, Good: 2500, Fair: 1500, Poor: 500},
			minScore: 80,
			maxScore: 80,
		},
		{
			name:     "exactly at fair threshold",
			value:    1500,
			refs:     types.ReferenceValues{Excellent: 3500, Good: 2500, Fair: 1500, Poor: 500},
			minScore: 60,
			maxScore: 60,
		},
		{
			name:     "exactly at poor threshold",
			value:    500,
			refs:     types.ReferenceValues{Excellent: 3500, Good: 2500, Fair: 1500, Poor: 500},
			minScore: 40,
			maxScore: 40,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := calculateScore(tt.value, tt.refs)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("calculateScore() = %v, want between %v and %v", score, tt.minScore, tt.maxScore)
			}
		})
	}
}

func TestCalculateScores(t *testing.T) {
	results := map[string]types.TestConfigResult{
		"seq_read_async_direct": {
			Metrics: types.TestMetrics{BandwidthMBps: 3000},
		},
		"seq_write_async_direct": {
			Metrics: types.TestMetrics{BandwidthMBps: 2500},
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
	}

	scores := CalculateScores(results, types.DiskClassNVMeSSD)

	expectedScores := map[string]bool{
		"seq_read":   true,
		"seq_write":  true,
		"rand_read":  true,
		"rand_write": true,
		"mixed":      true,
		"fsync":      true,
	}

	for key := range expectedScores {
		if _, ok := scores[key]; !ok {
			t.Errorf("CalculateScores() missing score for %v", key)
		}
	}
}

func TestCalculateOverallScore(t *testing.T) {
	tests := []struct {
		name     string
		scores   map[string]int
		minScore int
		maxScore int
	}{
		{
			name: "all excellent",
			scores: map[string]int{
				"seq_read":   100,
				"seq_write":  100,
				"rand_read":  100,
				"rand_write": 100,
				"mixed":      100,
				"fsync":      100,
			},
			minScore: 95,
			maxScore: 100,
		},
		{
			name: "all average",
			scores: map[string]int{
				"seq_read":   70,
				"seq_write":  70,
				"rand_read":  70,
				"rand_write": 70,
				"mixed":      70,
				"fsync":      70,
			},
			minScore: 65,
			maxScore: 75,
		},
		{
			name: "mixed scores",
			scores: map[string]int{
				"seq_read":   100,
				"seq_write":  80,
				"rand_read":  60,
				"rand_write": 50,
				"mixed":      70,
				"fsync":      40,
			},
			minScore: 55,
			maxScore: 75,
		},
		{
			name:     "empty scores",
			scores:   map[string]int{},
			minScore: 0,
			maxScore: 0,
		},
		{
			name: "partial scores",
			scores: map[string]int{
				"seq_read":  80,
				"rand_read": 80,
			},
			minScore: 70,
			maxScore: 90,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := CalculateOverallScore(tt.scores)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("CalculateOverallScore() = %v, want between %v and %v", score, tt.minScore, tt.maxScore)
			}
		})
	}
}

func TestCalculateOverallScoreWeights(t *testing.T) {
	scores := map[string]int{
		"seq_read":   100,
		"seq_write":  0,
		"rand_read":  0,
		"rand_write": 0,
		"mixed":      0,
		"fsync":      0,
	}

	score := CalculateOverallScore(scores)
	if score <= 0 {
		t.Errorf("CalculateOverallScore() should be positive when seq_read is 100, got %v", score)
	}

	scores2 := map[string]int{
		"seq_read":   0,
		"seq_write":  0,
		"rand_read":  100,
		"rand_write": 0,
		"mixed":      0,
		"fsync":      0,
	}

	score2 := CalculateOverallScore(scores2)
	if score2 <= 0 {
		t.Errorf("CalculateOverallScore() should be positive when rand_read is 100, got %v", score2)
	}
}

func TestGetReferenceValues(t *testing.T) {
	tests := []struct {
		name      string
		diskClass types.DiskClass
		checkFunc func(types.ScoreReferences) bool
	}{
		{
			name:      "NVMe SSD references",
			diskClass: types.DiskClassNVMeSSD,
			checkFunc: func(refs types.ScoreReferences) bool {
				return refs.SeqReadBW.Excellent > 3000 && refs.RandReadIOPS.Excellent > 500000
			},
		},
		{
			name:      "SATA SSD references",
			diskClass: types.DiskClassSATASSD,
			checkFunc: func(refs types.ScoreReferences) bool {
				return refs.SeqReadBW.Excellent > 400 && refs.RandReadIOPS.Excellent > 50000
			},
		},
		{
			name:      "HDD references",
			diskClass: types.DiskClassFastHDD,
			checkFunc: func(refs types.ScoreReferences) bool {
				return refs.SeqReadBW.Excellent <= 300
			},
		},
		{
			name:      "Unknown class uses HDD references",
			diskClass: "Unknown",
			checkFunc: func(refs types.ScoreReferences) bool {
				return refs.SeqReadBW.Excellent <= 300
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs := getReferenceValues(tt.diskClass)
			if !tt.checkFunc(refs) {
				t.Errorf("getReferenceValues() check failed for %v", tt.name)
			}
		})
	}
}

func TestGenerateRecommendations(t *testing.T) {
	tests := []struct {
		name          string
		scores        map[string]int
		diskClass     types.DiskClass
		expectAtLeast int
	}{
		{
			name: "high performance",
			scores: map[string]int{
				"seq_read":   90,
				"seq_write":  90,
				"rand_read":  90,
				"rand_write": 90,
				"mixed":      90,
				"fsync":      90,
			},
			diskClass:     types.DiskClassNVMeSSD,
			expectAtLeast: 1,
		},
		{
			name: "low fsync score",
			scores: map[string]int{
				"seq_read":   80,
				"seq_write":  80,
				"rand_read":  80,
				"rand_write": 80,
				"mixed":      80,
				"fsync":      50,
			},
			diskClass:     types.DiskClassNVMeSSD,
			expectAtLeast: 1,
		},
		{
			name: "average performance",
			scores: map[string]int{
				"seq_read":   70,
				"seq_write":  70,
				"rand_read":  70,
				"rand_write": 70,
				"mixed":      70,
				"fsync":      70,
			},
			diskClass:     types.DiskClassNVMeSSD,
			expectAtLeast: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recs := GenerateRecommendations(tt.scores, tt.diskClass)
			if len(recs) < tt.expectAtLeast {
				t.Errorf("GenerateRecommendations() returned %v recommendations, want at least %v",
					len(recs), tt.expectAtLeast)
			}
		})
	}
}

func TestIdentifyBottleneck(t *testing.T) {
	tests := []struct {
		name             string
		results          map[string]types.TestConfigResult
		scores           map[string]int
		expectBottleneck bool
		expectedName     string
	}{
		{
			name: "fsync is bottleneck",
			results: map[string]types.TestConfigResult{
				"fsync_limit": {Metrics: types.TestMetrics{IOPS: 5000}},
			},
			scores: map[string]int{
				"seq_read":   90,
				"seq_write":  85,
				"rand_read":  88,
				"rand_write": 82,
				"mixed":      80,
				"fsync":      40,
			},
			expectBottleneck: true,
			expectedName:     "fsync",
		},
		{
			name:    "no bottleneck - all good",
			results: map[string]types.TestConfigResult{},
			scores: map[string]int{
				"seq_read":   95,
				"seq_write":  90,
				"rand_read":  92,
				"rand_write": 88,
				"mixed":      85,
				"fsync":      90,
			},
			expectBottleneck: false,
		},
		{
			name:             "no scores",
			results:          map[string]types.TestConfigResult{},
			scores:           map[string]int{},
			expectBottleneck: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bottleneck, detail := IdentifyBottleneck(tt.results, tt.scores)

			if tt.expectBottleneck {
				if bottleneck == "" {
					t.Error("IdentifyBottleneck() expected bottleneck, got none")
				}
				if bottleneck != tt.expectedName {
					t.Errorf("IdentifyBottleneck() = %v, want %v", bottleneck, tt.expectedName)
				}
				if detail == "" {
					t.Error("IdentifyBottleneck() expected detail, got empty")
				}
			} else {
				if bottleneck != "" {
					t.Errorf("IdentifyBottleneck() expected no bottleneck, got %v", bottleneck)
				}
			}
		})
	}
}

func TestScoreToStars(t *testing.T) {
	tests := []struct {
		score    int
		expected string
	}{
		{100, "★★★★★"},
		{95, "★★★★★"},
		{80, "★★★★☆"},
		{79, "★★★☆☆"},
		{60, "★★★☆☆"},
		{59, "★★☆☆☆"},
		{40, "★★☆☆☆"},
		{39, "★☆☆☆☆"},
		{0, "★☆☆☆☆"},
	}

	for _, tt := range tests {
		result := ScoreToStars(tt.score)
		if result != tt.expected {
			t.Errorf("ScoreToStars(%v) = %v, want %v", tt.score, result, tt.expected)
		}
	}
}

func TestFormatBandwidth(t *testing.T) {
	tests := []struct {
		mbps     float64
		expected string
	}{
		{3500, "3.5 GB/s"},
		{2048, "2.0 GB/s"},
		{1000, "1.0 GB/s"},
		{999, "999 MB/s"},
		{500, "500 MB/s"},
		{100, "100 MB/s"},
		{0, "0 MB/s"},
	}

	for _, tt := range tests {
		result := FormatBandwidth(tt.mbps)
		if result != tt.expected {
			t.Errorf("FormatBandwidth(%v) = %v, want %v", tt.mbps, result, tt.expected)
		}
	}
}

func TestFormatIOPS(t *testing.T) {
	tests := []struct {
		iops     float64
		expected string
	}{
		{1500000, "1.5M"},
		{1000000, "1.0M"},
		{500000, "500K"},
		{1000, "1K"},
		{999, "999"},
		{100, "100"},
		{0, "0"},
	}

	for _, tt := range tests {
		result := FormatIOPS(tt.iops)
		if result != tt.expected {
			t.Errorf("FormatIOPS(%v) = %v, want %v", tt.iops, result, tt.expected)
		}
	}
}

func TestFormatLatency(t *testing.T) {
	tests := []struct {
		us       float64
		expected string
	}{
		{5000, "5.0 ms"},
		{1000, "1.0 ms"},
		{999, "999.0 μs"},
		{100, "100.0 μs"},
		{10, "10.0 μs"},
		{1, "1.0 μs"},
	}

	for _, tt := range tests {
		result := FormatLatency(tt.us)
		if result != tt.expected {
			t.Errorf("FormatLatency(%v) = %v, want %v", tt.us, result, tt.expected)
		}
	}
}

func TestGenerateReportID(t *testing.T) {
	hostname := "testhost"
	deviceName := "nvme0n1p1"
	timestamp := time.Date(2024, 3, 18, 10, 30, 45, 0, time.UTC)

	reportID := GenerateReportID(hostname, deviceName, timestamp)

	expected := "20240318_103045_testhost_nvme0n1p1"
	if reportID != expected {
		t.Errorf("GenerateReportID() = %v, want %v", reportID, expected)
	}
}

func TestGenerateJSON(t *testing.T) {
	report := &types.Report{
		Metadata: types.ReportMetadata{
			ReportID:    "test-report-001",
			GeneratedAt: time.Date(2024, 3, 18, 10, 0, 0, 0, time.UTC),
			ToolVersion: "1.0.0",
		},
		Summary: types.ReportSummary{
			OverallScore: 85,
		},
	}

	jsonData, err := GenerateJSON(report)
	if err != nil {
		t.Errorf("GenerateJSON() error = %v", err)
		return
	}

	if len(jsonData) == 0 {
		t.Error("GenerateJSON() returned empty data")
	}

	jsonStr := string(jsonData)
	if jsonStr == "" || jsonStr == "null" {
		t.Error("GenerateJSON() returned invalid JSON")
	}
}
