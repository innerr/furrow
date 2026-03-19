# WAL Benchmark Design for the Current Repository

## Overview

This document describes a shared benchmark framework for WAL implementations. The framework supports testing both `wal` (concurrent) and `wal-single-thread` implementations with a unified test suite.

## Goals

- Measure steady-state write throughput for WAL implementations.
- Measure per-write latency distribution under controlled conditions.
- Compare `wal` and `wal-single-thread` performance characteristics.
- Compare the impact of different `SyncMode` settings.
- Measure optional feature overhead for compression and encryption.
- Maintain stable benchmark naming for historical baseline comparisons.

## Non-Goals

- Do not treat lifecycle operations such as temp directory creation, `Wal::open`, and `Wal::close` as part of steady-state throughput unless a benchmark explicitly targets lifecycle cost.
- Do not claim percentile latency from a harness that only records end-to-end batch timings.

## Scope

### Current Scope

The benchmark suite targets both WAL implementations:

- Throughput benchmark by write size (both implementations).
- Latency benchmark with per-operation sampling (both implementations).
- Sync mode comparison (both implementations).
- Feature overhead comparison (both implementations).
- Concurrent writer benchmark (wal only, wal-single-thread single writer).
- Implementation comparison with identical test conditions.

### Planned Extension

- Result export in machine-readable formats (JSON, CSV).
- Baseline management for regression detection.
- Automated performance comparison reports.

## Benchmark Categories

### 1. Throughput

Purpose:

- Measure sustained write bandwidth and operations per second.

Metrics:

- MB/s
- ops/s

Method:

- Open one WAL instance before measurement.
- Pre-generate payload buffers outside the timed loop.
- Run many writes per sample to reduce harness overhead.
- Keep cleanup outside the measured section as much as possible.

Initial sizes:

- 64 B
- 256 B
- 1 KiB
- 4 KiB
- 16 KiB
- 64 KiB
- 256 KiB
- 1 MiB
- 2 MiB
- 4 MiB

### 2. Latency

Purpose:

- Measure individual write latency and tail latency.

Metrics:

- average
- p50
- p90
- p99
- p999
- max

Method:

- Record one latency sample per write.
- Use a fixed operation count per case.
- Report histogram-style percentile summaries after the run.

Initial sizes:

- 64 B
- 1 KiB
- 4 KiB
- 64 KiB

### 3. Sync Mode Comparison

Modes:

| Mode | Config | Purpose |
|------|--------|---------|
| Always | `SyncMode::Always` | maximum durability |
| Batch 1 MiB | `SyncMode::Batch { bytes: 1 MiB, time: 100 ms }` | lower threshold batching |
| Batch 4 MiB | `SyncMode::Batch { bytes: 4 MiB, time: 100 ms }` | larger batch threshold |
| Never | `SyncMode::Never` | upper-bound write-path performance |

### 4. Feature Overhead

Configurations:

| Configuration | Purpose |
|---------------|---------|
| Baseline | reference |
| Compression | compression cost |
| Encryption | encryption cost |
| Compression + Encryption | combined overhead |

### 5. Concurrency Scaling

Writer counts:

| Writers | Purpose |
|---------|---------|
| 1 | baseline |
| 2 | light contention |
| 4 | moderate contention |
| 8 | higher contention |

## Measurement Rules

- Generate payloads once per case, not once per write.
- Use a dedicated temp directory per benchmark case.
- Set `max_file_size` high enough to avoid unintended rotation in baseline throughput tests.
- Keep `preallocate_size` fixed and documented for all baseline runs.
- Run benchmarks in release mode.
- Print the platform and enabled feature flags with every run.

## Repository Layout

The benchmark uses a workspace structure with a shared benchmark crate:

```text
io/
├── Cargo.toml                              # workspace root
│   [workspace]
│   members = ["wal", "wal-single-thread", "benches"]
│
├── wal/                                    # Concurrent implementation
│   ├── src/
│   └── Cargo.toml
│
├── wal-single-thread/                      # Single-threaded implementation
│   ├── src/
│   └── Cargo.toml
│
└── benches/                                # Shared benchmark suite
    ├── Cargo.toml
    ├── src/
    │   └── lib.rs                         # Shared utilities
    └── benches/
        ├── throughput.rs
        ├── latency.rs
        ├── sync_modes.rs
        ├── features.rs
        └── concurrency.rs
```

Benefits of this structure:

- Independent evolution of each implementation
- Shared test suite ensures consistent comparison
- Single source of truth for benchmark methodology

## Harness Design

### Shared Utilities

Shared helpers should provide:

- temp directory creation
- standard `WalConfig` construction
- deterministic payload generation
- repeated-write loops
- concurrent task launch helpers

### Throughput Harness

Recommended approach:

- use `criterion`
- use `BenchmarkGroup`
- report `Throughput::Bytes`
- write multiple records per iteration

### Latency Harness

Recommended approach:

- use a dedicated latency benchmark entry
- record one duration per write
- compute percentile summaries from raw samples

Reasoning:

- throughput and latency are both important
- a single harness should not force one metric to be measured poorly in order to collect the other

### Implementation-Agnostic Macro

```rust
/// Macro to define benchmark for any WAL implementation
#[macro_export]
macro_rules! bench_wal_impl {
    ($group:ident, $wal_ty:ty, $name:expr, $size:expr, $sync_mode:expr) => {
        $group.bench_function(format!("{}_{}b", $name, $size), |b| {
            b.iter(|| {
                // Benchmark logic
            })
        });
    };
}
```

This allows writing benchmark code once and applying it to both implementations.

## Reporting Format

Every benchmark should print:

- benchmark name
- implementation name
- platform
- write size
- sync mode
- total bytes
- total operations
- throughput in MB/s
- throughput in ops/s

Latency benchmarks should additionally print:

- average latency
- p50
- p90
- p99
- p999
- max

Example throughput row:

```text
case=throughput impl=wal size=4096 sync=batch_4m ops=200000 bytes=819200000 mbps=412.3 ops_per_sec=105792
```

Example latency row:

```text
case=latency impl=wal size=4096 sync=always samples=100000 avg_us=9.5 p50_us=8.7 p90_us=10.9 p99_us=18.4 p999_us=41.2 max_us=88.0
```

## Commands

Examples after implementation:

```bash
# Run specific benchmark
cargo bench --bench throughput
cargo bench --bench latency
cargo bench --bench sync_modes
cargo bench --bench concurrency
cargo bench --bench features --features compression,encryption

# Compare implementations
cargo bench --bench throughput -- --save-baseline wal
cargo bench --bench throughput -- --baseline wal  # Compare against saved baseline
```

## Implementation Plan

### Phase 1: Core Framework

- Create workspace Cargo.toml with members
- Create benches/Cargo.toml and benches/src/lib.rs
- Implement shared utilities in benches/src/lib.rs
- Implement throughput.rs for both implementations
- Implement latency.rs for both implementations
- Implement sync_modes.rs
- Implement features.rs
- Implement concurrency.rs

### Phase 2: wal-single-thread

- Create wal-single-thread crate
- Implement basic single-threaded WAL
- Run comparison benchmarks
- Document performance differences

### Phase 3: Analysis & Tooling

- Add result export (JSON, CSV)
- Document baseline numbers for reference environments
- Create baseline management tooling
- Add regression detection support

## Success Criteria

Phase 1 is complete when:

- workspace structure compiles with all three crates
- throughput benchmarks report MB/s and ops/s for both implementations
- latency benchmarks report percentile summaries
- sync mode comparisons run against both implementations
- feature overhead can be measured through feature flags
- concurrent writer behavior can be measured with repeatable commands

Phase 2 is complete when:

- wal-single-thread crate compiles and passes tests
- comparison benchmarks run successfully
- performance differences are documented

Phase 3 is complete when:

- results can be exported in machine-readable formats
- baseline management tooling is available
- regression detection is automated
