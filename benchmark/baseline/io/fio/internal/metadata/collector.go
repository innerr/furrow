package metadata

import (
	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

type Collector interface {
	CollectHostInfo() (*types.HostInfo, error)
	CollectEnvironment() (*types.TestEnvironment, error)
}

func NewCollector() Collector {
	return newPlatformCollector()
}
