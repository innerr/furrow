package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/analyzer"
	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/errors"
	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/fio"
	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/fs"
	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/metadata"
	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/prompt"
	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/report"
	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

var (
	runPath   string
	runOutput string
	runQuick  bool
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run disk I/O benchmark",
	Long: `Run a comprehensive disk I/O benchmark using fio.
Without arguments, runs in interactive mode.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBenchmark()
	},
}

func init() {
	runCmd.Flags().StringVar(&runPath, "path", "", "Target filesystem path")
	runCmd.Flags().StringVarP(&runOutput, "output", "o", "./fio-reports", "Output directory for reports")
	runCmd.Flags().BoolVar(&runQuick, "quick", false, "Run quick sampling only (skip Phase 3)")
}

func runBenchmark() error {
	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────────────────────────┐")
	fmt.Println("│  FIO Benchmark Tool                                         │")
	fmt.Println("│  Disk I/O Performance Baseline Testing                      │")
	fmt.Println("└─────────────────────────────────────────────────────────────┘")

	runner, err := fio.NewRunner()
	if err != nil {
		return err
	}

	detector := fs.NewDetector()
	collector := metadata.NewCollector()

	var targetFS *types.Filesystem

	if runPath != "" {
		targetFS, err = detector.Get(runPath)
		if err != nil {
			return fmt.Errorf("invalid path: %w", err)
		}
	} else {
		filesystems, err := detector.List()
		if err != nil {
			return err
		}

		targetFS, err = prompt.SelectFilesystem(filesystems)
		if err != nil {
			return err
		}
	}

	estimatedDiskClass := analyzer.ClassifyFromDiskType(targetFS.DiskType)
	testFileSize := fio.CalculateTestFileSize(targetFS.TotalBytes, targetFS.FreeBytes, estimatedDiskClass)

	testFile, err := runner.CreateTestFile(targetFS.Path, testFileSize)
	if err != nil {
		return err
	}
	defer runner.CleanupTestFile(testFile)

	ctx := context.Background()

	sampleResult, err := runner.RunSampling(ctx, testFile, testFileSize)
	if err != nil {
		return err
	}

	sampleResult.DiskClass = analyzer.Classify(sampleResult)
	targetFS.DiskClass = sampleResult.DiskClass
	prompt.DisplaySamplingResults(sampleResult)

	if runQuick {
		fmt.Println()
		fmt.Println("  Quick mode - skipping deep tests")
		return nil
	}

	strategy := analyzer.GenerateStrategy(sampleResult, sampleResult.DiskClass)
	strategy.RuntimePerTest = 60

	action, err := prompt.ConfirmStrategy(strategy)
	if err != nil {
		return err
	}

	if action == "quit" {
		return errors.ErrUserCancelled
	}

	fmt.Println()
	fmt.Println("[Step 4/4] Running Tests")
	fmt.Println()

	results := make(map[string]types.TestConfigResult)
	rawLogs := make(map[string]string)

	for i, testName := range strategy.TestsPlanned {
		cfg, ok := fio.GetTestConfig(testName)
		if !ok {
			continue
		}

		cfg = fio.AdjustConfigForDiskClass(cfg, sampleResult.DiskClass)
		cfg.Runtime = strategy.RuntimePerTest

		opts := fio.RunOptions{
			TestFile: testFile,
			FileSize: testFileSize,
			Runtime:  cfg.Runtime,
			IODepth:  cfg.IODepth,
			NumJobs:  cfg.NumJobs,
			Direct:   cfg.Direct,
		}

		result, err := runner.Run(ctx, cfg, opts)
		if err != nil {
			prompt.DisplayError(fmt.Sprintf("%s: %v", testName, err))
			continue
		}

		prompt.DisplayTestProgress(i+1, len(strategy.TestsPlanned), testName, &result.Metrics)

		results[testName] = types.TestConfigResult{
			Config:  result.Config,
			Metrics: result.Metrics,
		}
		rawLogs[testName] = result.RawLog
	}

	if len(results) == 0 {
		return fmt.Errorf("all deep tests failed, cannot generate report")
	}

	reportData := buildReport(targetFS, sampleResult, strategy, results, rawLogs, runner, collector, testFileSize, testFile)

	if err := saveReport(reportData, runOutput, len(results), len(strategy.TestsPlanned)); err != nil {
		return err
	}

	return nil
}

func buildReport(target *types.Filesystem, sample *types.SampleResult, strategy *types.TestStrategy,
	results map[string]types.TestConfigResult, rawLogs map[string]string,
	runner *fio.Runner, collector metadata.Collector, testFileSize uint64, testFilePath string) *types.Report {

	hostInfo, _ := collector.CollectHostInfo()
	envInfo, _ := collector.CollectEnvironment()
	fioInfo := runner.GetFioInfo()

	now := time.Now()
	reportID := report.GenerateReportID(hostInfo.Hostname, target.DeviceName, now)

	scores := report.CalculateScores(results, sample.DiskClass)
	overallScore := report.CalculateOverallScore(scores)
	bottleneck, bottleneckDetail := report.IdentifyBottleneck(results, scores)
	recs := report.GenerateRecommendations(scores, sample.DiskClass)

	return &types.Report{
		Metadata: types.ReportMetadata{
			ReportID:    reportID,
			GeneratedAt: now,
			ToolVersion: "1.0.0",
			Host:        *hostInfo,
			Target:      *target,
			Fio:         *fioInfo,
			Environment: *envInfo,
			Test: types.TestInfo{
				Mode:              "adaptive",
				TestFileSizeBytes: testFileSize,
				TestFilePath:      testFilePath,
				IODepth:           strategy.IODepth,
				NumJobs:           strategy.NumJobs,
				TestsRun:          len(results),
				TestsSkipped:      len(strategy.TestsSkipped),
			},
		},
		Phase1Sampling: *sample,
		Phase2Strategy: *strategy,
		Phase3Results:  results,
		Summary: types.ReportSummary{
			Scores:           scores,
			OverallScore:     overallScore,
			Bottleneck:       bottleneck,
			BottleneckDetail: bottleneckDetail,
			Recommendations:  recs,
		},
		RawFioLogs: rawLogs,
	}
}

func saveReport(reportData *types.Report, outputDir string, successCount, totalCount int) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	reportPath := filepath.Join(outputDir, reportData.Metadata.ReportID)

	jsonData, err := report.GenerateJSON(reportData)
	if err != nil {
		return fmt.Errorf("failed to generate JSON report: %w", err)
	}
	if err := os.WriteFile(reportPath+".json", jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write JSON report: %w", err)
	}

	mdData, err := report.GenerateMarkdown(reportData)
	if err != nil {
		return fmt.Errorf("failed to generate Markdown report: %w", err)
	}
	if err := os.WriteFile(reportPath+".md", mdData, 0644); err != nil {
		return fmt.Errorf("failed to write Markdown report: %w", err)
	}

	prompt.DisplayCompletion(reportPath, successCount, totalCount)
	return nil
}
