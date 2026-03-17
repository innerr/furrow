//go:build darwin

package metadata

import (
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

type darwinCollector struct{}

func newPlatformCollector() Collector {
	return &darwinCollector{}
}

func (this *darwinCollector) CollectHostInfo() (*types.HostInfo, error) {
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

func (this *darwinCollector) CollectEnvironment() (*types.TestEnvironment, error) {
	env := &types.TestEnvironment{}
	return env, nil
}

func (this *darwinCollector) getFQDN() string {
	output, err := exec.Command("hostname", "-f").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func (this *darwinCollector) getIPAddresses() []string {
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
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	return ips
}

func (this *darwinCollector) getMACAddress() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 && len(iface.HardwareAddr) > 0 {
			return iface.HardwareAddr.String()
		}
	}
	return ""
}

func (this *darwinCollector) getKernel() string {
	output, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func (this *darwinCollector) getOSInfo() (string, string) {
	output, err := exec.Command("sw_vers").Output()
	if err != nil {
		return "macOS", ""
	}

	var name, version string
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "ProductName:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "ProductName:"))
		} else if strings.HasPrefix(line, "ProductVersion:") {
			version = strings.TrimSpace(strings.TrimPrefix(line, "ProductVersion:"))
		}
	}
	return name, version
}

func (this *darwinCollector) getCPUInfo() (string, int) {
	var model string
	var cores int

	output, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output()
	if err == nil {
		model = strings.TrimSpace(string(output))
	}

	output, err = exec.Command("sysctl", "-n", "hw.physicalcpu").Output()
	if err == nil {
		cores, _ = strconv.Atoi(strings.TrimSpace(string(output)))
	}

	return model, cores
}

func (this *darwinCollector) getMemoryInfo() (uint64, uint64) {
	var total, free uint64

	output, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err == nil {
		total, _ = strconv.ParseUint(strings.TrimSpace(string(output)), 10, 64)
	}

	var vmStat syscall.Statfs_t
	if err := syscall.Statfs("/", &vmStat); err == nil {
		free = vmStat.Bfree * uint64(vmStat.Bsize)
	}

	return total, free
}

func (this *darwinCollector) getSwapInfo() (uint64, uint64) {
	output, err := exec.Command("sysctl", "-n", "vm.swapusage").Output()
	if err != nil {
		return 0, 0
	}

	var total, free uint64
	parts := strings.Fields(string(output))
	for i, p := range parts {
		if p == "total" && i+2 < len(parts) {
			total = this.parseSize(parts[i+2])
		} else if p == "free" && i+2 < len(parts) {
			free = this.parseSize(parts[i+2])
		}
	}

	return total, free
}

func (this *darwinCollector) parseSize(s string) uint64 {
	s = strings.TrimSuffix(s, "M")
	s = strings.TrimSuffix(s, "G")
	val, _ := strconv.ParseFloat(s, 64)
	if strings.HasSuffix(s, "G") {
		return uint64(val * 1024 * 1024 * 1024)
	}
	return uint64(val * 1024 * 1024)
}

func (this *darwinCollector) GetTimezone() string {
	return time.Now().Location().String()
}
