# Confirmed Issues

This file tracks defects confirmed by code review against the current implementation.

## P0 - Critical

### 1. Overall score is multiplied by 100 again
**Files**:
- `internal/report/json.go:48-70`

**Issue**:
`total` is already a weighted score in the `0-100` range, but `CalculateOverallScore()` returns:

```go
return int(total / weightSum * 100)
```

This inflates the final score by 100x and makes the report summary unusable.

**Fix**:
Return the normalized weighted average directly:

```go
return int(total / weightSum)
```

---

### 2. fio bandwidth parsing uses the wrong JSON field
**Files**:
- `internal/fio/parser.go:140-159`

**Issue**:
`extractMetricsFromJob()` derives MB/s from `bw_agg`:

```go
metrics.BandwidthMBps = float64(job.Read.BandwidthAgg) / 1024
```

`bw_agg` is fio's aggregation percentage, not throughput. The code should read the actual bandwidth field (`bw` / `bw_bytes`). As written, sequential and random bandwidth are reported near zero or otherwise incorrect, which also corrupts classification, scoring, and strategy decisions.

**Fix**:
Parse the real fio bandwidth field and convert it to MB/s with the correct unit.

---

## P1 - High Priority

### 3. Test file size is computed before disk classification exists
**Files**:
- `cmd/run.go:78`

**Issue**:
`targetFS.DiskClass` is still empty when passed to `CalculateTestFileSize()`, so the HDD size reduction path never runs.

**Fix**:
Use detector-derived disk type for the pre-sampling estimate, or recompute file size after Phase 1 once `sampleResult.DiskClass` is known.

---

### 4. Test file sizing violates the 25% free-space cap
**Files**:
- `internal/fio/jobs.go:219-226`

**Issue**:
The code caps the size to `25%` of free space, then forces a minimum of `1 GB` afterwards:

```go
if size > maxAllowed {
    size = maxAllowed
}

if size < GB {
    size = GB
}
```

On small volumes this can exceed the documented cap and even exceed available space.

**Fix**:
Validate free space first and only apply the minimum when it still satisfies the configured cap.

---

### 5. Report metadata drops the actual test file info and disk class
**Files**:
- `cmd/run.go:179-208`
- `internal/report/markdown.go:56-77`

**Issue**:
- `TestFileSizeBytes` is hardcoded to `0`
- `TestFilePath` is hardcoded to `""`
- `target.DiskClass` is never updated from the sampling result before being copied into report metadata

This leaves the JSON/Markdown report with missing or empty target/test metadata.

**Fix**:
Populate these fields from the actual runtime values before building the report.

---

### 6. Deep-test failures still produce a “successful” report
**Files**:
- `cmd/run.go:139-158`
- `internal/prompt/prompt.go:112-119`

**Issue**:
Phase 3 test failures are only logged and skipped. If every planned test fails, the tool still generates reports and prints `All tests completed successfully`.

**Fix**:
Return an error when no deep-test results are collected, and report partial failure explicitly when only some tests succeed.

---

### 7. Latency section in Markdown never gets usable P99 data
**Files**:
- `internal/report/markdown.go:115-136`
- `internal/fio/parser.go:179-206`
- `internal/fio/jobs.go:92-99`

**Issue**:
- Markdown reads latency from `rand_read_4k_async_direct` / `rand_write_4k_async_direct`, but the dedicated percentile-enabled jobs are `latency_read` / `latency_write`
- The parser stores percentile keys as `p99_000000`, `p99_900000`, etc.
- Markdown looks up `p99`

As a result the latency table is usually empty even when latency jobs ran successfully.

**Fix**:
Read latency from the dedicated latency jobs and normalize percentile keys consistently.

---

### 8. Redundancy detection skips the wrong tests
**Files**:
- `internal/analyzer/strategy.go:29-54`

**Issue**:
When read/write performance is similar, the implementation removes only sync/buffered write tests. The main async write tests (`seq_write_async_direct`, `rand_write_4k_async_direct`) are still kept, which does not match the documented “skip separate write bandwidth/IOPS tests” rule.

**Fix**:
Apply redundancy elimination to the actual tests that dominate the benchmark plan for each disk class.

---

### 9. Interactive input ignores `ReadString` errors
**Files**:
- `internal/prompt/prompt.go:33-35`
- `internal/prompt/prompt.go:72-74`

**Issue**:
The code ignores input errors:

```go
input, _ := reader.ReadString('\n')
```

EOF or stdin failure falls through as an empty string, which can trap the user in the selection loop or silently choose the default action.

**Fix**:
Return the read error and let the caller exit cleanly.

---

### 10. Linux NVMe base-device extraction is broken
**Files**:
- `internal/fs/detector_linux.go:179-187`

**Issue**:
For names like `nvme0n1p1`, this code returns an empty string:

```go
parts := strings.SplitN(name, "n", 2)
return parts[0]
```

That breaks `/sys/block/...` lookup for NVMe devices, so disk type/model/block size metadata is not populated on Linux NVMe targets.

**Fix**:
Strip only the partition suffix (`p1`, `p2`, ...) and keep the base device name such as `nvme0n1`.

---

### 11. macOS host metadata reports wrong free memory and swap size
**Files**:
- `internal/metadata/collector_darwin.go:137-150`
- `internal/metadata/collector_darwin.go:172-179`

**Issue**:
- `getMemoryInfo()` uses `syscall.Statfs("/")`, which returns filesystem free space, not free RAM
- `parseSize()` trims the unit suffix before checking it, so `G` values are always interpreted as MB

This makes the macOS metadata block materially incorrect.

**Fix**:
Collect memory from a real VM/stat source and preserve the original unit before converting swap sizes.

---

## P2 - Medium Priority

### 12. `VersionNumeric` is never populated
**Files**:
- `internal/fio/runner.go:41-46`
- `internal/fio/parser.go:215-228`

**Issue**:
`ParseFioVersion()` already returns the numeric version parts, but `GetFioInfo()` stores only the raw string.

**Fix**:
Persist the parsed numeric version in `types.FioInfo.VersionNumeric`.

---

### 13. Linux physical core count is actually socket count
**Files**:
- `internal/metadata/collector_linux.go:141-166`

**Issue**:
`getCPUInfo()` counts unique `physical id` values, which represent CPU packages/sockets, not physical cores. On a typical single-socket machine this reports `1` physical core.

**Fix**:
Combine `physical id` with `core id`, or use another source that exposes physical core count directly.

---

### 14. Recommendations treat missing scores as zero
**Files**:
- `internal/report/json.go:118-140`

**Issue**:
`GenerateRecommendations()` reads `scores["fsync"]` and `scores["rand_write"]` without checking whether those scores exist. When a test is skipped or failed, Go returns `0`, so the report emits misleading warnings such as fsync advice for HDD plans that intentionally skipped fsync.

**Fix**:
Gate recommendations on score presence, not on zero-value map lookups.

---

### 15. `fsync_deep` skip reason references a non-existent test
**Files**:
- `internal/analyzer/strategy.go:52-53`

**Issue**:
The strategy records a skip reason for `fsync_deep`, but there is no such test in `TestCatalog` or the selection matrix. The reason is never surfaced consistently and does not affect planning.

**Fix**:
Either remove the dead name or rename it to the real fsync test entry.

---

### 16. Tool version is hardcoded in reports
**Files**:
- `cmd/run.go:181-183`

**Issue**:
Reports always emit `ToolVersion: "1.0.0"` regardless of the actual build.

**Fix**:
Inject the version at build time, for example through `-ldflags`.
