package analyzer

import (
	"testing"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

func TestGenerateStrategy(t *testing.T) {
	tests := []struct {
		name               string
		sample             *types.SampleResult
		diskClass          types.DiskClass
		checkFunc          func(*types.TestStrategy) bool
		expectedMinTests   int
		expectedMinSkipped int
	}{
		{
			name: "NVMe SSD strategy",
			sample: &types.SampleResult{
				SeqReadBWMBps:  3500,
				SeqWriteBWMBps: 3000,
				RandReadIOPS:   600000,
				RandWriteIOPS:  500000,
			},
			diskClass: types.DiskClassNVMeSSD,
			checkFunc: func(s *types.TestStrategy) bool {
				return s.IODepth == 32 && s.NumJobs == 4
			},
			expectedMinTests: 8,
		},
		{
			name: "SATA SSD strategy",
			sample: &types.SampleResult{
				SeqReadBWMBps:  500,
				SeqWriteBWMBps: 450,
				RandReadIOPS:   80000,
				RandWriteIOPS:  70000,
			},
			diskClass: types.DiskClassSATASSD,
			checkFunc: func(s *types.TestStrategy) bool {
				return s.IODepth == 32 && s.NumJobs == 4
			},
			expectedMinTests: 10,
		},
		{
			name: "Fast HDD strategy",
			sample: &types.SampleResult{
				SeqReadBWMBps:  180,
				SeqWriteBWMBps: 170,
				RandReadIOPS:   150,
				RandWriteIOPS:  140,
			},
			diskClass: types.DiskClassFastHDD,
			checkFunc: func(s *types.TestStrategy) bool {
				return s.IODepth == 8 && s.NumJobs == 2
			},
			expectedMinTests: 5,
		},
		{
			name: "Slow HDD strategy",
			sample: &types.SampleResult{
				SeqReadBWMBps:  80,
				SeqWriteBWMBps: 75,
				RandReadIOPS:   60,
				RandWriteIOPS:  55,
			},
			diskClass: types.DiskClassSlowHDD,
			checkFunc: func(s *types.TestStrategy) bool {
				return s.IODepth == 8 && s.NumJobs == 2
			},
			expectedMinTests: 5,
		},
		{
			name: "Unknown disk class defaults to SlowHDD",
			sample: &types.SampleResult{
				SeqReadBWMBps:  50,
				SeqWriteBWMBps: 45,
				RandReadIOPS:   30,
				RandWriteIOPS:  25,
			},
			diskClass: "UnknownClass",
			checkFunc: func(s *types.TestStrategy) bool {
				return s.IODepth == 8 && s.NumJobs == 2
			},
			expectedMinTests: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.sample.DiskClass = tt.diskClass
			strategy := GenerateStrategy(tt.sample, tt.diskClass)

			if len(strategy.TestsPlanned) < tt.expectedMinTests {
				t.Errorf("GenerateStrategy() tests planned = %v, want at least %v",
					len(strategy.TestsPlanned), tt.expectedMinTests)
			}

			if !tt.checkFunc(strategy) {
				t.Errorf("GenerateStrategy() check function failed for %v", tt.name)
			}

			if strategy.RuntimePerTest != 60 {
				t.Errorf("GenerateStrategy() RuntimePerTest = %v, want 60", strategy.RuntimePerTest)
			}

			if strategy.SkipReasons == nil {
				t.Error("GenerateStrategy() SkipReasons should not be nil")
			}
		})
	}
}

func TestGenerateStrategySkipRedundant(t *testing.T) {
	t.Run("skip seq_write when bandwidth similar", func(t *testing.T) {
		sample := &types.SampleResult{
			SeqReadBWMBps:  500,
			SeqWriteBWMBps: 495,
			RandReadIOPS:   80000,
			RandWriteIOPS:  70000,
			DiskClass:      types.DiskClassSATASSD,
		}

		strategy := GenerateStrategy(sample, types.DiskClassSATASSD)

		skipped := false
		for _, test := range strategy.TestsSkipped {
			if test == "seq_write_async_direct" {
				skipped = true
				break
			}
		}

		if !skipped {
			t.Fatal("seq_write_async_direct should be skipped when bandwidth is within 10%")
		}

		if reason := strategy.SkipReasons["seq_write_async_direct"]; reason != "read/write bandwidth within 10%" {
			t.Fatalf("SkipReasons[seq_write_async_direct] = %q, want %q", reason, "read/write bandwidth within 10%")
		}
	})

	t.Run("skip rand_write when IOPS similar", func(t *testing.T) {
		sample := &types.SampleResult{
			SeqReadBWMBps:  3500,
			SeqWriteBWMBps: 3000,
			RandReadIOPS:   500000,
			RandWriteIOPS:  490000,
			DiskClass:      types.DiskClassNVMeSSD,
		}

		strategy := GenerateStrategy(sample, types.DiskClassNVMeSSD)

		skipped := false
		for _, test := range strategy.TestsSkipped {
			if test == "rand_write_4k_async_direct" {
				skipped = true
				break
			}
		}

		if !skipped {
			t.Fatal("rand_write_4k_async_direct should be skipped when IOPS is within 15%")
		}

		latencyWriteSkipped := false
		for _, test := range strategy.TestsSkipped {
			if test == "latency_write" {
				latencyWriteSkipped = true
				break
			}
		}

		if !latencyWriteSkipped {
			t.Fatal("latency_write should be skipped together with rand_write_4k_async_direct")
		}

		if reason := strategy.SkipReasons["rand_write_4k_async_direct"]; reason != "read/write IOPS within 15%" {
			t.Fatalf("SkipReasons[rand_write_4k_async_direct] = %q, want %q", reason, "read/write IOPS within 15%")
		}

		if reason := strategy.SkipReasons["latency_write"]; reason != "read/write IOPS within 15%" {
			t.Fatalf("SkipReasons[latency_write] = %q, want %q", reason, "read/write IOPS within 15%")
		}
	})

	t.Run("do not skip when performance differs significantly", func(t *testing.T) {
		sample := &types.SampleResult{
			SeqReadBWMBps:  3500,
			SeqWriteBWMBps: 1500,
			RandReadIOPS:   600000,
			RandWriteIOPS:  200000,
			DiskClass:      types.DiskClassNVMeSSD,
		}

		strategy := GenerateStrategy(sample, types.DiskClassNVMeSSD)

		seqWritePlanned := false
		for _, test := range strategy.TestsPlanned {
			if test == "seq_write_async_direct" {
				seqWritePlanned = true
				break
			}
		}

		if !seqWritePlanned {
			t.Error("seq_write_async_direct should be planned when bandwidth differs significantly")
		}

		randWritePlanned := false
		for _, test := range strategy.TestsPlanned {
			if test == "rand_write_4k_async_direct" {
				randWritePlanned = true
				break
			}
		}

		if !randWritePlanned {
			t.Error("rand_write_4k_async_direct should be planned when IOPS differs significantly")
		}
	})
}

func TestGenerateStrategyNilSample(t *testing.T) {
	strategy := GenerateStrategy(nil, types.DiskClassNVMeSSD)

	if strategy == nil {
		t.Fatal("GenerateStrategy() returned nil")
	}

	if len(strategy.TestsPlanned) == 0 {
		t.Error("GenerateStrategy() should return some planned tests even with nil sample")
	}
}

func TestGenerateStrategyTestsContent(t *testing.T) {
	sample := &types.SampleResult{
		SeqReadBWMBps:  3500,
		SeqWriteBWMBps: 3000,
		RandReadIOPS:   600000,
		RandWriteIOPS:  500000,
		DiskClass:      types.DiskClassNVMeSSD,
	}

	strategy := GenerateStrategy(sample, types.DiskClassNVMeSSD)

	expectedTests := []string{
		"seq_read_async_direct",
		"rand_read_4k_async_direct",
		"mixed_70_30",
		"fsync_limit",
	}

	for _, expected := range expectedTests {
		found := false
		for _, test := range strategy.TestsPlanned {
			if test == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected test %v not found in TestsPlanned", expected)
		}
	}
}

func TestGenerateStrategyPercentiles(t *testing.T) {
	tests := []struct {
		name                string
		diskClass           types.DiskClass
		expectedMinLen      int
		expectedPercentiles []string
	}{
		{
			name:                "NVMe SSD percentiles",
			diskClass:           types.DiskClassNVMeSSD,
			expectedMinLen:      4,
			expectedPercentiles: []string{"p50", "p95", "p99"},
		},
		{
			name:                "SATA SSD percentiles",
			diskClass:           types.DiskClassSATASSD,
			expectedMinLen:      3,
			expectedPercentiles: []string{"p50", "p99"},
		},
		{
			name:                "Fast HDD percentiles",
			diskClass:           types.DiskClassFastHDD,
			expectedMinLen:      2,
			expectedPercentiles: []string{"p50", "p95"},
		},
		{
			name:                "Slow HDD percentiles",
			diskClass:           types.DiskClassSlowHDD,
			expectedMinLen:      1,
			expectedPercentiles: []string{"p50"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sample := &types.SampleResult{DiskClass: tt.diskClass}
			strategy := GenerateStrategy(sample, tt.diskClass)

			if len(strategy.Percentiles) < tt.expectedMinLen {
				t.Errorf("GenerateStrategy() percentiles length = %v, want at least %v",
					len(strategy.Percentiles), tt.expectedMinLen)
			}

			for _, expected := range tt.expectedPercentiles {
				found := false
				for _, p := range strategy.Percentiles {
					if p == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected percentile %v not found", expected)
				}
			}
		})
	}
}

func TestGetEstimatedTime(t *testing.T) {
	strategy := &types.TestStrategy{
		TestsPlanned:   []string{"test1", "test2", "test3"},
		RuntimePerTest: 60,
	}

	estimated := GetEstimatedTime(strategy)
	expected := 180

	if estimated != expected {
		t.Errorf("GetEstimatedTime() = %v, want %v", estimated, expected)
	}
}

func TestAbs(t *testing.T) {
	tests := []struct {
		input    int
		expected int
	}{
		{5, 5},
		{-5, 5},
		{0, 0},
		{-100, 100},
		{100, 100},
	}

	for _, tt := range tests {
		result := abs(tt.input)
		if result != tt.expected {
			t.Errorf("abs(%v) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}
