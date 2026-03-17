//go:build linux

package fs

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/errors"
	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

type linuxDetector struct{}

func newPlatformDetector() Detector {
	return &linuxDetector{}
}

func (this *linuxDetector) List() ([]types.Filesystem, error) {
	mounts, err := this.parseMounts()
	if err != nil {
		return nil, err
	}

	var filesystems []types.Filesystem
	for _, mount := range mounts {
		if this.shouldSkipMount(mount) {
			continue
		}

		fs, err := this.getMountInfo(mount)
		if err != nil {
			continue
		}

		filesystems = append(filesystems, *fs)
	}

	if len(filesystems) == 0 {
		return nil, errors.ErrNoFilesystems
	}

	return filesystems, nil
}

func (this *linuxDetector) Get(path string) (*types.Filesystem, error) {
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

	mounts, err := this.parseMounts()
	if err != nil {
		return nil, err
	}

	var bestMount *mountInfo
	for _, mount := range mounts {
		if strings.HasPrefix(targetPath, mount.MountPoint) {
			if bestMount == nil || len(mount.MountPoint) > len(bestMount.MountPoint) {
				bestMount = &mount
			}
		}
	}

	if bestMount == nil {
		return nil, errors.ErrInvalidPath
	}

	return this.getMountInfo(*bestMount)
}

type mountInfo struct {
	Device     string
	MountPoint string
	FSType     string
	MountOpts  string
}

func (this *linuxDetector) parseMounts() ([]mountInfo, error) {
	file, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, fmt.Errorf("failed to open /proc/mounts: %w", err)
	}
	defer file.Close()

	var mounts []mountInfo
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}

		mounts = append(mounts, mountInfo{
			Device:     fields[0],
			MountPoint: fields[1],
			FSType:     fields[2],
			MountOpts:  fields[3],
		})
	}

	return mounts, scanner.Err()
}

func (this *linuxDetector) shouldSkipMount(mount mountInfo) bool {
	skipFS := map[string]bool{
		"sysfs":      true,
		"proc":       true,
		"devtmpfs":   true,
		"devpts":     true,
		"tmpfs":      true,
		"securityfs": true,
		"cgroup":     true,
		"cgroup2":    true,
		"pstore":     true,
		"debugfs":    true,
		"tracefs":    true,
		"configfs":   true,
		"fusectl":    true,
		"mqueue":     true,
		"hugetlbfs":  true,
		"autofs":     true,
		"overlay":    true,
		"squashfs":   true,
		"rpc_pipefs": true,
	}

	if skipFS[mount.FSType] {
		return true
	}

	if strings.HasPrefix(mount.Device, "none") {
		return true
	}

	return false
}

func (this *linuxDetector) getMountInfo(mount mountInfo) (*types.Filesystem, error) {
	fs := &types.Filesystem{
		Path:           mount.MountPoint,
		MountPoint:     mount.MountPoint,
		FilesystemType: mount.FSType,
		MountOptions:   strings.Split(mount.MountOpts, ","),
		DevicePath:     mount.Device,
		DeviceName:     filepath.Base(mount.Device),
	}

	if stat, err := this.getStat(mount.MountPoint); err == nil {
		fs.TotalBytes = stat.TotalBytes
		fs.FreeBytes = stat.FreeBytes
		fs.AvailableBytes = stat.AvailableBytes
	}

	deviceName := this.getBaseDeviceName(mount.Device)
	this.getDeviceInfo(fs, deviceName)

	return fs, nil
}

func (this *linuxDetector) getBaseDeviceName(devicePath string) string {
	name := filepath.Base(devicePath)

	if strings.HasPrefix(name, "nvme") {
		parts := strings.SplitN(name, "n", 2)
		if len(parts) > 0 {
			return parts[0]
		}
	}

	if strings.HasPrefix(name, "sd") || strings.HasPrefix(name, "hd") || strings.HasPrefix(name, "vd") {
		return strings.TrimRightFunc(name, func(r rune) bool {
			return r >= '0' && r <= '9'
		})
	}

	if strings.HasPrefix(name, "dm-") {
		return name
	}

	return name
}

func (this *linuxDetector) getDeviceInfo(fs *types.Filesystem, deviceName string) {
	blockPath := "/sys/block/" + deviceName
	if _, err := os.Stat(blockPath); os.IsNotExist(err) {
		return
	}

	if rotational, err := this.readSysFile(blockPath + "/queue/rotational"); err == nil {
		fs.Rotational = rotational == "1"
		if fs.Rotational {
			fs.DiskType = "hdd"
		} else {
			if strings.HasPrefix(deviceName, "nvme") {
				fs.DiskType = "nvme"
			} else {
				fs.DiskType = "ssd"
			}
		}
	}

	if scheduler, err := this.readSysFile(blockPath + "/queue/scheduler"); err == nil {
		fs.Scheduler = this.parseScheduler(scheduler)
	}

	if model, err := this.readSysFile(blockPath + "/device/model"); err == nil {
		fs.DeviceModel = strings.TrimSpace(model)
	}

	if vendor, err := this.readSysFile(blockPath + "/device/vendor"); err == nil {
		fs.DeviceVendor = strings.TrimSpace(vendor)
	}

	if serial, err := this.readSysFile(blockPath + "/device/serial"); err == nil {
		fs.DeviceSerial = strings.TrimSpace(serial)
	}

	if phyBS, err := this.readSysFile(blockPath + "/queue/physical_block_size"); err == nil {
		if size, err := strconv.ParseUint(phyBS, 10, 64); err == nil {
			fs.PhysicalBlockSize = size
		}
	}

	if logBS, err := this.readSysFile(blockPath + "/queue/logical_block_size"); err == nil {
		if size, err := strconv.ParseUint(logBS, 10, 64); err == nil {
			fs.LogicalBlockSize = size
		}
	}
}

func (this *linuxDetector) readSysFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (this *linuxDetector) parseScheduler(scheduler string) string {
	start := strings.Index(scheduler, "[")
	end := strings.Index(scheduler, "]")
	if start >= 0 && end > start {
		return scheduler[start+1 : end]
	}
	return strings.TrimSpace(scheduler)
}

type statInfo struct {
	TotalBytes     uint64
	FreeBytes      uint64
	AvailableBytes uint64
}

func (this *linuxDetector) getStat(path string) (*statInfo, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, err
	}

	return &statInfo{
		TotalBytes:     stat.Blocks * uint64(stat.Bsize),
		FreeBytes:      stat.Bfree * uint64(stat.Bsize),
		AvailableBytes: stat.Bavail * uint64(stat.Bsize),
	}, nil
}
