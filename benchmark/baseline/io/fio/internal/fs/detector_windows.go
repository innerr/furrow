//go:build windows

package fs

import (
	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/errors"
	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

type windowsDetector struct{}

func newPlatformDetector() Detector {
	return &windowsDetector{}
}

func (this *windowsDetector) List() ([]types.Filesystem, error) {
	return nil, errors.ErrNotSupported
}

func (this *windowsDetector) Get(path string) (*types.Filesystem, error) {
	return nil, errors.ErrNotSupported
}
