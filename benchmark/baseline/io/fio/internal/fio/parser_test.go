package fio

import (
	"testing"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

func TestParseFioVersion(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedVer    string
		expectedNumLen int
	}{
		{
			name:           "standard version",
			input:          "fio-3.33\n",
			expectedVer:    "fio-3.33",
			expectedNumLen: 2,
		},
		{
			name:           "version with three parts",
			input:          "fio-3.33.1",
			expectedVer:    "fio-3.33.1",
			expectedNumLen: 3,
		},
		{
			name:           "version without prefix",
			input:          "3.28",
			expectedVer:    "3.28",
			expectedNumLen: 0,
		},
		{
			name:           "empty string",
			input:          "",
			expectedVer:    "",
			expectedNumLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ver, num := ParseFioVersion(tt.input)
			if ver != tt.expectedVer {
				t.Errorf("ParseFioVersion() version = %v, want %v", ver, tt.expectedVer)
			}
			if len(num) != tt.expectedNumLen {
				t.Errorf("ParseFioVersion() numeric length = %v, want %v", len(num), tt.expectedNumLen)
			}
		})
	}
}

func TestParseFioOutput(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantErr    bool
		checkField func(*FioOutput) bool
	}{
		{
			name: "valid json output",
			input: `{
				"fio version": "fio-3.33",
				"timestamp": 1234567890,
				"time": "2024-01-01T00:00:00Z",
				"jobs": [{
					"jobname": "test_job",
					"groupid": 0,
					"error": 0,
					"read": {
						"io_bytes": 1073741824,
						"bw": 1048576,
						"iops": 100000.0,
						"latency_us": {
							"min": 10,
							"max": 100,
							"mean": 50.5,
							"stddev": 20.0,
							"n": 1000,
							"percentile": {"99.0": 80.0, "99.9": 95.0}
						}
					},
					"write": {
						"io_bytes": 0,
						"bw": 0,
						"iops": 0
					}
				}]
			}`,
			wantErr: false,
			checkField: func(out *FioOutput) bool {
				return out.FioVersion == "fio-3.33" && len(out.Jobs) == 1
			},
		},
		{
			name:    "invalid json",
			input:   `{invalid json`,
			wantErr: true,
		},
		{
			name:    "empty json",
			input:   `{}`,
			wantErr: false,
			checkField: func(out *FioOutput) bool {
				return len(out.Jobs) == 0
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseFioOutput([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFioOutput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.checkField != nil {
				if !tt.checkField(result) {
					t.Errorf("ParseFioOutput() field check failed")
				}
			}
		})
	}
}

func TestExtractMetrics(t *testing.T) {
	tests := []struct {
		name         string
		fioOutput    *FioOutput
		jobName      string
		wantErr      bool
		expectedBW   float64
		expectedIOPS float64
		expectedLat  float64
	}{
		{
			name: "read job",
			fioOutput: &FioOutput{
				Jobs: []FioJob{
					{
						JobName: "seq_read",
						Read: FioRWStats{
							IOBytes:   1073741824,
							Bandwidth: 1048576,
							IOPS:      100000.0,
							LatencyUS: FioLatencyUS{
								Min:    10,
								Max:    100,
								Mean:   50.0,
								StdDev: 20.0,
								N:      1000,
								Pct:    map[string]float64{"99.0": 80.0},
							},
						},
						Write:  FioRWStats{},
						UsrCPU: 10.0,
						SysCPU: 5.0,
					},
				},
			},
			jobName:      "seq_read",
			wantErr:      false,
			expectedBW:   1024.0,
			expectedIOPS: 100000.0,
			expectedLat:  50.0,
		},
		{
			name: "write job",
			fioOutput: &FioOutput{
				Jobs: []FioJob{
					{
						JobName: "seq_write",
						Read:    FioRWStats{},
						Write: FioRWStats{
							IOBytes:   536870912,
							Bandwidth: 524288,
							IOPS:      50000.0,
							LatencyUS: FioLatencyUS{
								Min:    20,
								Max:    200,
								Mean:   100.0,
								StdDev: 40.0,
								N:      500,
							},
						},
						UsrCPU: 8.0,
						SysCPU: 12.0,
					},
				},
			},
			jobName:      "seq_write",
			wantErr:      false,
			expectedBW:   512.0,
			expectedIOPS: 50000.0,
			expectedLat:  100.0,
		},
		{
			name: "mixed read-write job",
			fioOutput: &FioOutput{
				Jobs: []FioJob{
					{
						JobName: "randrw",
						Read: FioRWStats{
							IOBytes:   536870912,
							Bandwidth: 524288,
							IOPS:      128000.0,
							LatencyUS: FioLatencyUS{Mean: 30.0, N: 1},
						},
						Write: FioRWStats{
							IOBytes:   536870912,
							Bandwidth: 524288,
							IOPS:      128000.0,
						},
						UsrCPU: 15.0,
						SysCPU: 10.0,
					},
				},
			},
			jobName:      "randrw",
			wantErr:      false,
			expectedBW:   1024.0,
			expectedIOPS: 256000.0,
		},
		{
			name: "job not found",
			fioOutput: &FioOutput{
				Jobs: []FioJob{
					{JobName: "other_job"},
				},
			},
			jobName: "nonexistent",
			wantErr: true,
		},
		{
			name: "latency in nanoseconds",
			fioOutput: &FioOutput{
				Jobs: []FioJob{
					{
						JobName: "fast_read",
						Read: FioRWStats{
							IOBytes:   1073741824,
							Bandwidth: 2097152,
							IOPS:      500000.0,
							LatencyUS: FioLatencyUS{N: 0},
							LatencyNS: FioLatencyNS{
								Min:    1000,
								Max:    10000,
								Mean:   5000.0,
								StdDev: 2000.0,
								N:      1000,
							},
						},
						Write: FioRWStats{},
					},
				},
			},
			jobName:      "fast_read",
			wantErr:      false,
			expectedBW:   2048.0,
			expectedIOPS: 500000.0,
			expectedLat:  5.0,
		},
		{
			name: "latency in milliseconds",
			fioOutput: &FioOutput{
				Jobs: []FioJob{
					{
						JobName: "slow_read",
						Read: FioRWStats{
							IOBytes:   1073741824,
							Bandwidth: 102400,
							IOPS:      1000.0,
							LatencyUS: FioLatencyUS{N: 0},
							LatencyNS: FioLatencyNS{N: 0},
							LatencyMS: FioLatencyMS{
								Min:    1,
								Max:    10,
								Mean:   5.0,
								StdDev: 2.0,
								N:      100,
							},
						},
						Write: FioRWStats{},
					},
				},
			},
			jobName:      "slow_read",
			wantErr:      false,
			expectedBW:   100.0,
			expectedIOPS: 1000.0,
			expectedLat:  5000.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics, err := ExtractMetrics(tt.fioOutput, tt.jobName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractMetrics() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if metrics.BandwidthMBps != tt.expectedBW {
				t.Errorf("ExtractMetrics() BandwidthMBps = %v, want %v", metrics.BandwidthMBps, tt.expectedBW)
			}
			if metrics.IOPS != tt.expectedIOPS {
				t.Errorf("ExtractMetrics() IOPS = %v, want %v", metrics.IOPS, tt.expectedIOPS)
			}
			if tt.expectedLat > 0 && metrics.LatencyMean != tt.expectedLat {
				t.Errorf("ExtractMetrics() LatencyMean = %v, want %v", metrics.LatencyMean, tt.expectedLat)
			}
		})
	}
}

func TestDetermineRW(t *testing.T) {
	tests := []struct {
		name     string
		job      *FioJob
		expected string
	}{
		{
			name: "read only",
			job: &FioJob{
				Read:  FioRWStats{IOBytes: 1000},
				Write: FioRWStats{IOBytes: 0},
			},
			expected: "read",
		},
		{
			name: "write only",
			job: &FioJob{
				Read:  FioRWStats{IOBytes: 0},
				Write: FioRWStats{IOBytes: 1000},
			},
			expected: "write",
		},
		{
			name: "mixed read-write",
			job: &FioJob{
				Read:  FioRWStats{IOBytes: 1000},
				Write: FioRWStats{IOBytes: 1000},
			},
			expected: "randrw",
		},
		{
			name: "no IO",
			job: &FioJob{
				Read:  FioRWStats{IOBytes: 0},
				Write: FioRWStats{IOBytes: 0},
			},
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := determineRW(tt.job)
			if result != tt.expected {
				t.Errorf("determineRW() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestNormalizePercentileKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"99.0", "p99"},
		{"99.00", "p99"},
		{"99.9", "p99.9"},
		{"99.90", "p99.9"},
		{"50.0", "p50"},
		{"0", "p0"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizePercentileKey(tt.input)
			if result != tt.expected {
				t.Errorf("normalizePercentileKey(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExtractLatencyPercentiles(t *testing.T) {
	rw := &FioRWStats{
		LatencyUS: FioLatencyUS{
			N:   1000,
			Min: 10,
			Max: 100,
			Pct: map[string]float64{
				"50.0":  30.0,
				"95.0":  70.0,
				"99.0":  90.0,
				"99.9":  98.0,
				"99.99": 99.9,
			},
		},
	}

	metrics := &types.TestMetrics{
		LatencyPercentiles: make(map[string]float64),
	}

	extractLatency(rw, metrics)

	expectedKeys := []string{"p50", "p95", "p99", "p99.9", "p99.99"}
	for _, key := range expectedKeys {
		if _, ok := metrics.LatencyPercentiles[key]; !ok {
			t.Errorf("Expected percentile key %v not found", key)
		}
	}

	if metrics.LatencyPercentiles["p99"] != 90.0 {
		t.Errorf("p99 latency = %v, want 90.0", metrics.LatencyPercentiles["p99"])
	}
}
