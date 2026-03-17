# FIO Benchmark Tool - Design Document

## 1. Overview

A standalone Go program that performs disk I/O baseline testing using fio, generating performance reports.

### Core Features

- **Progressive Testing**: Quick sampling → intelligent analysis → targeted deep tests
- **Adaptive Strategy**: Skip redundant tests based on sampling results
- **Dual Reports**: Concise Markdown (human) + detailed JSON (machine, with raw fio logs)
- **Interactive CLI**: Guided workflow with smart defaults

---

## 2. Testing Flow

```
Phase 1: Quick Sampling (~30s)
    5 basic tests × 5s each → detect disk class
            ↓
Phase 2: Intelligent Analysis
    classify disk → detect redundancy → generate strategy
            ↓
Phase 3: Targeted Deep Testing (3-10 min)
    run only relevant tests for this disk class
```

---

## 3. Phase 1: Quick Sampling

| Test | fio Parameters |
|------|----------------|
| Sequential Read | `rw=read, bs=1M, ioengine=libaio, direct=1` |
| Sequential Write | `rw=write, bs=1M, ioengine=libaio, direct=1` |
| Random Read 4K | `rw=randread, bs=4k, ioengine=libaio, direct=1` |
| Random Write 4K | `rw=randwrite, bs=4k, ioengine=libaio, direct=1` |
| fsync | `rw=write, bs=4k, ioengine=sync, direct=0, fsync=1` |

**Failure Handling**: If any test returns 0 or fails → display error and exit.

---

## 4. Phase 2: Disk Classification

| Disk Class | Criteria |
|------------|----------|
| **NVMe SSD** | SeqRead > 2000 MB/s AND RandRead > 500K IOPS |
| **SATA SSD** | SeqRead > 400 MB/s AND RandRead > 50K IOPS |
| **Fast HDD** | SeqRead > 150 MB/s, low IOPS/MB ratio |
| **Slow HDD** | SeqRead ≤ 150 MB/s, low IOPS/MB ratio |

### Redundancy Detection

Skip tests when read/write performance is similar:

- Bandwidth difference < 10% → skip separate write bandwidth tests
- IOPS difference < 15% → skip separate write IOPS tests
- fsync > 10K IOPS → skip fsync deep test

---

## 5. Phase 3: Test Selection

### Test Categories

| Category | Tests |
|----------|-------|
| **Sequential** | Async/Direct, Sync/Direct, Sync/Buffered (read + write) |
| **Random 4K** | Async/Direct, Sync/Direct, Sync/Buffered (read + write) |
| **Mixed** | 70/30, 50/50 randrw |
| **Multi-BlockSize** | 4k, 16k, 64k, 256k, 1M |
| **fsync** | write + fsync=1 |
| **Latency** | Full percentile distribution |

### Selection by Disk Class

| Test Category | NVMe SSD | SATA SSD | HDD |
|--------------|:--------:|:--------:|:---:|
| Seq Async+Direct | ✓ | ✓ | ✓ |
| Seq Sync modes | - | ✓ | ✓ |
| Random Async+Direct | ✓ | ✓ | ✓ (iodepth=8) |
| Random Sync modes | ✓ (read only) | ✓ | - |
| Random Buffered | - | ✓ | - |
| Mixed 70/30 | ✓ | ✓ | - |
| Mixed 50/50 | ✓ | - | - |
| Multi-BlockSize | ✓ | ✓ | - |
| fsync | ✓ | ✓ | - |
| Latency | ✓ (P99.99) | ✓ (P99.9) | ✓ (P99) |

### Common Parameters

| Parameter | SSD | HDD |
|-----------|-----|-----|
| iodepth | 32 | 8 |
| numjobs | 4 | 2 |
| runtime | 60s | 60s |
| time_based | 1 | 1 |

---

## 6. Test File Size

| Partition Size | Default Test File |
|----------------|-------------------|
| < 64 GB | 1 GB |
| 64 GB - 256 GB | 2 GB |
| 256 GB - 1 TB | 4 GB |
| > 1 TB | 8 GB |

**Adjustments**:
- HDD: × 0.6 (faster test)
- Cap at 25% of free space
- Minimum: 1 GB

---

## 7. CLI

### Commands

```bash
fio-bench              # Interactive mode (default)
fio-bench run          # Interactive mode
fio-bench run --path /mnt/data --output ./reports  # Non-interactive
fio-bench run --quick  # Sampling only, skip Phase 3
fio-bench list         # List filesystems
```

### Interactive Flow

```
[Step 1/4] Select Target Filesystem
  #   Path          Type    Size      Free      Disk Type
  1   /             ext4    256 GB    89 GB    SSD
  2   /mnt/data     xfs     1.0 TB    450 GB   NVMe
  
  Select target [1-2]: 2

[Step 2/4] Quick Sampling (30 seconds)
  Sequential Read:  3,250 MB/s
  Sequential Write: 3,100 MB/s
  Random Read 4K:   850,000 IOPS
  Random Write 4K:  780,000 IOPS
  fsync:            45,000 IOPS
  
  Detected: NVMe SSD

[Step 3/4] Test Plan
  Tests to run (10 items, ~8 min):
    ✓ Async+Direct sequential read/write
    ✓ Async+Direct random read/write
    ✓ Mixed 70/30, 50/50
    ✓ Multi-blocksize, fsync, latency
  
  Tests skipped (8 items):
    ✗ Sync sequential (async covers it)
    ✗ Buffered mode (direct more representative)
  
  [P]roceed  [C]ustomize  [Q]uit: P

[Step 4/4] Running Tests...
  Reports: ./fio-reports/20240318_100000_server01_nvme0n1p1.{md,json}
```

---

## 8. Report Formats

### File Naming

```
{YYYYMMDD}_{HHMMSS}_{hostname}_{device_basename}.{ext}
```

### Markdown Report (Concise)

```markdown
# Disk I/O Benchmark Report

## Metadata
| Report ID | 20240318_100000_server01_nvme0n1p1 |
| Generated | 2024-03-18 10:08:23 UTC |
| Duration  | 8 min 23 sec |

## Host
| Hostname | server01 |
| IP       | 192.168.1.100 |
| Platform | linux/amd64, Ubuntu 22.04 |

## Target
| Mount     | /mnt/data (xfs) |
| Device    | /dev/nvme0n1p1 |
| Model     | Samsung SSD 990 PRO 1TB |
| Type      | NVMe SSD |

## Performance
| Metric          | Value     | Rating |
| Sequential Read | 3,250 MB/s | ★★★★★ |
| Sequential Write| 3,100 MB/s | ★★★★★ |
| Random Read 4K  | 850K IOPS  | ★★★★★ |
| Random Write 4K | 780K IOPS  | ★★★★☆ |
| fsync           | 45K IOPS   | ★★★★☆ |

**Overall: 92/100**

## Latency (P99)
| Random Read  | 8.2 μs |
| Random Write | 12.5 μs |
| fsync        | 22.1 μs |

## Recommendations
✓ Well-suited for OLTP workloads
```

### JSON Report (Detailed)

Full structure with:
- Complete metadata (host, target, fio, environment)
- Phase 1 sampling results
- Phase 2 strategy (tests planned/skipped with reasons)
- Phase 3 results (config + metrics per test)
- Summary (scores, bottleneck, recommendations)
- **Raw fio logs** for each test

---

## 9. Metadata (JSON Report)

| Category | Fields |
|----------|--------|
| **Report** | report_id, generated_at, tool_version, git_commit |
| **Host** | hostname, fqdn, ip_addresses, platform, arch, kernel, os, cpu_model, cpu_cores, memory |
| **Target** | path, filesystem_type, mount_options, device_path, device_model, device_serial, disk_type, disk_class, total_bytes, free_bytes, block_size |
| **fio** | version, path, compile_options, capabilities |
| **Test** | mode, duration, test_file_size, iodepth, numjobs, ioengine, tests_run/skipped |
| **Environment** | cpu_governor, swappiness, dirty_ratio |

---

## 10. Platform Support

| Platform | Status |
|----------|--------|
| Linux | Full support |
| macOS | Full support |
| Windows | Stub (ErrNotSupported) |

---

## 11. Dependencies

- Go 1.21+
- fio (external, must be in PATH)
- github.com/spf13/cobra

---

## 12. Directory Structure

```
benchmark/baseline/io/fio/
├── cmd/
│   ├── root.go
│   ├── list.go
│   └── run.go
├── internal/
│   ├── fio/
│   │   ├── runner.go
│   │   ├── parser.go
│   │   └── jobs.go
│   ├── fs/
│   │   ├── detector.go
│   │   ├── detector_linux.go
│   │   ├── detector_darwin.go
│   │   └── detector_windows.go
│   ├── metadata/
│   │   └── collector_*.go
│   ├── analyzer/
│   │   ├── classifier.go
│   │   └── strategy.go
│   ├── prompt/
│   │   └── prompt.go
│   └── report/
│       ├── types.go
│       ├── json.go
│       └── markdown.go
├── doc/
│   ├── design.md
│   └── spec.md
├── go.mod
└── README.md
```
