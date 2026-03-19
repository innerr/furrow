# WAL Performance Benchmark Design

## Overview

This document describes the performance benchmark framework for WAL implementations. The framework supports testing multiple WAL implementations (concurrent vs single-threaded) with a shared test suite.

## Architecture

### Directory Structure

```
io/
├── Cargo.toml                              # workspace root
│   [workspace]
│   members = ["wal", "wal-single-thread", "benches"]
│
├── wal/                                    # Current concurrent implementation
│   ├── src/
│   │   ├── lib.rs                         # pub struct Wal
│   │   ├── config.rs
│   │   ├── error.rs
│   │   ├── record.rs                      # Format A (independent)
│   │   ├── writer_uring.rs                # io_uring backend
│   │   ├── writer_tokio.rs                # tokio::fs backend
│   │   └── ...
│   └── Cargo.toml
│
├── wal-single-thread/                      # Single-threaded implementation (experimental)
│   ├── src/
│   │   ├── lib.rs                         # pub struct Wal (same interface)
│   │   ├── config.rs
│   │   ├── error.rs
│   │   └── record.rs                      # Format B (independent, optimized for single-thread)
│   └── Cargo.toml
│
└── benches/                                # Shared benchmark suite
    ├── Cargo.toml
    ├── src/
    │   ├── lib.rs                         # Shared macros and utilities
    │   └── compare.rs                     # Comparison logic
    └── benches/
        ├── throughput.rs                  # Throughput tests
        ├── latency.rs                     # Latency distribution tests
        ├── write_sizes.rs                 # Different write sizes
        ├── sync_modes.rs                  # Sync strategy comparison
        └── features.rs                    # Compression/encryption overhead
```

### Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| **Separate crates** | Independent evolution, no shared code constraints |
| **Shared benches/** | One test suite for all implementations, easy comparison |
| **Independent file formats** | Each implementation can optimize its own format |
| **Experimental status** | `wal-single-thread` may be deleted if not useful |

## Benchmark Test Matrix

### 1. Write Size Impact (Core Focus)

Test different write sizes to understand throughput/latency characteristics.

| Category | Sizes | Purpose |
|----------|-------|---------|
| Small | 64B, 128B, 256B, 512B, 1KB | High IOPS, lock contention |
| Medium | 2KB, 4KB, 8KB, 16KB, 32KB, 64KB | Typical workloads |
| Large | 128KB, 256KB, 512KB, 1MB, 2MB, 4MB, 8MB, 16MB | Throughput bound |

**Metrics:**
- Throughput: MB/s
- IOPS: operations per second
- Latency: average, P50, P90, P99, P999, max (microseconds)

### 2. Sync Mode Comparison

| Mode | Configuration | Use Case |
|------|---------------|----------|
| Always | fsync after each write | Highest durability |
| Batch (4MB) | Batch { bytes: 4MB, time: 100ms } | Balanced |
| Batch (1MB) | Batch { bytes: 1MB, time: 100ms } | Balanced |
| Never | No fsync | Performance only (unsafe) |

### 3. Backend Comparison (Linux)

| Backend | Platform | Notes |
|---------|----------|-------|
| io_uring | Linux | Uses tokio-uring, zero-copy |
| tokio::fs | All platforms | Fallback, uses std::fs |

### 4. Concurrency Scaling

| Threads | Purpose |
|---------|---------|
| 1 | Baseline, no lock contention |
| 2 | Minimal contention |
| 4 | Moderate contention |
| 8 | High contention |
| 16 | Maximum contention |

### 5. Optional Feature Overhead

| Configuration | Purpose |
|---------------|---------|
| Baseline (no features) | Reference performance |
| Compression | Measure compression overhead |
| Encryption | Measure encryption overhead |
| Compression + Encryption | Combined overhead |

## Benchmark Implementation Details

### 1. Workspace Configuration

```toml
# io/Cargo.toml
[workspace]
members = ["wal", "wal-single-thread", "benches"]
resolver = "2"
```

### 2. Benchmark Crate

```toml
# benches/Cargo.toml
[package]
name = "wal-bench"
version = "0.1.0"
publish = false

[dependencies]
wal = { path = "../wal" }
wal-single-thread = { path = "../wal-single-thread" }
tokio = { version = "1", features = ["full"] }
criterion = { version = "0.5", features = ["async_tokio", "html_reports"] }
tempfile = "3"
rand = "0.8"

[[bench]]
name = "throughput"
harness = false

[[bench]]
name = "latency"
harness = false

[[bench]]
name = "sync_modes"
harness = false

[[bench]]
name = "features"
harness = false
required-features = ["compression", "encryption"]
```

### 3. Shared Test Utilities

```rust
// benches/src/lib.rs

/// Data sizes to test
pub const WRITE_SIZES: &[usize] = &[
    64, 128, 256, 512, 1024,  // Small
    2 * 1024, 4 * 1024, 8 * 1024, 16 * 1024, 32 * 1024, 64 * 1024,  // Medium
    128 * 1024, 256 * 1024, 512 * 1024,  // Large
    1024 * 1024, 2 * 1024 * 1024, 4 * 1024 * 1024, 8 * 1024 * 1024, 16 * 1024 * 1024,  // Very large
];

/// Generate random data of given size
pub fn generate_data(size: usize) -> Vec<u8> {
    use rand::Rng;
    let mut rng = rand::thread_rng();
    (0..size).map(|_| rng.gen()).collect()
}

/// Create a temp directory for benchmark
pub fn temp_wal_dir() -> tempfile::TempDir {
    tempfile::tempdir().expect("Failed to create temp dir")
}

/// Macro to define benchmark for any WAL implementation
#[macro_export]
macro_rules! bench_wal_impl {
    ($group:ident, $wal_mod:ty, $name:expr, $size:expr, $sync_mode:expr) => {
        $group.bench_function(format!("{}_{}b", $name, $size), |b| {
            b.to_async(tokio::runtime::Runtime::new().unwrap())
                .iter(|| async {
                    let dir = $crate::temp_wal_dir();
                    let config = wal::WalConfig::new(dir.path())
                        .sync_mode($sync_mode);
                    let wal = <$wal_mod>::open(config).await.unwrap();
                    
                    let data = $crate::generate_data($size);
                    wal.write(&data).await.unwrap();
                    wal.close().await.unwrap();
                })
        });
    };
}
```

### 4. Throughput Benchmark

```rust
// benches/benches/throughput.rs
use criterion::{criterion_group, Criterion, Throughput};
use wal::{Wal, WalConfig, SyncMode};
use wal_single_thread::Wal as SingleThreadWal;

fn throughput_comparison(c: &mut Criterion) {
    let mut group = c.benchmark_group("throughput");
    
    for &size in wal_bench::WRITE_SIZES.iter() {
        group.throughput(Throughput::Bytes(size as u64));
        
        // Concurrent implementation
        group.bench_function(format!("concurrent_{}b", size), |b| {
            b.iter(|| {
                // Test concurrent WAL throughput
            })
        });
        
        // Single-thread implementation
        group.bench_function(format!("single_thread_{}b", size), |b| {
            b.iter(|| {
                // Test single-thread WAL throughput
            })
        });
    }
    
    group.finish();
}

criterion_group!(benches, throughput_comparison);
criterion::criterion_main!(benches);
```

### 5. Latency Benchmark

```rust
// benches/benches/latency.rs
use criterion::{criterion_group, Criterion};

fn latency_distribution(c: &mut Criterion) {
    let mut group = c.benchmark_group("latency");
    
    // Measure latency distribution with detailed statistics
    // P50, P90, P99, P999, max
    
    for &size in &[64, 1024, 4096, 65536] {
        // Test each size with latency-focused measurement
    }
    
    group.finish();
}

criterion_group!(benches, latency_distribution);
criterion::criterion_main!(benches);
```

### 6. Sync Mode Benchmark

```rust
// benches/benches/sync_modes.rs
use criterion::{criterion_group, Criterion};
use std::time::Duration;

fn sync_mode_comparison(c: &mut Criterion) {
    let modes = vec![
        ("always", SyncMode::Always),
        ("batch_4mb", SyncMode::Batch { bytes: 4 * 1024 * 1024, time: Duration::from_millis(100) }),
        ("batch_1mb", SyncMode::Batch { bytes: 1 * 1024 * 1024, time: Duration::from_millis(100) }),
        ("never", SyncMode::Never),
    ];
    
    for (name, mode) in modes {
        // Test each sync mode
    }
}

criterion_group!(benches, sync_mode_comparison);
criterion::criterion_main!(benches);
```

## Running Benchmarks

### Quick Test (Development)

```bash
# Test single benchmark
cargo bench --bench throughput

# Test specific size
cargo bench -- "throughput/concurrent_1024b"
```

### Full Benchmark Suite

```bash
# Run all benchmarks
cargo bench

# Generate HTML reports in target/criterion/
```

### Compare Implementations

```bash
# Run comparison benchmark
cargo bench --bench throughput -- --save-baseline concurrent

# Switch to single-thread and compare
cargo bench --bench throughput -- --baseline concurrent
```

## Expected Results Format

### Console Output

```
=== WAL Performance Report ===
Platform: Linux x86_64
Backend: io_uring
Date: 2026-03-19

1. Write Throughput by Size (Concurrent)
Size      | Throughput (MB/s) | IOPS     | Avg Lat (us)
----------|-------------------|----------|-------------
64 B      | 15.2              | 243,200  | 4.1
1 KB      | 125.8             | 128,819  | 7.8
4 KB      | 412.3             | 105,792  | 9.5
64 KB     | 1,245.6           | 19,929   | 50.2
1 MB      | 1,823.4           | 1,823    | 548.5

2. Sync Mode Comparison (1KB writes)
Mode              | Throughput | Latency (us) | Durability
------------------|------------|--------------|------------
Always            | 125.8 MB/s | 7.8          | Highest
Batch (4MB/100ms) | 523.1 MB/s | 1.9          | High
Batch (1MB/100ms) | 412.5 MB/s | 2.4          | High
Never             | 1,245.3 MB/s | 0.8        | None

3. Concurrent vs Single-Thread (4KB writes, Batch mode)
Implementation   | Throughput | Latency (us)
-----------------|------------|-------------
Concurrent       | 412.3 MB/s | 9.5
Single-Thread    | 485.6 MB/s | 8.2

4. Feature Overhead (64KB writes)
Configuration     | Throughput | Overhead
------------------|------------|----------
Baseline          | 1,245.6 MB/s | -
Compression       | 412.3 MB/s  | -66.9%
Encryption        | 823.5 MB/s  | -33.9%
Comp + Enc        | 312.5 MB/s  | -74.9%

5. Concurrency Scaling (4KB writes, Batch mode)
Threads | Throughput | Scaling | Lock Contention
--------|------------|---------|----------------
1       | 485.6 MB/s | 1.00x   | 0%
2       | 685.2 MB/s | 1.41x   | 12%
4       | 1,102.5 MB/s | 2.27x | 28%
8       | 1,523.8 MB/s | 3.14x | 45%
16      | 1,623.1 MB/s | 3.34x | 62%
```

### HTML Reports

Generated in `target/criterion/`:
- Interactive charts
- Statistical analysis
- Regression detection

## Performance Analysis Tools

### 1. CPU Profiling

```bash
# Profile with perf
perf record -g cargo bench --bench throughput
perf report

# Generate flamegraph
cargo install flamegraph
flamegraph -o flamegraph.svg -- cargo bench --bench throughput
```

### 2. Memory Analysis

```bash
# Track heap allocations (requires valgrind)
valgrind --tool=massif cargo bench --bench throughput
ms_print massif.out.*

# Or use heaptrack
heaptrack cargo bench --bench throughput
heaptrack_print heaptrack.*.gz
```

### 3. I/O Analysis

```bash
# Monitor I/O during benchmark
iostat -x 1 > iostat.log &
cargo bench --bench throughput
kill %1
```

## Implementation Checklist

### Phase 1: Setup
- [ ] Create workspace Cargo.toml
- [ ] Create benches/Cargo.toml
- [ ] Create benches/src/lib.rs with utilities
- [ ] Update wal/Cargo.toml with benchmark config

### Phase 2: Core Benchmarks
- [ ] Implement throughput.rs
- [ ] Implement latency.rs
- [ ] Implement write_sizes.rs

### Phase 3: Comparison Benchmarks
- [ ] Implement sync_modes.rs
- [ ] Implement features.rs (with feature flags)

### Phase 4: wal-single-thread
- [ ] Create wal-single-thread crate
- [ ] Implement basic single-threaded WAL
- [ ] Run comparison benchmarks

### Phase 5: Analysis
- [ ] Create benchmark runner script
- [ ] Create result analysis script
- [ ] Document baseline performance numbers

## Notes

### Test Environment Requirements
- **Storage**: SSD recommended (HDD will be bottleneck)
- **Memory**: Sufficient RAM to avoid page cache eviction
- **OS**: Linux for io_uring testing, macOS for tokio::fs fallback
- **Isolation**: Run without other I/O-intensive tasks

### Benchmarking Best Practices
1. **Warm-up**: Each test includes warm-up phase
2. **Multiple runs**: Run 3-5 times, report median
3. **Clean state**: Clear temp directories between tests
4. **Consistent environment**: Same machine, same conditions
5. **Statistical rigor**: Use criterion's statistical analysis

### Known Limitations
- io_uring benchmarks only work on Linux
- Compression/encryption benchmarks require feature flags
- Concurrent scaling limited by hardware (CPU cores, storage bandwidth)
