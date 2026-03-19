# WAL Benchmark Design for the Current Repository

## Overview

This document describes a benchmark plan that can be implemented in the current `furrow-io-wal` repository without assuming extra crates already exist.

The goal is to make benchmark work actionable today while reserving clear extension points for future comparison against `wal-single-thread`.

## Goals

- Measure steady-state write throughput for the current `wal` implementation.
- Measure per-write latency distribution under controlled conditions.
- Compare the impact of different `SyncMode` settings.
- Measure optional feature overhead for compression and encryption.
- Add a benchmark structure that can later compare `wal` and `wal-single-thread` without redesigning the suite.

## Non-Goals

- Do not assume that `wal-single-thread` already exists.
- Do not treat lifecycle operations such as temp directory creation, `Wal::open`, and `Wal::close` as part of steady-state throughput unless a benchmark explicitly targets lifecycle cost.
- Do not claim percentile latency from a harness that only records end-to-end batch timings.

## Scope

### Current Scope

The first implementation phase targets the current crate only:

- Throughput benchmark by write size.
- Latency benchmark with per-operation sampling.
- Sync mode comparison.
- Feature overhead comparison.
- Concurrent writer benchmark using the current `Wal` API.

### Planned Extension

When `wal-single-thread` is added, the benchmark suite should extend to:

- Compare `wal` and `wal-single-thread` with the same write sizes.
- Compare both implementations under the same sync policy matrix.
- Keep result naming stable so historical baselines remain comparable.

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

The benchmark layout should match the current single-crate repository:

```text
wal/
├── Cargo.toml
├── benches/
│   ├── common/
│   │   └── mod.rs
│   ├── throughput.rs
│   ├── latency.rs
│   ├── sync_modes.rs
│   ├── features.rs
│   └── concurrency.rs
└── doc/
    ├── benchmark-design.md
    └── benchmark-design-implementation.md
```

Future extension for `wal-single-thread`:

- shared benchmark helpers stay reusable
- implementation-specific adapters can be added later
- benchmark names should encode the implementation name explicitly

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
cargo bench --bench throughput
cargo bench --bench latency
cargo bench --bench sync_modes
cargo bench --bench concurrency
cargo bench --bench features --features compression,encryption
```

When `wal-single-thread` is added, comparison runs should keep the same benchmark names and add an implementation label rather than changing the whole report format.

## Implementation Plan

### Phase 1

- add benchmark targets under `benches/`
- add shared helpers in `benches/common/mod.rs`
- implement `throughput.rs`
- implement `latency.rs`
- implement `sync_modes.rs`
- implement `features.rs`
- implement `concurrency.rs`

### Phase 2

- add result export in a machine-readable format
- document baseline numbers for a reference environment

### Phase 3

- add `wal-single-thread` adapters
- add implementation comparison cases
- add stable baseline naming for regression tracking

## Success Criteria

Phase 1 is complete when:

- benchmark targets compile
- throughput benchmarks report MB/s and ops/s
- latency benchmarks report percentile summaries
- sync mode comparisons run against the current `wal` crate
- feature overhead can be measured through feature flags
- concurrent writer behavior can be measured with repeatable commands

Phase 3 is complete when:

- `wal` and `wal-single-thread` can be benchmarked by the same suite
- results can be compared case by case without renaming the benchmark matrix
