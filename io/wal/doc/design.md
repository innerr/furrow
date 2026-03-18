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

### 3. Write Flow

```
1. Check record size (reject oversized payloads)
2. Acquire lock, check if rotation needed
3. Allocate LSN atomically (fetch_add)
4. Create record with allocated LSN and encode
5. Write encoded bytes to file
6. Sync based on SyncMode policy
```

### 4. Record Lifecycle

Records progress through these states:
- **Written**: Record persisted to disk with valid CRC
- **Discardable**: Data has been consumed by downstream (marked via `truncate()`)
- **Truncated**: File deleted when all records are discardable

Corrupted/partial records at tail are automatically truncated during recovery.

### 5. Space Reclamation

- Caller invokes `truncate(lsn)` to mark records with `LSN <= lsn` as discarded
- When all records in a file are discarded, delete the entire file
- Active file is never deleted; if it needs truncation, rotate first
- No compact needed (data moves to destination, then WAL is discarded)

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
    pub max_file_size: u64,        // Default: 64MB
    pub max_file_age: Option<Duration>,
    
    // Sync policy
    pub sync_mode: SyncMode,
    
    // Preallocation
    pub preallocate_size: u64,     // Default: 64MB (currently unused)
    
    // Recovery
    pub create_if_missing: bool,   // Default: true (currently unused)
    pub recovery_mode: RecoveryMode,
}

pub enum SyncMode {
    Always,                        // fsync after every write
    Batch { bytes: u64, time: Duration }, // fsync after threshold
    Never,                         // rely on OS (not crash-safe)
}

pub enum RecoveryMode {
    TolerateTailCorruption,        // Skip corrupted tail records (default)
    Strict,                        // Error on any corruption
}
```

Compression and encryption are optional Cargo features:
```toml
[features]
compression = ["lz4_flex"]
encryption = ["aes-gcm", "rand"]
```

## Recovery Mechanism

### Scan Strategy

1. List all `wal.*.log` files, sort by seq number
2. For the last file:
   - Read header to get `base_lsn`
   - Scan records, validate CRC for each record
   - Track `last_valid_offset` where valid records end
   - Truncate file to `last_valid_offset` to remove corrupted tail
3. For each file:
   - Yield valid records in order
4. Caller processes records via `iter()` or `iter_ordered()`

### Recovery Behavior

| RecoveryMode | Behavior |
|--------------|----------|
| TolerateTailCorruption | Skip corrupted records at tail, truncate file (default) |
| Strict | Return error on any corruption |

### Recovery Info Returned

During recovery, the system returns:
- `last_lsn`: LSN of the last valid record (for `next_lsn` initialization)
- `last_valid_offset`: End offset of last valid record (for file truncation)
- `next_seq`: File sequence number to use for new files (`max_seq + 1`)

## API

### Async API

```rust
pub struct Wal { /* ... */ }

impl Wal {
    pub async fn open(config: WalConfig) -> Result<Self>;
    
    // Write returns LSN
    pub async fn write(&self, data: &[u8]) -> Result<u64>;
    
    // Mark records with LSN <= lsn as discardable
    // Note: uses <= boundary (LSN <= lsn are deleted)
    pub async fn truncate(&self, lsn: u64) -> Result<()>;
    
    // Manually trigger file rotation
    pub async fn rotate(&self) -> Result<()>;
    
    // Sync pending writes
    pub async fn sync(&self) -> Result<()>;
    
    // Create reader for recovery (passes recovery_mode from Wal)
    pub fn reader(&self) -> Result<WalReader>;
    
    pub async fn close(self) -> Result<()>;
}
```

### Sync API (blocking wrapper)

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
    // Iterate all committed records (may return errors)
    pub fn iter(&self) -> impl Iterator<Item = Result<Record>>;
    
    // Iterate records ordered by LSN
    // Returns first error immediately (fail-fast)
    pub fn iter_ordered(&self) -> impl Iterator<Item = Result<Record>>;
    
    // Collect and sort all records, return error on first failure
    pub fn read_ordered(&self) -> Result<Vec<Record>>;
}

pub struct Record {
    pub lsn: u64,
    pub data: Vec<u8>,
}
```

## Module Structure

```
io/wal/
├── mod.rs              // Public API exports
├── doc/
│   ├── design.md       // This document
│   ├── issues.md       // Known issues (historical)
│   └── issues-address.md // Fix plans and records
├── config.rs           // Configuration types
├── error.rs            // Error types
├── file.rs             // File management (rolling, header)
├── record.rs           // Record format (encoding/decoding)
├── allocator.rs        // Offset allocation (atomic)
├── writer.rs           // Platform selection (tokio vs uring)
├── writer_tokio.rs     // Async writer (tokio::fs)
├── writer_uring.rs     // Async writer (io_uring, Linux only)
├── reader.rs           // Sync reader for recovery
├── compress.rs         // Compression (optional feature)
├── encrypt.rs          // Encryption (optional feature)
└── uring_advanced.rs   // Advanced io_uring helpers (unused)
```

## Record Format

```
| Header (18 bytes) | Payload (variable) |
|-------------------|---------------------|
| len | crc | lsn | type | flags | data |
| 4B  | 4B  | 8B  | 1B   | 1B    | ...  |

- len: Payload length (u32 LE)
- crc: CRC32 of payload (u32 LE)
- lsn: Log sequence number (u64 LE)
- type: Full=0, First=1, Middle=2, Last=3
- flags: bit0=compressed, bit1=encrypted
```

## File Header Structure

```
| magic | version | seq | base_lsn | created_at | reserved |
| 4B    | 2B      | 4B  | 8B       | 8B         | 38B      |
Total: 64 bytes

- magic: "WAL1" (4 bytes)
- version: 1 (u16 LE)
- seq: File sequence number (u32 LE)
- base_lsn: Starting LSN for this file (u64 LE)
- created_at: Unix timestamp (u64 LE)
- reserved: For future use
```

## Truncate Behavior

`truncate(lsn)` deletes files where all records have `LSN <= lsn`:

1. Check active file's last LSN:
   - If `active_last_lsn <= lsn`, rotate first (close current file, create new one)
2. Scan all WAL files:
   - Skip the new active file
   - For each file, scan to find `last_lsn`
   - If `last_lsn <= lsn`, delete the file
3. Active file is never deleted without rotation first

**Important**: Boundary is `<=`, not `<`. A file with `last_lsn == truncate_lsn` will be deleted.

## Dependencies

| Dependency | Purpose | Required |
|------------|---------|----------|
| thiserror | Error types | Yes |
| crc32fast | CRC checksum | Yes |
| tokio | Async runtime | Yes |
| tokio-uring | io_uring (Linux) | Linux only |
| lz4_flex | Compression | Optional (feature) |
| aes-gcm | Encryption | Optional (feature) |
| rand | Nonce generation | Optional (encryption) |

## Testing

### Unit Tests
- Record encoding/decoding
- CRC validation
- LSN allocation atomicity
- Configuration validation
- Record size limits

### Integration Tests
- Write and read consistency
- File rolling by size/time
- Sync policy enforcement
- Truncate behavior (boundary `<=`)
- LSN persistence across restart
- Sequence number recovery
- Corrupted tail truncation

### Crash Recovery Tests
- Simulate crash during write (partial record)
- Partial records discarded at recovery
- Existing complete records preserved
- File truncated to last valid offset

### Concurrency Tests
- Multiple concurrent writers
- LSN strictly increasing, no duplicates
- No data corruption

### Performance Tests
- Sequential write throughput (tokio vs io_uring)
- Latency P50/P99
- Comparison across sync policies (Always/Batch/Never)
- Comparison across record sizes

## Known Issues and Fixes

See `issues.md` for historical issues and `issues-address.md` for detailed fix plans.

### Resolved Critical Issues

| Issue | Description | Fix |
|-------|-------------|-----|
| #1 | Linux backend syntax broken | Reimplemented `write()` method |
| #2 | LSN persisted as 0 | Allocate LSN before encoding |
| #3 | Recovery resets next_seq | Use `max_seq + 1` from recovered files |
| #4 | truncate() deletes active file | Rotate before deleting active file |
| #5 | Recovery doesn't truncate corrupt tail | Validate CRC, truncate to last valid offset |
| #6 | truncate() uses `<` instead of `<=` | Fixed boundary condition |
| #7 | Recovery mode ignored | Pass `recovery_mode` to reader |
| #8 | Missing directory fsync | Sync parent dir after file creation |
| #9 | Error messages leak memory | Use `String` instead of `&'static str` |

## Future Considerations

- Zero-copy API
- Direct I/O option
- Checkpoint integration
- Metrics/observability
- Hole tracking for sparse files
- Concurrent read during write
