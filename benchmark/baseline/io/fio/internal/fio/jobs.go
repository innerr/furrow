package fio

import (
	"runtime"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

const (
	KB = 1024
	MB = 1024 * KB
	GB = 1024 * MB
	TB = 1024 * GB
)

func getDefaultIOEngine() string {
	if runtime.GOOS == "darwin" {
		return "posixaio"
	}
	return "libaio"
}

var TestCatalog = map[string]types.TestConfig{
	"seq_read_async_direct": {
		Name: "seq_read_async_direct", RW: "read", BS: "1M",
		IOEngine: getDefaultIOEngine(), Direct: true, IODepth: 32, NumJobs: 4,
	},
	"seq_write_async_direct": {
		Name: "seq_write_async_direct", RW: "write", BS: "1M",
		IOEngine: getDefaultIOEngine(), Direct: true, IODepth: 32, NumJobs: 4,
	},
	"seq_read_sync_direct": {
		Name: "seq_read_sync_direct", RW: "read", BS: "1M",
		IOEngine: "sync", Direct: true, IODepth: 1, NumJobs: 1,
	},
	"seq_write_sync_direct": {
		Name: "seq_write_sync_direct", RW: "write", BS: "1M",
		IOEngine: "sync", Direct: true, IODepth: 1, NumJobs: 1,
	},
	"seq_read_buffered": {
		Name: "seq_read_buffered", RW: "read", BS: "1M",
		IOEngine: "sync", Direct: false, IODepth: 1, NumJobs: 1,
	},
	"seq_write_buffered": {
		Name: "seq_write_buffered", RW: "write", BS: "1M",
		IOEngine: "sync", Direct: false, IODepth: 1, NumJobs: 1,
	},
	"rand_read_4k_async_direct": {
		Name: "rand_read_4k_async_direct", RW: "randread", BS: "4k",
		IOEngine: getDefaultIOEngine(), Direct: true, IODepth: 32, NumJobs: 4,
	},
	"rand_write_4k_async_direct": {
		Name: "rand_write_4k_async_direct", RW: "randwrite", BS: "4k",
		IOEngine: getDefaultIOEngine(), Direct: true, IODepth: 32, NumJobs: 4,
	},
	"rand_read_4k_sync_direct": {
		Name: "rand_read_4k_sync_direct", RW: "randread", BS: "4k",
		IOEngine: "sync", Direct: true, IODepth: 1, NumJobs: 1,
	},
	"rand_write_4k_sync_direct": {
		Name: "rand_write_4k_sync_direct", RW: "randwrite", BS: "4k",
		IOEngine: "sync", Direct: true, IODepth: 1, NumJobs: 1,
	},
	"rand_read_4k_buffered": {
		Name: "rand_read_4k_buffered", RW: "randread", BS: "4k",
		IOEngine: "sync", Direct: false, IODepth: 1, NumJobs: 1,
	},
	"rand_write_4k_buffered": {
		Name: "rand_write_4k_buffered", RW: "randwrite", BS: "4k",
		IOEngine: "sync", Direct: false, IODepth: 1, NumJobs: 1,
	},
	"mixed_70_30": {
		Name: "mixed_70_30", RW: "randrw", BS: "4k", RWMixRead: 70,
		IOEngine: getDefaultIOEngine(), Direct: true, IODepth: 32, NumJobs: 4,
	},
	"mixed_50_50": {
		Name: "mixed_50_50", RW: "randrw", BS: "4k", RWMixRead: 50,
		IOEngine: getDefaultIOEngine(), Direct: true, IODepth: 32, NumJobs: 4,
	},
	"multibs_read": {
		Name: "multibs_read", RW: "randread", BS: "4k,16k,64k,256k,1M",
		IOEngine: getDefaultIOEngine(), Direct: true, IODepth: 32, NumJobs: 4,
	},
	"multibs_write": {
		Name: "multibs_write", RW: "randwrite", BS: "4k,16k,64k,256k,1M",
		IOEngine: getDefaultIOEngine(), Direct: true, IODepth: 32, NumJobs: 4,
	},
	"fsync_limit": {
		Name: "fsync_limit", RW: "write", BS: "4k",
		IOEngine: "sync", Direct: false, Fsync: true, IODepth: 1, NumJobs: 1,
	},
	"latency_read": {
		Name: "latency_read", RW: "randread", BS: "4k",
		IOEngine: getDefaultIOEngine(), Direct: true, IODepth: 32, NumJobs: 4, LatPercentiles: true,
	},
	"latency_write": {
		Name: "latency_write", RW: "randwrite", BS: "4k",
		IOEngine: getDefaultIOEngine(), Direct: true, IODepth: 32, NumJobs: 4, LatPercentiles: true,
	},
}

var SamplingTests = []string{
	"seq_read_async_direct",
	"seq_write_async_direct",
	"rand_read_4k_async_direct",
	"rand_write_4k_async_direct",
	"fsync_limit",
}

var TestSelectionMatrix = map[types.DiskClass]types.TestSelection{
	types.DiskClassNVMeSSD: {
		Run: []string{
			"seq_read_async_direct", "seq_write_async_direct",
			"rand_read_4k_async_direct", "rand_write_4k_async_direct",
			"rand_read_4k_sync_direct",
			"mixed_70_30", "mixed_50_50",
			"multibs_read", "multibs_write",
			"fsync_limit",
			"latency_read", "latency_write",
		},
		Skip: []string{
			"seq_read_sync_direct", "seq_write_sync_direct",
			"seq_read_buffered", "seq_write_buffered",
			"rand_write_4k_sync_direct",
			"rand_read_4k_buffered", "rand_write_4k_buffered",
		},
		IODepth:     32,
		NumJobs:     4,
		Percentiles: []string{"p50", "p95", "p99", "p99.9", "p99.99"},
	},
	types.DiskClassSATASSD: {
		Run: []string{
			"seq_read_async_direct", "seq_write_async_direct",
			"seq_read_sync_direct", "seq_write_sync_direct",
			"seq_read_buffered", "seq_write_buffered",
			"rand_read_4k_async_direct", "rand_write_4k_async_direct",
			"rand_read_4k_sync_direct", "rand_write_4k_sync_direct",
			"rand_read_4k_buffered", "rand_write_4k_buffered",
			"mixed_70_30",
			"multibs_read", "multibs_write",
			"fsync_limit",
			"latency_read", "latency_write",
		},
		Skip:        []string{"mixed_50_50"},
		IODepth:     32,
		NumJobs:     4,
		Percentiles: []string{"p50", "p95", "p99", "p99.9"},
	},
	types.DiskClassFastHDD: {
		Run: []string{
			"seq_read_async_direct", "seq_write_async_direct",
			"seq_read_sync_direct", "seq_write_sync_direct",
			"seq_read_buffered", "seq_write_buffered",
			"rand_read_4k_async_direct", "rand_write_4k_async_direct",
			"latency_read",
		},
		Skip: []string{
			"rand_read_4k_sync_direct", "rand_write_4k_sync_direct",
			"rand_read_4k_buffered", "rand_write_4k_buffered",
			"mixed_70_30", "mixed_50_50",
			"multibs_read", "multibs_write",
			"fsync_limit", "latency_write",
		},
		IODepth:     8,
		NumJobs:     2,
		Percentiles: []string{"p50", "p95", "p99"},
	},
	types.DiskClassSlowHDD: {
		Run: []string{
			"seq_read_async_direct", "seq_write_async_direct",
			"seq_read_sync_direct", "seq_write_sync_direct",
			"seq_read_buffered", "seq_write_buffered",
			"rand_read_4k_async_direct", "rand_write_4k_async_direct",
			"latency_read",
		},
		Skip: []string{
			"rand_read_4k_sync_direct", "rand_write_4k_sync_direct",
			"rand_read_4k_buffered", "rand_write_4k_buffered",
			"mixed_70_30", "mixed_50_50",
			"multibs_read", "multibs_write",
			"fsync_limit", "latency_write",
		},
		IODepth:     8,
		NumJobs:     2,
		Percentiles: []string{"p50", "p95"},
	},
}

var SizeTiers = []types.SizeTier{
	{MinGB: 0, MaxGB: 64, DefaultSizeGB: 1, MinFreeRatio: 0.15},
	{MinGB: 64, MaxGB: 256, DefaultSizeGB: 2, MinFreeRatio: 0.15},
	{MinGB: 256, MaxGB: 1024, DefaultSizeGB: 4, MinFreeRatio: 0.15},
	{MinGB: 1024, MaxGB: 0, DefaultSizeGB: 8, MinFreeRatio: 0.15},
}

func CalculateTestFileSize(partitionSize, freeSpace uint64, diskClass types.DiskClass) uint64 {
	var tier *types.SizeTier
	for i := range SizeTiers {
		t := &SizeTiers[i]
		if t.MaxGB == 0 {
			tier = t
			break
		}
		if partitionSize/GB >= t.MinGB && partitionSize/GB < t.MaxGB {
			tier = t
			break
		}
	}
	if tier == nil {
		tier = &SizeTiers[len(SizeTiers)-1]
	}

	size := tier.DefaultSizeGB * GB

	if diskClass == types.DiskClassSlowHDD || diskClass == types.DiskClassFastHDD {
		size = uint64(float64(size) * 0.6)
	}

	maxAllowed := uint64(float64(freeSpace) * 0.25)
	minSize := uint64(GB)

	if maxAllowed == 0 {
		return 0
	}

	if size > maxAllowed {
		size = maxAllowed
	}

	if maxAllowed >= minSize && size < minSize {
		size = minSize
	}

	return size
}

func GetTestConfig(name string) (types.TestConfig, bool) {
	cfg, ok := TestCatalog[name]
	return cfg, ok
}

func AdjustConfigForDiskClass(cfg types.TestConfig, diskClass types.DiskClass) types.TestConfig {
	selection, ok := TestSelectionMatrix[diskClass]
	if !ok {
		return cfg
	}

	cfg.IODepth = selection.IODepth
	cfg.NumJobs = selection.NumJobs
	return cfg
}
