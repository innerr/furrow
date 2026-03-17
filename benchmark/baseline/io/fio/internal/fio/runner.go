package fio

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/errors"
	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

type Runner struct {
	fioPath string
	version string
}

func NewRunner() (*Runner, error) {
	fioPath, err := exec.LookPath("fio")
	if err != nil {
		return nil, errors.ErrFioNotFound
	}

	version, err := getFioVersion(fioPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get fio version: %w", err)
	}

	return &Runner{
		fioPath: fioPath,
		version: version,
	}, nil
}

func (this *Runner) GetFioInfo() *types.FioInfo {
	return &types.FioInfo{
		Version:      this.version,
		Path:         this.fioPath,
		Capabilities: this.getCapabilities(),
	}
}

func (this *Runner) getCapabilities() []string {
	var caps []string
	cmd := exec.Command(this.fioPath, "--enghelp")
	output, err := cmd.Output()
	if err != nil {
		return caps
	}

	engines := []string{"libaio", "posixaio", "sync", "io_uring", "windowsaio"}
	for _, e := range engines {
		if strings.Contains(string(output), e) {
			caps = append(caps, e)
		}
	}
	return caps
}

func (this *Runner) CheckInstalled() error {
	if _, err := exec.LookPath("fio"); err != nil {
		return errors.ErrFioNotFound
	}
	return nil
}

type RunOptions struct {
	TestFile string
	FileSize uint64
	Runtime  int
	IODepth  int
	NumJobs  int
	Direct   bool
	IOEngine string
}

func (this *Runner) Run(ctx context.Context, cfg types.TestConfig, opts RunOptions) (*types.TestResult, error) {
	startTime := time.Now()

	args := this.buildArgs(cfg, opts)

	cmd := exec.CommandContext(ctx, this.fioPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &types.TestResult{
		Name:     cfg.Name,
		Config:   cfg,
		Duration: time.Since(startTime),
		RawLog:   stdout.String() + "\n" + stderr.String(),
	}

	if err != nil {
		result.Error = fmt.Errorf("%w: %v", errors.ErrFioError, err)
		return result, result.Error
	}

	fioOutput, err := ParseFioOutput(stdout.Bytes())
	if err != nil {
		result.Error = fmt.Errorf("failed to parse fio output: %w", err)
		return result, result.Error
	}

	metrics, err := ExtractMetrics(fioOutput, cfg.Name)
	if err != nil {
		result.Error = fmt.Errorf("failed to extract metrics: %w", err)
		return result, result.Error
	}

	result.Metrics = *metrics
	result.RawLog = stdout.String()

	return result, nil
}

func (this *Runner) buildArgs(cfg types.TestConfig, opts RunOptions) []string {
	args := []string{
		"--output-format=json",
		"--name=" + cfg.Name,
		"--filename=" + opts.TestFile,
		"--rw=" + cfg.RW,
		"--bs=" + cfg.BS,
		"--ioengine=" + this.getIOEngine(cfg.IOEngine),
		"--iodepth=" + strconv.Itoa(overrideValue(cfg.IODepth, opts.IODepth)),
		"--numjobs=" + strconv.Itoa(overrideValue(cfg.NumJobs, opts.NumJobs)),
		"--runtime=" + strconv.Itoa(overrideValue(cfg.Runtime, opts.Runtime)),
		"--time_based",
		"--group_reporting",
		"--randrepeat=0",
	}

	if cfg.Direct && opts.Direct {
		args = append(args, "--direct=1")
	} else {
		args = append(args, "--direct=0")
	}

	if cfg.Fsync {
		args = append(args, "--fsync=1")
	}

	if cfg.RWMixRead > 0 {
		args = append(args, "--rwmixread="+strconv.Itoa(cfg.RWMixRead))
	}

	if cfg.LatPercentiles {
		args = append(args, "--lat_percentiles=1")
	}

	if opts.FileSize > 0 {
		args = append(args, "--size="+formatSize(opts.FileSize))
	}

	return args
}

func (this *Runner) getIOEngine(cfgEngine string) string {
	if cfgEngine == "libaio" && runtime.GOOS == "darwin" {
		return "posixaio"
	}
	return cfgEngine
}

func overrideValue(cfgVal, optVal int) int {
	if optVal > 0 {
		return optVal
	}
	return cfgVal
}

func formatSize(bytes uint64) string {
	if bytes >= TB {
		return fmt.Sprintf("%dT", bytes/TB)
	}
	if bytes >= GB {
		return fmt.Sprintf("%dG", bytes/GB)
	}
	if bytes >= MB {
		return fmt.Sprintf("%dM", bytes/MB)
	}
	if bytes >= KB {
		return fmt.Sprintf("%dK", bytes/KB)
	}
	return fmt.Sprintf("%d", bytes)
}

func (this *Runner) RunSampling(ctx context.Context, testFile string, fileSize uint64) (*types.SampleResult, error) {
	result := &types.SampleResult{}
	totalStart := time.Now()

	sampleRuntime := 5

	for _, testName := range SamplingTests {
		cfg, ok := GetTestConfig(testName)
		if !ok {
			continue
		}

		opts := RunOptions{
			TestFile: testFile,
			FileSize: fileSize,
			Runtime:  sampleRuntime,
			Direct:   cfg.Direct,
		}

		testResult, err := this.Run(ctx, cfg, opts)
		if err != nil {
			return nil, fmt.Errorf("%w: %s failed", errors.ErrSampleFailed, testName)
		}

		if testResult.Metrics.IOPS == 0 && testResult.Metrics.BandwidthMBps == 0 {
			return nil, fmt.Errorf("%w: %s returned zero results", errors.ErrSampleFailed, testName)
		}

		switch testName {
		case "seq_read_async_direct":
			result.SeqReadBWMBps = uint64(testResult.Metrics.BandwidthMBps)
		case "seq_write_async_direct":
			result.SeqWriteBWMBps = uint64(testResult.Metrics.BandwidthMBps)
		case "rand_read_4k_async_direct":
			result.RandReadIOPS = uint64(testResult.Metrics.IOPS)
		case "rand_write_4k_async_direct":
			result.RandWriteIOPS = uint64(testResult.Metrics.IOPS)
		case "fsync_limit":
			result.FsyncIOPS = uint64(testResult.Metrics.IOPS)
		}
	}

	result.Duration = time.Since(totalStart)
	return result, nil
}

func (this *Runner) CreateTestFile(dir string, size uint64) (string, error) {
	testFileName := ".fio-bench-testfile"
	testFilePath := filepath.Join(dir, testFileName)

	f, err := os.Create(testFilePath)
	if err != nil {
		return "", fmt.Errorf("%w: %v", errors.ErrTestFileCreate, err)
	}

	if err := f.Truncate(int64(size)); err != nil {
		f.Close()
		os.Remove(testFilePath)
		return "", fmt.Errorf("%w: failed to allocate file: %v", errors.ErrTestFileCreate, err)
	}

	if err := f.Close(); err != nil {
		os.Remove(testFilePath)
		return "", fmt.Errorf("%w: %v", errors.ErrTestFileCreate, err)
	}

	return testFilePath, nil
}

func (this *Runner) CleanupTestFile(testFile string) {
	if testFile != "" {
		os.Remove(testFile)
	}
}

func getFioVersion(fioPath string) (string, error) {
	cmd := exec.Command(fioPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	version, _ := ParseFioVersion(string(output))
	return version, nil
}
