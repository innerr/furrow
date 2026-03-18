package analyzer

import (
	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/fio"
	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

func GenerateStrategy(sample *types.SampleResult, diskClass types.DiskClass) *types.TestStrategy {
	selection, ok := fio.TestSelectionMatrix[diskClass]
	if !ok {
		selection = fio.TestSelectionMatrix[types.DiskClassSlowHDD]
	}

	strategy := &types.TestStrategy{
		TestsPlanned:   make([]string, 0),
		TestsSkipped:   make([]string, 0),
		SkipReasons:    make(map[string]string),
		IODepth:        selection.IODepth,
		NumJobs:        selection.NumJobs,
		Percentiles:    selection.Percentiles,
		RuntimePerTest: 60,
	}

	testsToRun := make(map[string]bool)
	for _, test := range selection.Run {
		testsToRun[test] = true
	}

	if sample != nil {
		if sample.SeqReadBWMBps > 0 && sample.SeqWriteBWMBps > 0 {
			bwDiff := abs(int(sample.SeqReadBWMBps) - int(sample.SeqWriteBWMBps))
			if bwDiff < int(float64(sample.SeqReadBWMBps)*0.1) {
				delete(testsToRun, "seq_write_async_direct")
				strategy.SkipReasons["seq_write_async_direct"] = "read/write bandwidth within 10%"
			}
		}

		if sample.RandReadIOPS > 0 && sample.RandWriteIOPS > 0 {
			iopsDiff := abs(int(sample.RandReadIOPS) - int(sample.RandWriteIOPS))
			if iopsDiff < int(float64(sample.RandReadIOPS)*0.15) {
				delete(testsToRun, "rand_write_4k_async_direct")
				delete(testsToRun, "latency_write")
				strategy.SkipReasons["rand_write_4k_async_direct"] = "read/write IOPS within 15%"
				strategy.SkipReasons["latency_write"] = "read/write IOPS within 15%"
			}
		}
	}

	for _, test := range selection.Run {
		if testsToRun[test] {
			strategy.TestsPlanned = append(strategy.TestsPlanned, test)
		} else {
			strategy.TestsSkipped = append(strategy.TestsSkipped, test)
		}
	}

	for _, test := range selection.Skip {
		strategy.TestsSkipped = append(strategy.TestsSkipped, test)
		if _, hasReason := strategy.SkipReasons[test]; !hasReason {
			strategy.SkipReasons[test] = "not needed for this disk class"
		}
	}

	return strategy
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func GetEstimatedTime(strategy *types.TestStrategy) int {
	return len(strategy.TestsPlanned) * strategy.RuntimePerTest
}
