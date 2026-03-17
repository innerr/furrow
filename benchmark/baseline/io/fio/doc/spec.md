# FIO Benchmark Tool - Specification

## 1. Disk Classification

```go
type DiskClass string

const (
    DiskClassSlowHDD  DiskClass = "SlowHDD"
    DiskClassFastHDD  DiskClass = "FastHDD"
    DiskClassSATASSD  DiskClass = "SATA_SSD"
    DiskClassNVMeSSD  DiskClass = "NVMe_SSD"
)
```

---

## 2. Core Data Structures

### Filesystem

```go
type Filesystem struct {
    Path, MountPoint, FilesystemType  string
    MountOptions                      []string
    DevicePath, DeviceName            string
    DeviceModel, DeviceVendor         string
    DeviceSerial, DeviceFirmware      string
    DiskType                          string    // hdd, ssd, nvme
    DiskClass                         DiskClass
    TotalBytes, FreeBytes, AvailableBytes  uint64
    PhysicalBlockSize, LogicalBlockSize     uint64
    Rotational                        bool
    Scheduler                         string
    // NVMe specific
    NVMeNamespace                     uint
    NVMePCIPath                       string
}
```

### HostInfo

```go
type HostInfo struct {
    Hostname, FQDN            string
    IPAddresses               []string
    MACAddress                string
    Platform, Arch            string  // linux/amd64
    Kernel, OS, OSVersion     string
    CPUModel                  string
    CPUCores, CPUCoresPhysical int
    MemoryTotalBytes, MemoryFreeBytes uint64
    SwapTotalBytes, SwapFreeBytes     uint64
}
```

### SampleResult

```go
type SampleResult struct {
    SeqReadBWMBps, SeqWriteBWMBps  uint64
    RandReadIOPS, RandWriteIOPS    uint64
    FsyncIOPS                      uint64
    DiskClass                      DiskClass
}
```

### TestStrategy

```go
type TestStrategy struct {
    TestsPlanned, TestsSkipped  []string
    SkipReasons                 map[string]string
    IODepth, NumJobs            int
    Percentiles                 []string
}
```

### TestConfig

```go
type TestConfig struct {
    Name, RW, BS, IOEngine  string
    Direct, Fsync, Sync     bool
    IODepth, NumJobs, Runtime int
    RWMixRead               int  // 0-100
    LatPercentiles          bool
}
```

### TestMetrics

```go
type TestMetrics struct {
    BandwidthMBps, IOPS     float64
    LatencyMin, LatencyMax  float64
    LatencyMean, LatencyStddev float64
    LatencyPercentiles      map[string]float64  // p50, p95, p99...
    CPUUser, CPUSystem      float64
}
```

### Report

```go
type Report struct {
    Metadata        ReportMetadata
    Phase1Sampling  SampleResult
    Phase2Strategy  TestStrategy
    Phase3Results   map[string]TestResult
    Summary         ReportSummary
    RawFioLogs      map[string]string  // test_name -> raw output
}

type ReportSummary struct {
    Scores             map[string]int  // seq_read, seq_write, rand_read...
    OverallScore       int
    Bottleneck         string
    Recommendations    []string
}
```

---

## 3. Interfaces

```go
type Detector interface {
    List() ([]Filesystem, error)
    Get(path string) (*Filesystem, error)
}

type MetadataCollector interface {
    CollectHostInfo() (*HostInfo, error)
    CollectEnvironment() (*TestEnvironment, error)
}

type FioRunner interface {
    CheckInstalled() (*FioInfo, error)
    Run(ctx context.Context, cfg TestConfig, testFile string) (*TestResult, error)
    RunSampling(ctx context.Context, testFile string, size uint64) (*SampleResult, error)
}

type Analyzer interface {
    Classify(sample *SampleResult) DiskClass
    GenerateStrategy(sample *SampleResult, class DiskClass) *TestStrategy
}

type Reporter interface {
    GenerateJSON(report *Report) ([]byte, error)
    GenerateMarkdown(report *Report) ([]byte, error)
}
```

---

## 4. Test Catalog

| Name | RW | BS | IOEngine | Direct | Fsync | IODepth |
|------|----|----|----------|--------|-------|---------|
| **Sequential** |
| seq_read_async_direct | read | 1M | libaio | true | false | 32 |
| seq_write_async_direct | write | 1M | libaio | true | false | 32 |
| seq_read_sync_direct | read | 1M | sync | true | false | 1 |
| seq_write_sync_direct | write | 1M | sync | true | false | 1 |
| seq_read_buffered | read | 1M | sync | false | false | 1 |
| seq_write_buffered | write | 1M | sync | false | false | 1 |
| **Random 4K** |
| rand_read_4k_async_direct | randread | 4k | libaio | true | false | 32 |
| rand_write_4k_async_direct | randwrite | 4k | libaio | true | false | 32 |
| rand_read_4k_sync_direct | randread | 4k | sync | true | false | 1 |
| rand_write_4k_sync_direct | randwrite | 4k | sync | true | false | 1 |
| rand_read_4k_buffered | randread | 4k | sync | false | false | 1 |
| rand_write_4k_buffered | randwrite | 4k | sync | false | false | 1 |
| **Mixed** |
| mixed_70_30 | randrw | 4k | libaio | true | false | 32 |
| mixed_50_50 | randrw | 4k | libaio | true | false | 32 |
| **Multi-BlockSize** |
| multibs_read | randread | 4k,16k,64k,256k,1M | libaio | true | false | 32 |
| multibs_write | randwrite | 4k,16k,64k,256k,1M | libaio | true | false | 32 |
| **fsync** |
| fsync_limit | write | 4k | sync | false | true | 1 |
| **Latency** |
| latency_read | randread | 4k | libaio | true | false | 32 |
| latency_write | randwrite | 4k | libaio | true | false | 32 |

---

## 5. Test Selection Rules

### NVMe SSD
- Run: Async sequential, Async random, Sync random read, Mixed both, Multi-BS, fsync, Latency both
- Skip: Sync sequential, Buffered modes
- IODepth: 32, NumJobs: 4, Percentiles: P50-P99.99

### SATA SSD
- Run: All modes except mixed_50_50
- Skip: mixed_50_50
- IODepth: 32, NumJobs: 4, Percentiles: P50-P99.9

### HDD (Fast/Slow)
- Run: Sequential all modes, Async random, Latency read
- Skip: Sync random, Buffered random, Mixed, Multi-BS, fsync, Latency write
- IODepth: 8, NumJobs: 2, Percentiles: P50-P99

---

## 6. Scoring

### Score Calculation (0-100)

| Performance | Score |
|-------------|-------|
| ≥ Excellent | 100 |
| Good - Excellent | 80-100 |
| Fair - Good | 60-80 |
| Poor - Fair | 40-60 |
| < Poor | 0-40 |

### Reference Values (MB/s, IOPS)

| Metric | NVMe Excellent | SATA Excellent | HDD Excellent |
|--------|----------------|----------------|---------------|
| Seq Read | 3500 | 550 | 200 |
| Seq Write | 3000 | 520 | 200 |
| Rand Read | 800K | 100K | 300 |
| Rand Write | 700K | 90K | 300 |
| fsync | 50K | 30K | 1K |

### Overall Score Weights

| Category | Weight |
|----------|--------|
| Seq Read | 15% |
| Seq Write | 15% |
| Rand Read | 25% |
| Rand Write | 25% |
| Mixed | 10% |
| fsync | 10% |

---

## 7. Errors

```go
var (
    ErrFioNotFound        = errors.New("fio not found")
    ErrNotSupported       = errors.New("platform not supported")
    ErrSampleFailed       = errors.New("sampling test failed")
    ErrInsufficientSpace  = errors.New("insufficient disk space")
    ErrTestFileCreate     = errors.New("failed to create test file")
    ErrInvalidPath        = errors.New("invalid path")
    ErrFioError           = errors.New("fio execution failed")
)
```

---

## 8. Report ID

```
{YYYYMMDD}_{HHMMSS}_{hostname}_{device_basename}
```

Example: `20240318_100000_server01_nvme0n1p1`
