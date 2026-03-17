//go:build darwin

package fs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/errors"
	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

const DKIOCGETBLOCKSIZE = 0x40046418

type darwinDetector struct{}

func newPlatformDetector() Detector {
	return &darwinDetector{}
}

func (this *darwinDetector) List() ([]types.Filesystem, error) {
	output, err := exec.Command("df", "-l").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run df: %w", err)
	}

	var filesystems []types.Filesystem
	lines := strings.Split(string(output), "\n")

	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}

		fs, err := this.getFSInfo(fields[0], fields[8])
		if err != nil {
			continue
		}

		if this.shouldSkipFS(fs) {
			continue
		}

		filesystems = append(filesystems, *fs)
	}

	if len(filesystems) == 0 {
		return nil, errors.ErrNoFilesystems
	}

	return filesystems, nil
}

func (this *darwinDetector) Get(path string) (*types.Filesystem, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, errors.ErrInvalidPath
	}

	stat, err := os.Stat(absPath)
	if err != nil {
		return nil, errors.ErrInvalidPath
	}

	var targetPath string
	if stat.IsDir() {
		targetPath = absPath
	} else {
		targetPath = filepath.Dir(absPath)
	}

	output, err := exec.Command("df", targetPath).Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run df: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return nil, errors.ErrInvalidPath
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 9 {
		return nil, errors.ErrInvalidPath
	}

	return this.getFSInfo(fields[0], fields[8])
}

func (this *darwinDetector) shouldSkipFS(fs *types.Filesystem) bool {
	skipFS := map[string]bool{
		"devfs":  true,
		"autofs": true,
		"map":    true,
		"vmhgfs": true,
	}

	if skipFS[fs.FilesystemType] {
		return true
	}

	if strings.HasPrefix(fs.FilesystemType, "map ") {
		return true
	}

	return false
}

func (this *darwinDetector) getFSInfo(device, mountPoint string) (*types.Filesystem, error) {
	fs := &types.Filesystem{
		Path:       mountPoint,
		MountPoint: mountPoint,
		DevicePath: device,
		DeviceName: filepath.Base(device),
	}

	if stat, err := this.getStat(mountPoint); err == nil {
		fs.TotalBytes = stat.TotalBytes
		fs.FreeBytes = stat.FreeBytes
		fs.AvailableBytes = stat.AvailableBytes
	}

	this.getDiskInfo(fs, device)

	return fs, nil
}

func (this *darwinDetector) getDiskInfo(fs *types.Filesystem, device string) {
	output, err := exec.Command("diskutil", "info", device).Output()
	if err != nil {
		return
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Device / Node:") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				fs.DevicePath = fields[len(fields)-1]
			}
		} else if strings.HasPrefix(line, "Device / Media Name:") {
			idx := strings.Index(line, ":")
			if idx >= 0 {
				fs.DeviceModel = strings.TrimSpace(line[idx+1:])
			}
		} else if strings.HasPrefix(line, "Solid State:") {
			idx := strings.Index(line, ":")
			if idx >= 0 {
				val := strings.TrimSpace(line[idx+1:])
				if val == "Yes" {
					fs.Rotational = false
					fs.DiskType = "ssd"
				} else {
					fs.Rotational = true
					fs.DiskType = "hdd"
				}
			}
		} else if strings.HasPrefix(line, "File System Personality:") {
			idx := strings.Index(line, ":")
			if idx >= 0 {
				fs.FilesystemType = strings.TrimSpace(line[idx+1:])
			}
		}
	}

	if strings.Contains(device, "disk") {
		if fs.DiskType == "" {
			fs.DiskType = "hdd"
			fs.Rotational = true
			if strings.HasPrefix(fs.DeviceModel, "APPLE SSD") ||
				strings.Contains(strings.ToUpper(fs.DeviceModel), "SSD") {
				fs.DiskType = "ssd"
				fs.Rotational = false
			}
		}
	}

	if strings.HasPrefix(device, "/dev/disk") {
		this.getBlockSize(fs, device)
	}
}

func (this *darwinDetector) getBlockSize(fs *types.Filesystem, device string) {
	fd, err := syscall.Open(device, syscall.O_RDONLY, 0)
	if err != nil {
		return
	}
	defer syscall.Close(fd)

	var devBlockSize uint64
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), DKIOCGETBLOCKSIZE, uintptr(unsafe.Pointer(&devBlockSize)), 0, 0, 0)
	if errno == 0 {
		fs.PhysicalBlockSize = devBlockSize
		fs.LogicalBlockSize = devBlockSize
	} else {
		fs.PhysicalBlockSize = 512
		fs.LogicalBlockSize = 512
	}
}

type statInfo struct {
	TotalBytes     uint64
	FreeBytes      uint64
	AvailableBytes uint64
}

func (this *darwinDetector) getStat(path string) (*statInfo, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, err
	}

	return &statInfo{
		TotalBytes:     uint64(stat.Blocks) * uint64(stat.Bsize),
		FreeBytes:      uint64(stat.Bfree) * uint64(stat.Bsize),
		AvailableBytes: uint64(stat.Bavail) * uint64(stat.Bsize),
	}, nil
}
