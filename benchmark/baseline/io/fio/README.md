# fio-bench

A progressive, adaptive disk I/O benchmark tool that uses fio to measure storage performance and generate detailed reports.

## Features

- **Progressive Testing**: Quick sampling → intelligent analysis → targeted deep tests
- **Adaptive Strategy**: Automatically skips redundant tests based on disk class
- **Dual Reports**: Concise Markdown (human) + detailed JSON (machine, with raw fio logs)
- **Interactive CLI**: Guided workflow with smart defaults
- **Cross-Platform**: Linux, macOS (Windows stub)

## Requirements

- Go 1.21+
- fio (must be installed and in PATH)

### Installing fio

```bash
# Linux (Debian/Ubuntu)
sudo apt install fio

# Linux (RHEL/CentOS)
sudo yum install fio

# macOS
brew install fio
```

## Installation

```bash
go build -o fio-bench .
```

## Usage

### Interactive Mode (Recommended)

```bash
./fio-bench
# or
./fio-bench run
```

The interactive mode guides you through:
1. Select target filesystem
2. Quick sampling (30 seconds)
3. Review test plan
4. Run deep tests

### Non-Interactive Mode

```bash
# Run benchmark on specific path
./fio-bench run --path /mnt/data --output ./reports

# Quick mode (sampling only)
./fio-bench run --path /mnt/data --quick
```

### List Filesystems

```bash
./fio-bench list
./fio-bench list --format json
```

## Test Strategy

### Phase 1: Quick Sampling (30s)

5 basic tests × 5 seconds each to detect disk class:
- Sequential read/write bandwidth
- Random read/write IOPS
- fsync performance

### Phase 2: Intelligent Analysis

- Classify disk type (NVMe SSD / SATA SSD / Fast HDD / Slow HDD)
- Detect redundant tests (skip if read/write performance similar)
- Generate adaptive test strategy

### Phase 3: Targeted Deep Testing (3-10 min)

Run only relevant tests for the detected disk class.

## Output

Reports are saved to the output directory (default: `./fio-reports/`):

```
./fio-reports/
├── 20240318_100000_server01_nvme0n1p1.md      # Human-readable
└── 20240318_100000_server01_nvme0n1p1.json    # Machine-readable + raw logs
```

### Markdown Report

Concise summary with:
- Metadata (host, target device, test configuration)
- Performance metrics with star ratings
- Latency percentiles
- Recommendations

### JSON Report

Detailed output including:
- Complete metadata
- Phase 1 sampling results
- Phase 2 strategy (tests planned/skipped with reasons)
- Phase 3 results (config + metrics per test)
- Raw fio logs for each test

## Test Categories

| Category | Tests |
|----------|-------|
| Sequential | Async/Direct, Sync/Direct, Sync/Buffered (read + write) |
| Random 4K | Async/Direct, Sync/Direct, Sync/Buffered (read + write) |
| Mixed | 70/30, 50/50 randrw |
| Multi-BlockSize | 4k, 16k, 64k, 256k, 1M |
| fsync | write + fsync=1 |
| Latency | Full percentile distribution |

## License

Apache-2.0
