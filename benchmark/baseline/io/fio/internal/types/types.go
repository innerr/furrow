package types

import "time"

type DiskClass string

const (
	DiskClassSlowHDD DiskClass = "SlowHDD"
	DiskClassFastHDD DiskClass = "FastHDD"
	DiskClassSATASSD DiskClass = "SATA_SSD"
	DiskClassNVMeSSD DiskClass = "NVMe_SSD"
)

type Filesystem struct {
	Path              string
	MountPoint        string
	FilesystemType    string
	MountOptions      []string
	DevicePath        string
	DeviceName        string
	DeviceModel       string
	DeviceVendor      string
	DeviceSerial      string
	DeviceFirmware    string
	DiskType          string
	DiskClass         DiskClass
	TotalBytes        uint64
	FreeBytes         uint64
	AvailableBytes    uint64
	PhysicalBlockSize uint64
	LogicalBlockSize  uint64
	Rotational        bool
	Scheduler         string
	NVMeNamespace     uint
	NVMePCIPath       string
}

type HostInfo struct {
	Hostname         string
	FQDN             string
	IPAddresses      []string
	MACAddress       string
	Platform         string
	Arch             string
	Kernel           string
	OS               string
	OSVersion        string
	CPUModel         string
	CPUCores         int
	CPUCoresPhysical int
	MemoryTotalBytes uint64
	MemoryFreeBytes  uint64
	SwapTotalBytes   uint64
	SwapFreeBytes    uint64
}

type FioInfo struct {
	Version        string
	VersionNumeric []int
	Path           string
	CompileOptions []string
	Capabilities   []string
}

type TestEnvironment struct {
	CPUGovernor          string
	TransparentHugepages string
	Swappiness           uint64
	DirtyRatio           uint64
	DropCaches           bool
}

type SampleResult struct {
	SeqReadBWMBps  uint64
	SeqWriteBWMBps uint64
	RandReadIOPS   uint64
	RandWriteIOPS  uint64
	FsyncIOPS      uint64
	DiskClass      DiskClass
	Duration       time.Duration
}

type TestStrategy struct {
	TestsPlanned   []string
	TestsSkipped   []string
	SkipReasons    map[string]string
	IODepth        int
	NumJobs        int
	Percentiles    []string
	RuntimePerTest int
}

type TestConfig struct {
	Name           string
	RW             string
	BS             string
	IOEngine       string
	Direct         bool
	Fsync          bool
	IODepth        int
	NumJobs        int
	Runtime        int
	RWMixRead      int
	LatPercentiles bool
}

type TestMetrics struct {
	BandwidthMBps      float64
	BandwidthBytes     uint64
	IOPS               float64
	LatencyMin         float64
	LatencyMax         float64
	LatencyMean        float64
	LatencyStddev      float64
	LatencyPercentiles map[string]float64
	CPUUser            float64
	CPUSystem          float64
}

type TestResult struct {
	Name     string
	Config   TestConfig
	Metrics  TestMetrics
	RawLog   string
	Duration time.Duration
	Error    error
}

type TestConfigResult struct {
	Config  TestConfig
	Metrics TestMetrics
}

type ReportMetadata struct {
	ReportID      string
	GeneratedAt   time.Time
	ToolVersion   string
	ToolGitCommit string
	Host          HostInfo
	Target        Filesystem
	Fio           FioInfo
	Environment   TestEnvironment
	Test          TestInfo
}

type TestInfo struct {
	Mode              string
	Phase1DurationSec int
	Phase3DurationSec int
	TotalDurationSec  int
	TestFileSizeBytes uint64
	TestFilePath      string
	IODepth           int
	NumJobs           int
	IOEngine          string
	Direct            bool
	TestsRun          int
	TestsSkipped      int
	TestsTotal        int
	RandomSeed        int64
}

type ReportSummary struct {
	Scores           map[string]int
	OverallScore     int
	Bottleneck       string
	BottleneckDetail string
	Recommendations  []string
}

type Report struct {
	Metadata       ReportMetadata
	Phase1Sampling SampleResult
	Phase2Strategy TestStrategy
	Phase3Results  map[string]TestConfigResult
	Summary        ReportSummary
	RawFioLogs     map[string]string
}

type SizeTier struct {
	MinGB         uint64
	MaxGB         uint64
	DefaultSizeGB uint64
	MinFreeRatio  float64
}

type ReferenceValues struct {
	Excellent float64
	Good      float64
	Fair      float64
	Poor      float64
}

type ScoreReferences struct {
	SeqReadBW     ReferenceValues
	SeqWriteBW    ReferenceValues
	RandReadIOPS  ReferenceValues
	RandWriteIOPS ReferenceValues
	FsyncIOPS     ReferenceValues
	MixedIOPS     ReferenceValues
}

type TestSelection struct {
	Run         []string
	Skip        []string
	IODepth     int
	NumJobs     int
	Percentiles []string
}
