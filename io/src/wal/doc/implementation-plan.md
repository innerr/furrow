# WAL Implementation Plan

## Overview

Implement a high-performance WAL (Write-Ahead Log) module in Rust with focus on concurrent write optimizations.

## Implementation Phases

### Phase 1: Core Skeleton + Basic Write (MVP)

**Goal**: Working single-file WAL with tokio::fs (no io_uring yet)

**Modules**:
```
io/src/wal/
├── mod.rs           // Public API exports
├── config.rs        // WalConfig, SyncMode, etc.
├── error.rs         // Error types
├── record.rs        // Record encoding/decoding
├── file.rs          // File management (header, open/close)
├── allocator.rs     // Atomic LSN/Offset allocation
├── writer.rs        // Async writer (tokio::fs)
└── reader.rs        // Async reader (recovery)
```

**Tasks**:
- [ ] Add dependencies to Cargo.toml
- [ ] error.rs - Error types
- [ ] config.rs - Configuration types
- [ ] record.rs - Record format (Header + Payload)
- [ ] allocator.rs - Atomic LSN/Offset allocator
- [ ] file.rs - File management and header
- [ ] writer.rs - Async writer with tokio::fs
- [ ] reader.rs - Recovery reader
- [ ] mod.rs - Public API (Wal struct)
- [ ] Add sync API wrapper

**Record Format**:
```
Header (18 bytes):
| len(4) | crc(4) | lsn(8) | type(1) | flags(1) |

type: Full=0, First=1, Middle=2, Last=3
flags: bit0=compressed, bit1=encrypted
```

**File Header Format**:
```
| magic(4) | version(2) | seq(4) | base_lsn(8) | created_at(8) | reserved(38) |
Total: 64 bytes
```

**Unit Tests**:
- [ ] Record encode/decode
- [ ] CRC calculation/validation
- [ ] LSN allocation atomicity
- [ ] Config validation

---

### Phase 2: Rolling Files + Recovery

**Goal**: Support multiple file rotation and crash recovery

**Tasks**:
- [ ] file.rs - File rotation logic (size/time threshold)
- [ ] File naming: `wal.{seq:06}.log`
- [ ] Seal old file, activate new file
- [ ] truncate() - Mark records as discardable
- [ ] Delete files when all records discarded
- [ ] Recovery - Scan all files, CRC validation
- [ ] Skip PENDING/corrupted records

**Integration Tests**:
- [ ] Write → restart → read verification
- [ ] File rotation correctness
- [ ] Truncate behavior
- [ ] Recovery with corrupted records

---

### Phase 3: io_uring Optimization (Linux Only)

**Goal**: Replace tokio::fs with io_uring for true concurrent I/O

**Tasks**:
- [ ] Add tokio-uring dependency (target_os = "linux")
- [ ] Conditional compilation for writer
- [ ] Batch submission pattern
- [ ] Registered file descriptors
- [ ] Linked write + fsync
- [ ] Fallback to tokio::fs on non-Linux

**Performance Tests**:
- [ ] Throughput comparison (tokio::fs vs io_uring)
- [ ] Latency P50/P99
- [ ] Concurrent writer benchmark

---

### Phase 4: Compression/Encryption (Optional Features)

**Cargo.toml features**:
```toml
[features]
default = []
compression = ["lz4_flex"]
encryption = ["aes-gcm"]
```

**Tasks**:
- [ ] compress.rs - LZ4 compression (feature-gated)
- [ ] encrypt.rs - AES-GCM encryption (feature-gated)
- [ ] Update record flags handling
- [ ] Tests for compressed/encrypted records

---

## API Design

### Async API

```rust
pub struct Wal { /* ... */ }

impl Wal {
    pub async fn open(config: WalConfig) -> Result<Self>;
    pub async fn write(&self, data: &[u8]) -> Result<u64>;
    pub async fn truncate(&self, lsn: u64) -> Result<()>;
    pub async fn rotate(&self) -> Result<()>;
    pub async fn sync(&self) -> Result<()>;
    pub fn reader(&self) -> Result<WalReader>;
    pub async fn close(self) -> Result<()>;
}
```

### Sync API

```rust
pub struct WalSync { /* ... */ }

impl WalSync {
    pub fn open(config: WalConfig) -> Result<Self>;
    pub fn write(&self, data: &[u8]) -> Result<u64>;
    pub fn truncate(&self, lsn: u64) -> Result<()>;
    pub fn rotate(&self) -> Result<()>;
    pub fn sync(&self) -> Result<()>;
    pub fn reader(&self) -> Result<WalReader>;
    pub fn close(self) -> Result<()>;
}
```

### Recovery Reader

```rust
pub struct WalReader { /* ... */ }

impl WalReader {
    pub fn iter(&self) -> impl Iterator<Item = Result<Record>>;
    pub fn iter_ordered(&self) -> impl Iterator<Item = Result<Record>>;
}

pub struct Record {
    pub lsn: u64,
    pub data: Vec<u8>,
}
```

---

## Configuration

```rust
pub struct WalConfig {
    pub dir: PathBuf,
    pub max_file_size: u64,           // Default: 64MB
    pub max_file_age: Option<Duration>,
    pub sync_mode: SyncMode,
    pub preallocate_size: u64,        // Default: 64MB
    pub create_if_missing: bool,
    pub recovery_mode: RecoveryMode,
}

pub enum SyncMode {
    Always,                           // fsync after every write
    Batch { bytes: u64, time: Duration }, // fsync after threshold
    Never,                            // rely on OS (not crash-safe)
}

pub enum RecoveryMode {
    TolerateTailCorruption,           // Skip corrupted tail records
    Strict,                           // Error on any corruption
}
```

---

## Dependencies

| Crate | Purpose | Required |
|-------|---------|----------|
| tokio | Async runtime | Yes |
| thiserror | Error types | Yes |
| crc32fast | CRC checksum | Yes |
| bytes | Buffer management | Yes |
| tokio-uring | io_uring (Linux) | Phase 3 |
| lz4_flex | Compression | Optional |
| aes-gcm | Encryption | Optional |

---

## Testing Strategy

### Unit Tests
- Record encoding/decoding
- CRC calculation and validation
- LSN allocation atomicity
- Configuration validation

### Integration Tests
- Write and read consistency
- File rolling correctness
- Sync policy enforcement
- Truncate behavior
- Recovery after simulated crash

### Concurrency Tests
- Multiple concurrent writers
- LSN strictly increasing, no duplicates
- No data corruption

### Performance Tests
- Sequential write throughput
- Latency P50/P99/P99.9
- Comparison across sync policies
- Comparison across record sizes

---

## Progress Tracking

- Phase 1: [ ] 0%
- Phase 2: [ ] 0%
- Phase 3: [ ] 0%
- Phase 4: [ ] 0%

Last updated: 2026-03-18
