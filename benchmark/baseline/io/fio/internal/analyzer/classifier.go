package analyzer

import (
	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

func Classify(sample *types.SampleResult) types.DiskClass {
	if sample.SeqReadBWMBps > 2000 && sample.RandReadIOPS > 500000 {
		return types.DiskClassNVMeSSD
	}

	if sample.SeqReadBWMBps > 400 && sample.RandReadIOPS > 50000 {
		return types.DiskClassSATASSD
	}

	if sample.SeqReadBWMBps > 150 {
		return types.DiskClassFastHDD
	}

	return types.DiskClassSlowHDD
}

func ClassifyFromDiskType(diskType string) types.DiskClass {
	switch diskType {
	case "nvme":
		return types.DiskClassNVMeSSD
	case "ssd":
		return types.DiskClassSATASSD
	case "hdd":
		return types.DiskClassFastHDD
	default:
		return types.DiskClassSlowHDD
	}
}
