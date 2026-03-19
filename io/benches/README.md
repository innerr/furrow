# WAL Performance Benchmarks

This directory contains performance benchmarks for the WAL (Write-Ahead Log) implementation.

## Quick Start

```bash
# Run quick test (development)
./scripts/benchmark.sh quick

# Run all benchmarks
./scripts/benchmark.sh all

# Run specific benchmark
./scripts/benchmark.sh throughput
./scripts/benchmark.sh latency
./scripts/benchmark.sh sync_modes
./scripts/benchmark.sh features

# Compare with baseline
./scripts/benchmark.sh compare
```

## Benchmark Suite

### 1. Throughput (`throughput.rs`)

Tests write throughput across different data sizes and sync modes.

**Test scenarios:**
- 19 different write sizes (64B to 16MB)
- 4 sync modes (Always, Batch 4MB, Batch 1MB, Never)
- Sustained throughput (100MB data)

**Key metrics:**
- MB/s throughput
- IOPS (operations per second)

### 2. Latency (`latency.rs`)

Measures write latency distribution.

**Test scenarios:**
- 5 typical sizes (64B, 1KB, 4KB, 64KB, 1MB)
- 3 sync modes (Always, Batch, Never)
- Statistical distribution (P50, P90, P99, P999)

**Key metrics:**
- Average latency (μs)
- Latency percentiles

### 3. Sync Modes (`sync_modes.rs`)

Comprehensive comparison of sync strategies.

**Test scenarios:**
- All sync modes across multiple sizes
- Batch write patterns (1000 x 4KB)

**Key metrics:**
- Throughput comparison
- Safety vs performance tradeoff

### 4. Features (`features.rs`)

Measures overhead of compression and encryption.

**Test scenarios:**
- Compression alone (LZ4)
- Encryption alone (AES-GCM)
- Combined compression + encryption
- WAL with features enabled

**Key metrics:**
- Throughput overhead (%)
- Operation latency (μs)

## Viewing Results

### Console Output

Results are printed to console with statistics.

### HTML Reports

Detailed interactive reports generated at:
```
target/criterion/<benchmark_name>/report/index.html
```

Open in browser:
```bash
open target/criterion/throughput_by_size/report/index.html
```

### Baseline Comparison

Save baseline:
```bash
cargo bench -- --save-baseline main
```

Compare against baseline:
```bash
cargo bench -- --baseline main
```

## Performance Analysis

### CPU Profiling

```bash
# Profile with perf
perf record -g cargo bench --bench throughput
perf report

# Generate flamegraph
cargo install flamegraph
flamegraph -o flamegraph.svg -- cargo bench --bench throughput
```

### I/O Analysis

```bash
# Monitor I/O during benchmark
iostat -x 1 > iostat.log &
cargo bench --bench throughput
kill %1
```

## Environment Recommendations

For consistent and meaningful results:

1. **Storage**: SSD recommended (HDD will be bottleneck)
2. **Memory**: Sufficient RAM to avoid page cache eviction
3. **OS**: Linux for io_uring, macOS for tokio::fs fallback
4. **Isolation**: Run without other I/O-intensive tasks
5. **Power**: Disable CPU frequency scaling for consistent performance

## Expected Performance

On a modern SSD with Linux (io_uring backend):

| Size  | Throughput | Latency (μs) |
|-------|------------|--------------|
| 64B   | ~15 MB/s   | ~4           |
| 4KB   | ~400 MB/s  | ~10          |
| 64KB  | ~1.2 GB/s  | ~50          |
| 1MB   | ~1.8 GB/s  | ~550         |

*Note: Actual results vary by hardware and configuration.*

## Troubleshooting

### "Too many open files"

Increase ulimit:
```bash
ulimit -n 65536
```

### Slow performance on macOS

macOS uses tokio::fs (not io_uring), expect lower performance than Linux.

### Criterion not found

Install Criterion:
```bash
cargo install cargo-criterion
```

## Adding New Benchmarks

1. Create new file in `benches/benches/`
2. Add to `benches/Cargo.toml`:
```toml
[[bench]]
name = "your_benchmark"
harness = false
```
3. Update `scripts/benchmark.sh`

## Architecture

See `wal/doc/benchmark-design.md` for detailed design documentation.
