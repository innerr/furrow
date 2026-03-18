# Known Issues and Bugs

## P0 - Critical (Must Fix)

### 1. CalculateOverallScore calculation error
**File**: `internal/report/json.go:70`

**Issue**: 
```go
return int(total / weightSum * 100)
```
This inflates the score 100x. `total` already contains the weighted average (0-100 range), multiplying by 100 again is wrong.

**Fix**:
```go
return int(total)
```

---

### 2. Empty DiskClass passed to CalculateTestFileSize
**File**: `cmd/run.go:78`

**Issue**:
```go
testFileSize := fio.CalculateTestFileSize(targetFS.TotalBytes, targetFS.FreeBytes, targetFS.DiskClass)
```
`targetFS.DiskClass` is empty at this point because it's only set after sampling. This causes the HDD 0.6 factor to never apply.

**Fix**:
1. Use `targetFS.DiskType` instead, or
2. Call this function after sampling when `sampleResult.DiskClass` is available

---

### 3. ReadString error ignored
**File**: `internal/prompt/prompt.go:34, 73`

**Issue**:
```go
input, _ := reader.ReadString('\n')
```
Error is silently ignored, which may cause infinite loop or panic on EOF.

**Fix**:
```go
input, err := reader.ReadString('\n')
if err != nil {
    return "", err
}
```

---

## P1 - High Priority

### 4. TestFileSizeBytes always 0 in report
**File**: `cmd/run.go:189-191`

**Issue**: `TestFileSizeBytes` is hardcoded to 0, should record actual value.

**Fix**:
```go
TestFileSizeBytes: testFileSize,
```

---

### 5. TestFilePath always empty in report
**File**: `cmd/run.go:189-191`

**Issue**: `TestFilePath` is hardcoded to empty string.

**Fix**:
```go
TestFilePath: testFile,
```

---

### 6. VersionNumeric always nil
**File**: `internal/fio/runner.go:41-46`

**Issue**: `GetFioInfo()` doesn't set `VersionNumeric` field.

**Fix**: Parse version in `getFioVersion()` and store it.

---

### 7. Wrong test names in redundancy detection
**File**: `internal/analyzer/strategy.go:32-36, 43-45`

**Issue**: 
- Skips `seq_write_sync_direct` but NVMe's selection.Run doesn't include this test
- Should skip `seq_write_async_direct` when read/write bandwidth is similar

---

### 8. Duplicate formatBytes functions
**Files**: 
- `internal/prompt/prompt.go:149-170`
- `internal/report/markdown.go:151-172`
- `cmd/list.go:48-60`

**Issue**: Same function defined 3 times.

**Fix**: Move to a shared utility package.

---

### 9. fsync_deep test doesn't exist
**File**: `internal/analyzer/strategy.go:52-54`

**Issue**: Code tries to skip `fsync_deep` but this test is not defined in TestCatalog.

---

## P2 - Medium Priority

### 10. Missing unit tests
**Issue**: No test coverage for any module.

**Fix**: Add unit tests for:
- `internal/fio/parser.go` - JSON parsing
- `internal/analyzer/classifier.go` - Disk classification
- `internal/report/json.go` - Score calculation

---

### 11. No Ctrl+C graceful shutdown
**File**: `cmd/run.go`

**Issue**: During Phase 3 tests, Ctrl+C doesn't clean up test file.

**Fix**: Use signal handler to cleanup on interrupt.

---

### 12. macOS block device access requires permission
**File**: `internal/fs/detector_darwin.go:195`

**Issue**: `syscall.Open(device, O_RDONLY, 0)` requires root or disk group membership.

**Fix**: Add permission check and user-friendly error message.

---

### 13. df output field index hardcoded
**File**: `internal/fs/detector_darwin.go:45`

**Issue**: Assumes mount point is at index 8, may vary across macOS versions.

**Fix**: Parse df output more robustly or use different method.

---

### 14. All tests failing still generates report
**File**: `cmd/run.go:139-143`

**Issue**: If all Phase 3 tests fail, an empty report is still generated.

**Fix**: Check if `len(results) == 0` and return error.

---

### 15. Zero value check incomplete
**File**: `internal/fio/runner.go:220-221`

**Issue**: Only checks `IOPS == 0 && BandwidthMBps == 0`, but some tests may have only one metric.

**Fix**: Check based on test type (bandwidth tests vs IOPS tests).

---

## Design Issues

### 16. DiskClass vs DiskType confusion
**File**: `internal/types/types.go`

**Issue**: Both `DiskClass` and `DiskType` exist with overlapping purposes:
- `DiskType`: "hdd", "ssd", "nvme" (from detector)
- `DiskClass`: "SlowHDD", "FastHDD", "SATA_SSD", "NVMe_SSD" (from sampling)

**Fix**: Clarify usage or consolidate into one field.

---

### 17. ToolVersion hardcoded
**File**: `cmd/run.go:183`

**Issue**: Version is hardcoded as "1.0.0".

**Fix**: Inject version at build time via `-ldflags`.
