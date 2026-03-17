//go:build windows

package metadata

import (
	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/errors"
	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

type windowsCollector struct{}

func newPlatformCollector() Collector {
	return &windowsCollector{}
}

func (this *windowsCollector) CollectHostInfo() (*types.HostInfo, error) {
	return nil, errors.ErrNotSupported
}

func (this *windowsCollector) CollectEnvironment() (*types.TestEnvironment, error) {
	return nil, errors.ErrNotSupported
}
