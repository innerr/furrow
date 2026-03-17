package fs

import (
	"fmt"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

type Detector interface {
	List() ([]types.Filesystem, error)
	Get(path string) (*types.Filesystem, error)
}

func NewDetector() Detector {
	return newPlatformDetector()
}

func FormatBytes(bytes uint64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)

	if bytes >= TB {
		return formatFloat(float64(bytes)/float64(TB), 1) + " TB"
	}
	if bytes >= GB {
		return formatFloat(float64(bytes)/float64(GB), 1) + " GB"
	}
	if bytes >= MB {
		return formatFloat(float64(bytes)/float64(MB), 1) + " MB"
	}
	if bytes >= KB {
		return formatFloat(float64(bytes)/float64(KB), 1) + " KB"
	}
	return formatFloat(float64(bytes), 0) + " B"
}

func formatFloat(f float64, precision int) string {
	if precision == 0 {
		return fmt.Sprintf("%d", int(f))
	}
	return fmt.Sprintf(fmt.Sprintf("%%.%df", precision), f)
}
