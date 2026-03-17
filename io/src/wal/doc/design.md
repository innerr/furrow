# WAL Design Document

## Overview

A Write-Ahead Log (WAL) module for Rust, designed for high-throughput and low-latency concurrent writes.

## Core Design Decisions

### 1. Rolling Multiple Files

- Files are named `wal.{seq}.log` (e.g., `wal.000001.log`, `wal.000002.log`)
- Rolling triggers:
  - File size reaches threshold (`max_file_size`)
  - Time reaches threshold (`max_file_age`)
  - Manual trigger via `rotate()` API

### 2. Global Atomic LSN

- Single atomic counter for LSN allocation (lock-free)
- Each file header records `base_lsn` (starting LSN of that file)
- Recovery scans files in seq order

### 3. Two-Phase Allocation

```
1. Reserve: atomic fetch_add(record_size) → returns offset
2. Write: concurrent write at offset (state=PENDING)
3. Confirm: update state to COMMITTED (atomic write to header field)
```

### 4. Record Lifecycle

```
PENDING → COMMITTED → Discardable (consumed) → Truncated
```

- PENDING: Record reserved but not fully written
- COMMITTED: Record fully written and confirmed
- Discardable: Data has been written to destination
- Truncated: Space released, file may be deleted

### 5. Space Reclamation

- Caller invokes `truncate(lsn)` to mark records with LSN ≤ X as discarded
- When all records in a file are discarded, delete the entire file
- No compact needed (data moves to destination, then WAL is discarded)

### 6. Hole Handling

- Holes are created when PENDING records fail to complete
- Recovery: lazily skip PENDING records during scan
- Hole tracking stored in file header (pre-allocated, chainable if exceeds capacity)

## Concurrency Model

### Concurrent Writes with io_uring

```
Writer 1 → Reserve offset → Write (io_uring)
Writer 2 → Reserve offset → Write (io_uring)
Writer N → Reserve offset → Write (io_uring)
         ↓
    io_uring submits all writes concurrently
```

- No serialization points
- Each writer reserves space independently (atomic fetch_add)
- io_uring handles concurrent I/O submission

### File Handle Management

- Fixed number of active files (configurable)
- Old sealed files can be closed when no longer needed

## Configuration

```rust
pub struct WalConfig {
    pub dir: PathBuf,
    
    // Rolling policy
    pub max_file_size: u64,
    pub max_file_age: Option<Duration>,
    
    // Sync policy
    pub sync: SyncConfig,
    
    // Compression (optional)
    pub compression: Option<Compression>,
    
    // Encryption (optional)
    pub encryption: Option<Encryption>,
}

pub struct SyncConfig {
    pub mode: SyncMode,
    pub bytes_threshold: Option<u64>,
    pub time_threshold: Option<Duration>,
}

pub enum SyncMode {
    Always,    // fsync on every confirm
    Batch,     // batch fsync
    Never,     // rely on OS
}

pub enum Compression {
    Snappy,
    Zstd { level: u8 },
    Lz4,
}

pub struct Encryption {
    pub algorithm: EncryptionAlgo,
    pub key: Vec<u8>,
}

pub enum EncryptionAlgo {
    AesGcm,
    ChaCha20Poly1305,
}
```

## Recovery Mechanism

### Scan Strategy

1. List all `wal.*.log` files, sort by seq number
2. For each file:
   - Read header (base_lsn, hole bitmap, etc.)
   - Scan records, skip PENDING / holes
   - Yield valid COMMITTED records
3. Caller processes records (parallel or ordered, configurable)

### Recovery Options

| Strategy | Description | Use Case |
|----------|-------------|----------|
| Parallel | All records yielded concurrently | High throughput |
| Ordered by LSN | Sort records before yielding | Need strict ordering |

## API Sketch

```rust
pub struct Wal { /* ... */ }

impl Wal {
    pub async fn open(config: WalConfig) -> Result<Self>;
    
    // Write returns LSN
    pub async fn write(&self, data: &[u8]) -> Result<u64>;
    
    // Mark records with LSN <= lsn as discardable
    pub async fn truncate(&self, lsn: u64) -> Result<()>;
    
    // Manually trigger file rotation
    pub async fn rotate(&self) -> Result<()>;
    
    // Create reader for recovery
    pub fn reader(&self) -> Result<WalReader>;
    
    pub async fn close(self) -> Result<()>;
}

pub struct WalReader { /* ... */ }

impl WalReader {
    // Iterate all committed records
    pub fn iter(&self) -> impl Iterator<Item = Result<Record>>;
    
    // Iterate with ordering option
    pub fn iter_ordered(&self) -> impl Iterator<Item = Result<Record>>;
}

pub struct Record {
    pub lsn: u64,
    pub data: Vec<u8>,
}
```

## Module Structure

```
io/src/wal/
├── mod.rs           // Public API exports
├── doc/
│   └── design.md    // This document
├── config.rs        // Configuration types
├── error.rs         // Error types
├── file.rs          // File management (rolling, header)
├── record.rs        // Record format (encoding/decoding)
├── allocator.rs     // Offset allocation (atomic)
├── writer.rs        // Async writer (io_uring)
├── reader.rs        // Async reader
├── compress.rs      // Compression (optional feature)
└── encrypt.rs       // Encryption (optional feature)
```

## Record Format (TBD at Implementation)

```
| Header (fixed) | Payload (variable) |
|----------------|---------------------|
| len | crc | lsn | type | flags | data |
| 4B  | 4B  | 8B  | 1B   | 1B    | ...  |

type: Full / First / Middle / Last (for large records spanning multiple writes)
flags: bit0=compressed, bit1=encrypted
```

## File Header Structure (TBD at Implementation)

```
| magic | version | seq | base_lsn | hole_bitmap_offset | ... |
```

## Dependencies

| Dependency | Purpose | Required |
|------------|---------|----------|
| thiserror | Error types | Yes |
| crc32fast | CRC checksum | Yes |
| tokio | Async runtime | Yes |
| io-uring | Linux async I/O | Yes (Linux only) |
| snap / lz4 / zstd | Compression | Optional (feature) |
| aes-gcm / chacha20-poly1305 | Encryption | Optional (feature) |

## Testing Points

### Unit Tests
- Record encoding/decoding
- CRC validation
- LSN allocation atomicity
- Configuration validation

### Integration Tests
- Write and read consistency
- File rolling correctness
- Sync policy enforcement
- Truncate behavior

### Crash Tests
- Simulate crash during write
- Partial records discarded
- Existing complete records preserved

### Concurrency Tests
- Multiple writers concurrent write
- LSN strictly increasing, no duplicates
- No data corruption

### Performance Tests
- Throughput (sequential write)
- Latency (P50/P99)
- Comparison across sync policies
- Comparison across record sizes

## Future Considerations

- Zero-copy API
- Direct I/O option
- Checkpoint integration
- Metrics/observability
