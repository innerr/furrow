# Issues Address Plan

This document provides detailed fix plans for each confirmed issue in `issues.md`.

## P0 - Critical

### 1. Overall score is multiplied by 100 again

**Root Cause Analysis**:

In `internal/report/json.go:70`, `CalculateOverallScore()` computes a weighted average where each `score` is already in the range 0-100 (from `calculateScore()`). The formula:

```go
total += float64(score) * w
```

produces a value in 0-100 range. Multiplying by 100 at the end inflates the result to 0-10000 range.

**Fix Plan**:

```go
// internal/report/json.go:70
// Before:
return int(total / weightSum * 100)

// After:
return int(total / weightSum)
```

**Verification**: If all scores are 80 with equal weights, the result should be 80, not 8000.

---

### 2. fio bandwidth parsing uses the wrong JSON field

**Root Cause Analysis**:

In `internal/fio/parser.go:143`, the code reads `BandwidthAgg` (JSON field `bw_agg`):

```go
metrics.BandwidthMBps = float64(job.Read.BandwidthAgg) / 1024
```

According to fio documentation:
- `bw` = actual bandwidth in KB/s
- `bw_agg` = percentage of aggregates (0-100%), NOT bandwidth

**Fix Plan**:

```go
// internal/fio/parser.go:143-159
// Replace BandwidthAgg with Bandwidth for all three branches

// Before (line 143):
metrics.BandwidthMBps = float64(job.Read.BandwidthAgg) / 1024

// After:
metrics.BandwidthMBps = float64(job.Read.Bandwidth) / 1024  // bw is in KB/s

// Apply same change to:
// - Line 148 (write branch)
// - Line 153 (randrw branch - sum read.Bandwidth + write.Bandwidth)
// - Line 158 (unknown branch)
```

**Verification**: Run a simple fio test, the reported MB/s should match fio's own output.

---

## P1 - High Priority

### 3. Test file size is computed before disk classification exists

**Root Cause Analysis**:

In `cmd/run.go:78`:
```go
testFileSize := fio.CalculateTestFileSize(targetFS.TotalBytes, targetFS.FreeBytes, targetFS.DiskClass)
```

At this point, `targetFS.DiskClass` is empty because:
1. `detector.Get()` populates `DiskType` (e.g., "nvme", "ssd", "hdd") but not `DiskClass`
2. `DiskClass` is only set after Phase 1 sampling via `analyzer.Classify(sampleResult)` at line 93

The HDD size reduction path in `CalculateTestFileSize()` (lines 215-217) never executes.

**Fix Plan**:

Option A (Recommended): Recompute file size after Phase 1

```go
// cmd/run.go - after line 93 (after sampleResult.DiskClass is set)
sampleResult.DiskClass = analyzer.Classify(sampleResult)
// Recompute test file size with correct disk class
testFileSize = fio.CalculateTestFileSize(targetFS.TotalBytes, targetFS.FreeBytes, sampleResult.DiskClass)

// If size changed significantly, recreate the test file
if newFileSize != testFileSize {
    runner.CleanupTestFile(testFile)
    testFile, err = runner.CreateTestFile(targetFS.Path, newFileSize)
    if err != nil {
        return err
    }
    testFileSize = newFileSize
}
```

Option B: Map `DiskType` to `DiskClass` for initial estimate

Add a helper function:
```go
func estimateDiskClass(diskType string) types.DiskClass {
    switch diskType {
    case "nvme":
        return types.DiskClassNVMeSSD
    case "ssd":
        return types.DiskClassSATASSD
    case "hdd":
        return types.DiskClassSlowHDD
    default:
        return types.DiskClassSlowHDD
    }
}
```

**Recommendation**: Option A is preferred as it uses actual measured performance for classification.

---

### 4. Test file sizing violates the 25% free-space cap

**Root Cause Analysis**:

In `internal/fio/jobs.go:219-226`:
```go
maxAllowed := uint64(float64(freeSpace) * 0.25)
if size > maxAllowed {
    size = maxAllowed
}

if size < GB {
    size = GB  // This can exceed maxAllowed!
}
```

On a volume with 3GB free space:
- `maxAllowed = 0.75GB`
- After cap: `size = 0.75GB`
- After minimum: `size = 1GB` (exceeds the 25% cap!)

**Fix Plan**:

```go
// internal/fio/jobs.go:219-229
// Before:
maxAllowed := uint64(float64(freeSpace) * 0.25)
if size > maxAllowed {
    size = maxAllowed
}

if size < GB {
    size = GB
}

// After:
maxAllowed := uint64(float64(freeSpace) * 0.25)

// Apply minimum only if it doesn't violate the cap
minSize := GB
if minSize > maxAllowed {
    // Not enough free space for minimum, use maxAllowed
    // Or return an error if we want to enforce a minimum
    size = maxAllowed
    if size < 64*MB {  // Absolute minimum (64MB)
        return 0  // Caller should handle this as error
    }
} else {
    if size > maxAllowed {
        size = maxAllowed
    }
    if size < minSize {
        size = minSize
    }
}
```

**Alternative**: Add a validation function and return error if free space is insufficient:
```go
func ValidateFreeSpace(freeSpace uint64) error {
    minRequired := GB
    if freeSpace < minRequired {
        return fmt.Errorf("insufficient free space: need at least 1GB, have %s", formatBytes(freeSpace))
    }
    return nil
}
```

---

### 5. Report metadata drops the actual test file info and disk class

**Root Cause Analysis**:

In `cmd/run.go:179-208`:
```go
Test: types.TestInfo{
    Mode:              "adaptive",
    TestFileSizeBytes: 0,           // Hardcoded
    TestFilePath:      "",          // Hardcoded
    ...
},
```

And `target.DiskClass` is never updated from `sampleResult.DiskClass`.

**Fix Plan**:

```go
// cmd/run.go - in buildReport() or before calling it

// Option 1: Update target before passing to buildReport
target.DiskClass = sample.DiskClass

// Option 2: Pass additional parameters to buildReport
func buildReport(target *types.Filesystem, sample *types.SampleResult, strategy *types.TestStrategy,
    results map[string]types.TestConfigResult, rawLogs map[string]string,
    runner *fio.Runner, collector metadata.Collector,
    testFileSize uint64, testFilePath string) *types.Report {
    ...
    Test: types.TestInfo{
        Mode:              "adaptive",
        TestFileSizeBytes: testFileSize,
        TestFilePath:      testFilePath,
        ...
    },
    ...
}

// In runBenchmark(), call with:
reportData := buildReport(targetFS, sampleResult, strategy, results, rawLogs, runner, collector, testFileSize, testFile)
```

**Required changes**:
1. Update `targetFS.DiskClass = sampleResult.DiskClass` after line 93
2. Pass `testFileSize` and `testFile` to `buildReport()`
3. Populate `TestFileSizeBytes` and `TestFilePath` in the report

---

### 6. Deep-test failures still produce a "successful" report

**Root Cause Analysis**:

In `cmd/run.go:139-158`, test failures are logged and skipped:
```go
result, err := runner.Run(ctx, cfg, opts)
if err != nil {
    prompt.DisplayError(fmt.Sprintf("%s: %v", testName, err))
    continue  // Just continue, results map remains empty for this test
}
```

If all tests fail, `results` map is empty but the tool still:
1. Generates reports (line 154)
2. Prints "All tests completed successfully" via `DisplayCompletion()`

**Fix Plan**:

```go
// cmd/run.go - after the test loop (around line 153)

// Track failures
var failedTests []string
for _, testName := range strategy.TestsPlanned {
    if _, ok := results[testName]; !ok {
        failedTests = append(failedTests, testName)
    }
}

// Check if we have any successful results
if len(results) == 0 {
    return fmt.Errorf("all deep tests failed, cannot generate report")
}

// Report partial failure
if len(failedTests) > 0 {
    fmt.Printf("\n  Warning: %d test(s) failed: %v\n", len(failedTests), failedTests)
}

// Modify prompt.DisplayCompletion to accept a status
func DisplayCompletion(reportPath string, successCount, totalCount int) {
    fmt.Println()
    if successCount == totalCount {
        fmt.Println("  All tests completed successfully")
    } else {
        fmt.Printf("  %d/%d tests completed successfully\n", successCount, totalCount)
    }
    ...
}
```

---

### 7. Latency section in Markdown never gets usable P99 data

**Root Cause Analysis**:

Three issues:

1. **Wrong test names**: `writeLatency()` reads from `rand_read_4k_async_direct` / `rand_write_4k_async_direct`, but these tests don't enable `LatPercentiles`. The dedicated latency tests are `latency_read` / `latency_write`.

2. **Percentile key mismatch**: In `parser.go:186`:
   ```go
   key := "p" + strings.ReplaceAll(k, ".", "_")
   ```
   This converts fio's `"99.000000"` to `"p99_000000"`, but Markdown looks up `"p99"`.

**Fix Plan**:

1. **Fix the test name lookup in markdown.go**:

```go
// internal/report/markdown.go:120-129
// Before:
if r, ok := report.Phase3Results["rand_read_4k_async_direct"]; ok {
    if p99, ok := r.Metrics.LatencyPercentiles["p99"]; ok {
        ...
    }
}

// After:
if r, ok := report.Phase3Results["latency_read"]; ok {
    if p99, ok := r.Metrics.LatencyPercentiles["p99"]; ok {
        fmt.Fprintf(sb, "| Random Read 4K | %s |\n", FormatLatency(p99))
    }
}
if r, ok := report.Phase3Results["latency_write"]; ok {
    if p99, ok := r.Metrics.LatencyPercentiles["p99"]; ok {
        fmt.Fprintf(sb, "| Random Write 4K | %s |\n", FormatLatency(p99))
    }
}
```

2. **Fix the percentile key normalization in parser.go**:

```go
// internal/fio/parser.go:185-188
// Before:
for k, v := range rw.LatencyUS.Pct {
    key := "p" + strings.ReplaceAll(k, ".", "_")
    metrics.LatencyPercentiles[key] = v
}

// After:
for k, v := range rw.LatencyUS.Pct {
    // Normalize percentile keys: "99.000000" -> "p99", "99.900000" -> "p99.9"
    key := normalizePercentileKey(k)
    metrics.LatencyPercentiles[key] = v
}

// Add helper function:
func normalizePercentileKey(k string) string {
    // fio returns keys like "99.000000", "99.900000", "99.990000"
    // We want to convert to "p99", "p99.9", "p99.99"
    var val float64
    if _, err := fmt.Sscanf(k, "%f", &val); err != nil {
        return "p" + k
    }
    
    // Format based on precision
    if val == float64(int(val)) {
        return fmt.Sprintf("p%d", int(val))
    }
    return fmt.Sprintf("p%.1f", val)  // or keep more precision as needed
}
```

Apply same change to the `LatencyNS` and `LatencyMS` loops.

---

### 8. Redundancy detection skips the wrong tests

**Root Cause Analysis**:

In `internal/analyzer/strategy.go:33-48`:
```go
// When read/write bandwidth is within 10%:
delete(testsToRun, "seq_write_sync_direct")
delete(testsToRun, "seq_write_buffered")

// When read/write IOPS is within 15%:
delete(testsToRun, "rand_write_4k_sync_direct")
delete(testsToRun, "rand_write_4k_buffered")
delete(testsToRun, "latency_write")
```

The main async write tests (`seq_write_async_direct`, `rand_write_4k_async_direct`) are kept, which defeats the purpose of skipping "separate write bandwidth/IOPS tests" when read/write performance is similar.

**Fix Plan**:

```go
// internal/analyzer/strategy.go:31-50
// When performance is similar, skip the MAIN async write tests instead

if sample != nil {
    // Sequential bandwidth similarity
    if sample.SeqReadBWMBps > 0 && sample.SeqWriteBWMBps > 0 {
        bwDiff := abs(int(sample.SeqReadBWMBps) - int(sample.SeqWriteBWMBps))
        if bwDiff < int(float64(sample.SeqReadBWMBps)*0.1) {
            // Skip main async write test, not sync/buffered
            delete(testsToRun, "seq_write_async_direct")
            strategy.SkipReasons["seq_write_async_direct"] = "read/write bandwidth within 10%"
        }
    }

    // Random IOPS similarity
    if sample.RandReadIOPS > 0 && sample.RandWriteIOPS > 0 {
        iopsDiff := abs(int(sample.RandReadIOPS) - int(sample.RandWriteIOPS))
        if iopsDiff < int(float64(sample.RandReadIOPS)*0.15) {
            // Skip main async write test and latency_write
            delete(testsToRun, "rand_write_4k_async_direct")
            delete(testsToRun, "latency_write")
            strategy.SkipReasons["rand_write_4k_async_direct"] = "read/write IOPS within 15%"
            strategy.SkipReasons["latency_write"] = "read/write IOPS within 15%"
        }
    }
}
```

**Rationale**: When read and write performance are similar, the async write test provides redundant information. Keeping sync/buffered tests still has value for understanding different I/O patterns.

---

### 9. Interactive input ignores `ReadString` errors

**Root Cause Analysis**:

In `internal/prompt/prompt.go:34` and `73`:
```go
input, _ := reader.ReadString('\n')
```

The error is discarded. When stdin is closed (EOF) or has errors, `input` becomes an empty string, which:
- In `SelectFilesystem()`: falls through to invalid selection, traps user in loop
- In `ConfirmStrategy()`: defaults to "proceed" which may be unintended

**Fix Plan**:

```go
// internal/prompt/prompt.go:34
// Before:
input, _ := reader.ReadString('\n')

// After:
input, err := reader.ReadString('\n')
if err != nil {
    return nil, fmt.Errorf("failed to read input: %w", err)
}

// Same fix for line 73:
input, err := reader.ReadString('\n')
if err != nil {
    return "", fmt.Errorf("failed to read input: %w", err)
}
```

**Caller handling**: In `cmd/run.go`, handle the returned error appropriately:
```go
targetFS, err = prompt.SelectFilesystem(filesystems)
if err != nil {
    // If it's an EOF/input error, exit gracefully
    if errors.Is(err, io.EOF) {
        fmt.Println("\nInput closed, exiting.")
        return nil
    }
    return err
}
```

---

### 10. Linux NVMe base-device extraction is broken

**Root Cause Analysis**:

In `internal/fs/detector_linux.go:182-187`:
```go
if strings.HasPrefix(name, "nvme") {
    parts := strings.SplitN(name, "n", 2)  // "nvme0n1p1" -> ["nvme0", "1p1"]
    if len(parts) > 0 {
        return parts[0]  // Returns "nvme0" - WRONG!
    }
}
```

NVMe device naming convention: `nvme<controller>n<namespace>p<partition>`
- Example: `nvme0n1p1` → base device is `nvme0n1` (controller 0, namespace 1)
- The code incorrectly splits on "n", returning only "nvme0"

**Fix Plan**:

```go
// internal/fs/detector_linux.go:182-187
// Before:
if strings.HasPrefix(name, "nvme") {
    parts := strings.SplitN(name, "n", 2)
    if len(parts) > 0 {
        return parts[0]
    }
}

// After:
if strings.HasPrefix(name, "nvme") {
    // NVMe format: nvme<controller>n<namespace>p<partition>
    // We need to strip only the partition suffix (p<number>)
    // Examples:
    //   nvme0n1p1 -> nvme0n1
    //   nvme0n1   -> nvme0n1
    //   nvme1n2p3 -> nvme1n2
    
    // Find and remove partition suffix
    idx := strings.Index(name, "p")
    if idx > 0 {
        // Check if what follows "p" is a number (partition number)
        partitionPart := name[idx+1:]
        if len(partitionPart) > 0 && partitionPart[0] >= '0' && partitionPart[0] <= '9' {
            return name[:idx]
        }
    }
    return name
}
```

**Alternative using regex**:
```go
import "regexp"

var nvmePartitionRegex = regexp.MustCompile(`^(nvme\d+n\d+)p\d+$`)

func (this *linuxDetector) getBaseDeviceName(devicePath string) string {
    name := filepath.Base(devicePath)
    
    if strings.HasPrefix(name, "nvme") {
        if matches := nvmePartitionRegex.FindStringSubmatch(name); len(matches) > 1 {
            return matches[1]
        }
        return name
    }
    // ... rest of function
}
```

---

### 11. macOS host metadata reports wrong free memory and swap size

**Root Cause Analysis**:

Two bugs in `internal/metadata/collector_darwin.go`:

1. **Line 145-148**: Uses `syscall.Statfs("/")` which returns filesystem free space, NOT free RAM:
   ```go
   var vmStat syscall.Statfs_t
   if err := syscall.Statfs("/", &vmStat); err == nil {
       free = vmStat.Bfree * uint64(vmStat.Bsize)  // This is disk space!
   }
   ```

2. **Line 172-179**: `parseSize()` trims the unit suffix BEFORE checking it:
   ```go
   func (this *darwinCollector) parseSize(s string) uint64 {
       s = strings.TrimSuffix(s, "M")  // "4G" becomes "4G" (no M suffix)
       s = strings.TrimSuffix(s, "G")  // "4G" becomes "4"
       val, _ := strconv.ParseFloat(s, 64)
       if strings.HasSuffix(s, "G") {  // "4" doesn't have "G" suffix!
           return uint64(val * 1024 * 1024 * 1024)
       }
       return uint64(val * 1024 * 1024)  // Always returns MB
   }
   ```

**Fix Plan**:

1. **Fix `getMemoryInfo()`**:
```go
// internal/metadata/collector_darwin.go:137-150
func (this *darwinCollector) getMemoryInfo() (uint64, uint64) {
    var total, free uint64

    // Total memory
    output, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
    if err == nil {
        total, _ = strconv.ParseUint(strings.TrimSpace(string(output)), 10, 64)
    }

    // Free memory: use vm_stat
    output, err = exec.Command("vm_stat").Output()
    if err == nil {
        // vm_stat output: "Pages free:       123456."
        lines := strings.Split(string(output), "\n")
        for _, line := range lines {
            if strings.HasPrefix(line, "Pages free:") {
                fields := strings.Fields(line)
                if len(fields) >= 3 {
                    // Remove trailing period if present
                    countStr := strings.TrimSuffix(fields[2], ".")
                    pageCount, _ := strconv.ParseUint(countStr, 10, 64)
                    // macOS page size is typically 4096 bytes
                    free = pageCount * 4096
                }
                break
            }
        }
    }

    return total, free
}
```

2. **Fix `parseSize()`**:
```go
// internal/metadata/collector_darwin.go:172-180
func (this *darwinCollector) parseSize(s string) uint64 {
    // Check unit BEFORE trimming
    isGB := strings.HasSuffix(s, "G")
    s = strings.TrimSuffix(strings.TrimSuffix(s, "M"), "G")
    
    val, _ := strconv.ParseFloat(s, 64)
    if isGB {
        return uint64(val * 1024 * 1024 * 1024)
    }
    return uint64(val * 1024 * 1024)
}
```

---

## P2 - Medium Priority

### 12. `VersionNumeric` is never populated

**Root Cause Analysis**:

In `internal/fio/runner.go:41-46`:
```go
func (this *Runner) GetFioInfo() *types.FioInfo {
    return &types.FioInfo{
        Version:      this.version,
        Path:         this.fioPath,
        Capabilities: this.getCapabilities(),
        // VersionNumeric is never set
    }
}
```

`ParseFioVersion()` in `parser.go:215-228` returns both the string and numeric parts, but only the string is used.

**Fix Plan**:

```go
// internal/fio/runner.go:41-47
func (this *Runner) GetFioInfo() *types.FioInfo {
    versionStr, versionNum := ParseFioVersion(this.version)
    return &types.FioInfo{
        Version:        versionStr,
        VersionNumeric: versionNum,
        Path:           this.fioPath,
        Capabilities:   this.getCapabilities(),
    }
}
```

**Note**: Ensure `types.FioInfo` has the `VersionNumeric` field defined:
```go
type FioInfo struct {
    Version        string
    VersionNumeric []int  // [major, minor, patch]
    Path           string
    Capabilities   []string
}
```

---

### 13. Linux physical core count is actually socket count

**Root Cause Analysis**:

In `internal/metadata/collector_linux.go:141-166`:
```go
physicalIDs := make(map[string]bool)
...
if strings.HasPrefix(line, "physical id") {
    parts := strings.SplitN(line, ":", 2)
    if len(parts) == 2 {
        physicalIDs[strings.TrimSpace(parts[1])] = true
    }
}
...
physicalCores := len(physicalIDs)  // This counts sockets, not cores!
```

On a single-socket 8-core CPU: `physicalIDs` has 1 entry, returns 1 "physical core".
On a dual-socket 8-core each (16 cores total): `physicalIDs` has 2 entries, returns 2 "physical cores".

**Fix Plan**:

```go
// internal/metadata/collector_linux.go:134-167
func (this *linuxCollector) getCPUInfo() (string, int) {
    file, err := os.Open("/proc/cpuinfo")
    if err != nil {
        return "", 0
    }
    defer file.Close()

    var model string
    // Track unique (physical_id, core_id) pairs
    coreMap := make(map[string]bool)

    scanner := bufio.NewScanner(file)
    var currentPhysicalID, currentCoreID string

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
                currentPhysicalID = strings.TrimSpace(parts[1])
            }
        }
        
        if strings.HasPrefix(line, "core id") {
            parts := strings.SplitN(line, ":", 2)
            if len(parts) == 2 {
                currentCoreID = strings.TrimSpace(parts[1])
                // Combine physical_id and core_id for unique identification
                key := currentPhysicalID + ":" + currentCoreID
                coreMap[key] = true
            }
        }
    }

    physicalCores := len(coreMap)
    if physicalCores == 0 {
        physicalCores = runtime.NumCPU()
    }

    return model, physicalCores
}
```

---

### 14. Recommendations treat missing scores as zero

**Root Cause Analysis**:

In `internal/report/json.go:128-134`:
```go
if scores["fsync"] < 70 {
    recs = append(recs, "Consider enabling write-back cache if data integrity allows")
}

if scores["rand_write"] < 60 && diskClass != types.DiskClassSlowHDD {
    recs = append(recs, "Random write performance may benefit from larger queue depth")
}
```

When a test is skipped, `scores["fsync"]` returns 0 (Go's zero value for map misses), triggering false warnings.

**Fix Plan**:

```go
// internal/report/json.go:128-134
// Before:
if scores["fsync"] < 70 {
    recs = append(recs, "Consider enabling write-back cache if data integrity allows")
}

if scores["rand_write"] < 60 && diskClass != types.DiskClassSlowHDD {
    recs = append(recs, "Random write performance may benefit from larger queue depth")
}

// After:
if score, ok := scores["fsync"]; ok && score < 70 {
    recs = append(recs, "Consider enabling write-back cache if data integrity allows")
}

if score, ok := scores["rand_write"]; ok && score < 60 && diskClass != types.DiskClassSlowHDD {
    recs = append(recs, "Random write performance may benefit from larger queue depth")
}
```

---

### 15. `fsync_deep` skip reason references a non-existent test

**Root Cause Analysis**:

In `internal/analyzer/strategy.go:52-53`:
```go
if sample.FsyncIOPS > 10000 {
    strategy.SkipReasons["fsync_deep"] = "fsync IOPS already excellent (> 10K)"
}
```

`fsync_deep` does not exist in `TestCatalog`. The actual test name is `fsync_limit`.

**Fix Plan**:

Option A: Fix the test name
```go
// internal/analyzer/strategy.go:52-53
if sample.FsyncIOPS > 10000 {
    strategy.SkipReasons["fsync_limit"] = "fsync IOPS already excellent (> 10K)"
    delete(testsToRun, "fsync_limit")
}
```

Option B: Remove dead code (if this feature is not intended)
```go
// Remove lines 52-54 entirely
```

**Recommendation**: Option A if the intent is to skip fsync testing when performance is already excellent.

---

### 16. Tool version is hardcoded in reports

**Root Cause Analysis**:

In `cmd/run.go:183`:
```go
ToolVersion: "1.0.0",
```

This is always "1.0.0" regardless of the actual build version.

**Fix Plan**:

1. Add a version variable that can be set at build time:

```go
// cmd/run.go or a separate version.go
var (
    Version   = "dev"
    GitCommit = "unknown"
    BuildDate = "unknown"
)

// Or in Makefile:
// go build -ldflags "-X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT)"
```

2. Use the variable in buildReport:
```go
// cmd/run.go:183
ToolVersion: Version,
```

3. Build script example (Makefile):
```makefile
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

build:
	go build -ldflags "-X main.Version=$(VERSION) \
		-X main.GitCommit=$(GIT_COMMIT) \
		-X main.BuildDate=$(BUILD_DATE)" \
		-o bin/fio-bench ./cmd/...
```

---

## Summary

| Issue | Priority | Complexity | Files Affected |
|-------|----------|------------|----------------|
| 1 | P0 | Low | json.go |
| 2 | P0 | Low | parser.go |
| 3 | P1 | Medium | run.go |
| 4 | P1 | Low | jobs.go |
| 5 | P1 | Low | run.go |
| 6 | P1 | Medium | run.go, prompt.go |
| 7 | P1 | Medium | markdown.go, parser.go |
| 8 | P1 | Low | strategy.go |
| 9 | P1 | Low | prompt.go |
| 10 | P1 | Low | detector_linux.go |
| 11 | P1 | Medium | collector_darwin.go |
| 12 | P2 | Low | runner.go |
| 13 | P2 | Medium | collector_linux.go |
| 14 | P2 | Low | json.go |
| 15 | P2 | Low | strategy.go |
| 16 | P2 | Low | run.go + build config |

**Recommended Fix Order**:
1. P0 issues (1, 2) - Critical data correctness
2. P1 issues with data correctness impact (5, 7, 10, 11)
3. P1 issues with behavior impact (3, 4, 6, 8, 9)
4. P2 issues (12-16) - Polish and completeness
