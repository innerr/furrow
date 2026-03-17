package errors

import "errors"

var (
	ErrFioNotFound       = errors.New("fio not found: please install fio")
	ErrNotSupported      = errors.New("platform not supported")
	ErrSampleFailed      = errors.New("sampling test failed")
	ErrInsufficientSpace = errors.New("insufficient disk space for test file")
	ErrTestFileCreate    = errors.New("failed to create test file")
	ErrInvalidPath       = errors.New("invalid or non-existent path")
	ErrFioError          = errors.New("fio execution failed")
	ErrNoFilesystems     = errors.New("no mountable filesystems found")
	ErrUserCancelled     = errors.New("user cancelled")
)
