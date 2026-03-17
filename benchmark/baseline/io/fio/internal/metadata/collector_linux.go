//go:build linux

package metadata

import (
	"bufio"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

type linuxCollector struct{}

func newPlatformCollector() Collector {
	return &linuxCollector{}
}

func (this *linuxCollector) CollectHostInfo() (*types.HostInfo, error) {
	info := &types.HostInfo{
		Platform: runtime.GOOS,
		Arch:     runtime.GOARCH,
		CPUCores: runtime.NumCPU(),
	}

	info.Hostname, _ = os.Hostname()
	info.FQDN = this.getFQDN()
	info.IPAddresses = this.getIPAddresses()
	info.MACAddress = this.getMACAddress()
	info.Kernel = this.getKernel()
	info.OS, info.OSVersion = this.getOSInfo()
	info.CPUModel, info.CPUCoresPhysical = this.getCPUInfo()
	info.MemoryTotalBytes, info.MemoryFreeBytes = this.getMemoryInfo()
	info.SwapTotalBytes, info.SwapFreeBytes = this.getSwapInfo()

	return info, nil
}

func (this *linuxCollector) CollectEnvironment() (*types.TestEnvironment, error) {
	env := &types.TestEnvironment{
		CPUGovernor:          this.getCPUGovernor(),
		TransparentHugepages: this.getTransparentHugepages(),
		Swappiness:           this.getSwappiness(),
		DirtyRatio:           this.getDirtyRatio(),
	}

	return env, nil
}

func (this *linuxCollector) getFQDN() string {
	output, err := exec.Command("hostname", "-f").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func (this *linuxCollector) getIPAddresses() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var ips []string
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					ips = append(ips, ipnet.IP.String())
				}
			}
		}
	}
	return ips
}

func (this *linuxCollector) getMACAddress() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.HardwareAddr != nil && len(iface.HardwareAddr) > 0 {
			return iface.HardwareAddr.String()
		}
	}
	return ""
}

func (this *linuxCollector) getKernel() string {
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (this *linuxCollector) getOSInfo() (string, string) {
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return "", ""
	}
	defer file.Close()

	var name, version string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			name = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
		} else if strings.HasPrefix(line, "VERSION_ID=") {
			version = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"")
		}
	}
	return name, version
}

func (this *linuxCollector) getCPUInfo() (string, int) {
	file, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return "", 0
	}
	defer file.Close()

	var model string
	physicalIDs := make(map[string]bool)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				model = strings.TrimSpace(parts[1])
			}
		}
		if strings.HasPrefix(line, "physical id") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				physicalIDs[strings.TrimSpace(parts[1])] = true
			}
		}
	}

	physicalCores := len(physicalIDs)
	if physicalCores == 0 {
		physicalCores = runtime.NumCPU()
	}

	return model, physicalCores
}

func (this *linuxCollector) getMemoryInfo() (uint64, uint64) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer file.Close()

	var total, free uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				val, _ := strconv.ParseUint(fields[1], 10, 64)
				total = val * 1024
			}
		} else if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				val, _ := strconv.ParseUint(fields[1], 10, 64)
				free = val * 1024
			}
		} else if strings.HasPrefix(line, "MemFree:") && free == 0 {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				val, _ := strconv.ParseUint(fields[1], 10, 64)
				free = val * 1024
			}
		}
	}
	return total, free
}

func (this *linuxCollector) getSwapInfo() (uint64, uint64) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer file.Close()

	var total, free uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "SwapTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				val, _ := strconv.ParseUint(fields[1], 10, 64)
				total = val * 1024
			}
		} else if strings.HasPrefix(line, "SwapFree:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				val, _ := strconv.ParseUint(fields[1], 10, 64)
				free = val * 1024
			}
		}
	}
	return total, free
}

func (this *linuxCollector) getCPUGovernor() string {
	data, err := os.ReadFile("/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (this *linuxCollector) getTransparentHugepages() string {
	data, err := os.ReadFile("/sys/kernel/mm/transparent_hugepage/enabled")
	if err != nil {
		return ""
	}
	s := string(data)
	if idx := strings.Index(s, "["); idx >= 0 {
		if end := strings.Index(s[idx:], "]"); end > 0 {
			return s[idx+1 : idx+end]
		}
	}
	return strings.TrimSpace(s)
}

func (this *linuxCollector) getSwappiness() uint64 {
	data, err := os.ReadFile("/proc/sys/vm/swappiness")
	if err != nil {
		return 0
	}
	val, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	return val
}

func (this *linuxCollector) getDirtyRatio() uint64 {
	data, err := os.ReadFile("/proc/sys/vm/dirty_ratio")
	if err != nil {
		return 0
	}
	val, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	return val
}

func (this *linuxCollector) getUptime() uint64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) > 0 {
		val, _ := strconv.ParseFloat(fields[0], 64)
		return uint64(val)
	}
	return 0
}

func (this *linuxCollector) GetTimezone() string {
	return time.Now().Location().String()
}
