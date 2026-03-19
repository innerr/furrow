package fio

import (
	"runtime"
	"testing"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

func TestNewRunner(t *testing.T) {
	runner, err := NewRunner()

	if err != nil {
		t.Logf("NewRunner() error = %v (fio may not be installed)", err)
		return
	}

	if runner == nil {
		t.Error("NewRunner() returned nil runner")
		return
	}

	if runner.fioPath == "" {
		t.Error("NewRunner() runner has empty fioPath")
	}

	if runner.version == "" {
		t.Error("NewRunner() runner has empty version")
	}
}

func TestGetFioInfo(t *testing.T) {
	runner, err := NewRunner()
	if err != nil {
		t.Skipf("Skipping test: fio not installed: %v", err)
	}

	info := runner.GetFioInfo()

	if info == nil {
		t.Fatal("GetFioInfo() returned nil")
	}

	if info.Version == "" {
		t.Error("GetFioInfo() version is empty")
	}

	if info.Path == "" {
		t.Error("GetFioInfo() path is empty")
	}
}

func TestGetIOEngine(t *testing.T) {
	runner := &Runner{fioPath: "/usr/bin/fio", version: "fio-3.33"}

	expectedLibaio := "libaio"
	if runtime.GOOS == "darwin" {
		expectedLibaio = "posixaio"
	}

	tests := []struct {
		cfgEngine string
		expected  string
	}{
		{"libaio", expectedLibaio},
		{"posixaio", "posixaio"},
		{"sync", "sync"},
		{"io_uring", "io_uring"},
	}

	for _, tt := range tests {
		result := runner.getIOEngine(tt.cfgEngine)
		if result != tt.expected {
			t.Errorf("getIOEngine(%v) = %v, want %v", tt.cfgEngine, result, tt.expected)
		}
	}
}

func TestOverrideValue(t *testing.T) {
	tests := []struct {
		cfgVal   int
		optVal   int
		expected int
	}{
		{10, 20, 20},
		{10, 0, 10},
		{0, 20, 20},
		{0, 0, 0},
		{5, -1, 5},
	}

	for _, tt := range tests {
		result := overrideValue(tt.cfgVal, tt.optVal)
		if result != tt.expected {
			t.Errorf("overrideValue(%v, %v) = %v, want %v", tt.cfgVal, tt.optVal, result, tt.expected)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes    uint64
		expected string
	}{
		{0, "0"},
		{512, "512"},
		{KB, "1K"},
		{MB, "1M"},
		{GB, "1G"},
		{TB, "1T"},
		{2 * GB, "2G"},
		{4 * MB, "4M"},
		{512 * KB, "512K"},
	}

	for _, tt := range tests {
		result := formatSize(tt.bytes)
		if result != tt.expected {
			t.Errorf("formatSize(%v) = %v, want %v", tt.bytes, result, tt.expected)
		}
	}
}

func TestBuildArgs(t *testing.T) {
	runner := &Runner{fioPath: "/usr/bin/fio", version: "fio-3.33"}

	cfg := types.TestConfig{
		Name:      "test_job",
		RW:        "randread",
		BS:        "4k",
		IOEngine:  "libaio",
		Direct:    true,
		Fsync:     false,
		IODepth:   32,
		NumJobs:   4,
		Runtime:   60,
		RWMixRead: 0,
	}

	opts := RunOptions{
		TestFile: "/tmp/testfile",
		FileSize: 1 * GB,
		Runtime:  60,
		IODepth:  32,
		NumJobs:  4,
		Direct:   true,
	}

	args := runner.buildArgs(cfg, opts)

	requiredArgs := []string{
		"--output-format=json",
		"--name=test_job",
		"--filename=/tmp/testfile",
		"--rw=randread",
		"--bs=4k",
		"--direct=1",
		"--iodepth=32",
		"--numjobs=4",
		"--runtime=60",
		"--time_based",
		"--group_reporting",
		"--size=1G",
	}

	for _, required := range requiredArgs {
		found := false
		for _, arg := range args {
			if arg == required {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("buildArgs() missing required argument: %v", required)
		}
	}
}

func TestBuildArgsWithDirect(t *testing.T) {
	runner := &Runner{fioPath: "/usr/bin/fio", version: "fio-3.33"}

	cfg := types.TestConfig{
		Name:     "buffered_test",
		RW:       "read",
		BS:       "1M",
		IOEngine: "sync",
		Direct:   true,
	}

	tests := []struct {
		name        string
		optsDirect  bool
		expectedArg string
	}{
		{"direct enabled", true, "--direct=1"},
		{"direct disabled", false, "--direct=0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := RunOptions{
				TestFile: "/tmp/test",
				Direct:   tt.optsDirect,
			}

			args := runner.buildArgs(cfg, opts)

			found := false
			for _, arg := range args {
				if arg == tt.expectedArg {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("buildArgs() missing %v", tt.expectedArg)
			}
		})
	}
}

func TestBuildArgsWithFsync(t *testing.T) {
	runner := &Runner{fioPath: "/usr/bin/fio", version: "fio-3.33"}

	cfg := types.TestConfig{
		Name:     "fsync_test",
		RW:       "write",
		BS:       "4k",
		IOEngine: "sync",
		Fsync:    true,
	}

	opts := RunOptions{
		TestFile: "/tmp/test",
	}

	args := runner.buildArgs(cfg, opts)

	found := false
	for _, arg := range args {
		if arg == "--fsync=1" {
			found = true
			break
		}
	}

	if !found {
		t.Error("buildArgs() missing --fsync=1 for fsync test")
	}
}

func TestBuildArgsWithRWMixRead(t *testing.T) {
	runner := &Runner{fioPath: "/usr/bin/fio", version: "fio-3.33"}

	cfg := types.TestConfig{
		Name:      "mixed_test",
		RW:        "randrw",
		BS:        "4k",
		IOEngine:  "libaio",
		RWMixRead: 70,
	}

	opts := RunOptions{
		TestFile: "/tmp/test",
	}

	args := runner.buildArgs(cfg, opts)

	found := false
	for _, arg := range args {
		if arg == "--rwmixread=70" {
			found = true
			break
		}
	}

	if !found {
		t.Error("buildArgs() missing --rwmixread=70 for mixed workload")
	}
}

func TestBuildArgsWithLatPercentiles(t *testing.T) {
	runner := &Runner{fioPath: "/usr/bin/fio", version: "fio-3.33"}

	cfg := types.TestConfig{
		Name:           "latency_test",
		RW:             "randread",
		BS:             "4k",
		IOEngine:       "libaio",
		LatPercentiles: true,
	}

	opts := RunOptions{
		TestFile: "/tmp/test",
	}

	args := runner.buildArgs(cfg, opts)

	found := false
	for _, arg := range args {
		if arg == "--lat_percentiles=1" {
			found = true
			break
		}
	}

	if !found {
		t.Error("buildArgs() missing --lat_percentiles=1 for latency test")
	}
}

func TestCheckInstalled(t *testing.T) {
	runner := &Runner{fioPath: "/usr/bin/fio", version: "fio-3.33"}

	err := runner.CheckInstalled()
	if err != nil {
		t.Logf("CheckInstalled() = %v (fio may not be installed)", err)
	}
}
