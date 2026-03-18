# WAL Implementation Research

This document summarizes WAL implementation techniques from various projects, with focus on concurrent write optimizations.

## Table of Contents

1. [Concurrent WAL Write Patterns](#concurrent-wal-write-patterns)
2. [Project Implementations](#project-implementations)
3. [io_uring for Concurrent I/O](#io-uring-for-concurrent-io)
4. [fsync/fdatasync Behavior](#fsyncfdatasync-behavior)
5. [Compression for WAL](#compression-for-wal)
6. [CRC/Checksum Selection](#crcchecksum-selection)
7. [File Preallocation](#file-preallocation)
8. [Async Buffer Management](#async-buffer-management)
9. [Memory Ordering](#memory-ordering)
10. [Implementation Recommendations](#implementation-recommendations)

---

## Concurrent WAL Write Patterns

### Important Distinction: Group Commit vs Parallel Write

| Pattern | Description | WAL Write | Example Projects |
|---------|-------------|-----------|------------------|
| **Group Commit** | Batch multiple writes, serial WAL write | Serial | RocksDB, MySQL, PostgreSQL |
| **Parallel Write** | Multiple writers write WAL concurrently | Parallel | X-Engine, PolarDB, Aurora |

### Pattern 1: Group Commit (Batched Serial Write)

```
Writer 1 ─┐
Writer 2 ─┼──→ Leader serializes and writes WAL ──→ single fsync ──→ parallel MemTable
Writer 3 ─┘

Characteristics:
- WAL write is still SERIAL (single leader)
- fsync overhead amortized across batch
- 50-200x improvement vs per-write fsync
```

**Projects using Group Commit:**
- RocksDB
- MySQL InnoDB
- PostgreSQL
- SQLite

### Pattern 2: Parallel WAL Write (True Concurrency)

```
Writer 1 ──→ Reserve offset ──→ Parallel I/O ─┐
Writer 2 ──→ Reserve offset ──→ Parallel I/O ─┼──→ Concurrent writes
Writer 3 ──→ Reserve offset ──→ Parallel I/O ─┘

Characteristics:
- Multiple writers write to WAL simultaneously
- No serialization bottleneck
- Requires LSN-based ordering for recovery
```

**Projects with Parallel WAL:**

| Project | Approach | Key Technique |
|---------|----------|---------------|
| **X-Engine (Alibaba)** | Partition-based | Each subtable has independent WAL |
| **PolarDB (Alibaba)** | Parallel Redo | Multi-thread writes to different log regions |
| **Aurora (AWS)** | Storage-level parallelism | Log is data, storage nodes write in parallel |
| **Kafka** | Partition-level | Each partition has independent log |

#### X-Engine Parallel WAL

```
┌─────────────────────────────────────────────────┐
│                 X-Engine Architecture            │
├─────────────────────────────────────────────────┤
│                                                 │
│  Subtable 1 ──→ WAL 1 ──┐                       │
│  Subtable 2 ──→ WAL 2 ──┼──→ Parallel Flush     │
│  Subtable N ──→ WAL N ──┘                       │
│                                                 │
│  Each subtable has independent WAL              │
│  True concurrent WAL writes across subtables    │
└─────────────────────────────────────────────────┘
```

#### PolarDB Parallel Redo

```
Traditional MySQL: Single-thread redo log write
PolarDB:           Multi-thread parallel writes to different regions

┌──────────────────────────────────────────────────┐
│              Parallel Redo Log                    │
├──────────────────────────────────────────────────┤
│  Thread 1 ──→ [Region 1] ─┐                      │
│  Thread 2 ──→ [Region 2] ─┼──→ Parallel Commit   │
│  Thread N ──→ [Region N] ─┘                      │
│                                                  │
│  LSN distinguishes order                         │
│  Recovery replays by LSN ordering                │
└──────────────────────────────────────────────────┘
```

#### Our Design: io_uring Parallel Write

```
┌─────────────────────────────────────────────────────┐
│                   WAL Writer                         │
├─────────────────────────────────────────────────────┤
│                                                     │
│  Writer 1 ──┐                                       │
│  Writer 2 ──┼──→ Atomic LSN/Offset Allocation      │
│  Writer N ──┘           │                           │
│                         ▼                           │
│                ┌─────────────────┐                  │
│                │   io_uring SQ   │  Batch Submit    │
│                └────────┬────────┘                  │
│                         │                           │
│                         ▼                           │
│                ┌─────────────────┐                  │
│                │   WAL File(s)   │                  │
│                └─────────────────┘                  │
└─────────────────────────────────────────────────────┘

Key Differences from Group Commit:
- No leader election
- No serialization point
- I/O parallelism via io_uring
- Order maintained by LSN
```

### Comparison: Group Commit vs Our Design

| Aspect | Group Commit (RocksDB) | Our Design (io_uring) |
|--------|------------------------|----------------------|
| WAL Write | Serial (leader only) | Parallel (all writers) |
| Offset Allocation | Leader batch alloc | Atomic fetch_add |
| fsync | Single per batch | Batch or periodic |
| Bottleneck | Leader thread | None (lock-free) |
| Recovery | Sequential scan | Parallel or ordered |

---

## Project Implementations

### RocksDB WAL (Group Commit)

#### Core Architecture

RocksDB uses a **Write Thread** with state machine to manage concurrent writes:

```
Writer States:
- STATE_INIT: Initial state, waiting in JoinBatchGroup
- STATE_GROUP_LEADER: Leader of a write batch group
- STATE_MEMTABLE_WRITER_LEADER: Leader of memtable writer group
- STATE_PARALLEL_MEMTABLE_WRITER: Parallel memtable writer
- STATE_COMPLETED: Terminal state
```

#### Group Commit Implementation

```cpp
struct WriteGroup {
    Writer* leader = nullptr;
    Writer* last_writer = nullptr;
    SequenceNumber last_sequence;
    Status status;
    std::atomic<size_t> running;
    size_t size = 0;
};
```

**Key Points:**
- Maximum group size: 1MB
- RocksDB does NOT proactively delay writes to increase batch size
- WAL write is serial (leader only), MemTable write is parallel

#### Additional Optimizations

| Optimization | Description | Benefit |
|--------------|-------------|---------|
| **File Recycling** | `recycle_log_file_num = true` | Avoid metadata I/O |
| **Manual Flush** | `manual_wal_flush = true` | Reduce CPU overhead |
| **Non-Blocking Sync** | `SyncWAL()` | Don't block other writers |

#### Performance Reference

| Configuration | Throughput | Notes |
|--------------|------------|-------|
| sync=false | 500K-1M ops/s | Not crash-safe |
| sync=true + group commit | 50K-200K ops/s | Crash-safe |
| sync=true, no group commit | 1K-5K ops/s | fsync per write |

---

### SQLite WAL

#### Architecture

```
Three-file structure:
├── main.db          # Main database
├── main.db-wal      # WAL file (append-only)
└── main.db-shm      # WAL-Index (shared memory)

Concurrency Model: Multiple readers, single writer
```

#### WAL Frame Structure

```
┌─────────────┬──────────────────┐
│ Frame Header│   Page Data      │
│  (24 bytes) │  (page_size)     │
└─────────────┴──────────────────┘

Frame Header:
- pageOffset (4B): Page number
- dbSize (4B): Non-zero for commit frame
- salt1 (8B): From WAL header
- checksum (8B): Integrity check
```

#### Key Techniques

1. **End Mark Mechanism**: Each reader records last valid commit position
2. **WAL Reset**: Reuse WAL file instead of delete when all frames checkpointed
3. **Dual Header**: WAL-Index has two header copies for atomicity
4. **Frame Coalescing**: Same page modified multiple times in one transaction only records final version

#### Checkpoint Types

| Type | Behavior |
|------|----------|
| PASSIVE | Non-blocking, best effort |
| FULL | Wait for readers, then checkpoint |
| RESTART | Block until all readers use new WAL |
| TRUNCATE | Truncate WAL to zero |

---

### LMDB (Copy-on-Write B+Tree)

LMDB achieves crash safety **without WAL** through COW architecture.

#### Core Principle

```
Traditional WAL: Write log → Modify data → Checkpoint
LMDB COW:        Allocate new pages → Modify → Atomic root pointer switch
```

#### Dual Metadata Pages

```
┌─────────┬─────────┬──────────────────┐
│ Meta 0  │ Meta 1  │   Data Pages...  │
│ txnid=N │ txnid=N-1│                  │
└─────────┴─────────┴──────────────────┘

Commit: Alternately update two meta pages
Recovery: Choose meta page with larger txnid
```

#### MVCC Implementation

- **Reader Lock Table**: Shared memory tracking active read transactions
- **Snapshot Isolation**: Each read sees consistent view at transaction start
- **Lock-free Reads**: No locks for read operations

#### Space Reclamation (Freelist)

- B+ tree manages free pages
- Pages only reusable when older than oldest active reader
- Long transactions prevent space reclamation → file growth

---

### Bitcask (Log-Structured KV)

#### Architecture

```
Disk:  Append-only log files (write-once)
Memory: Keydir hash table (key → [file_id, offset, size])
```

#### Read/Write Flow

```
Write:
  1. Append to data file
  2. Update memory Keydir
  3. Return

Read:
  1. Lookup key in Keydir → [file_id, offset, size]
  2. Read value from file at offset (single seek)
```

#### Hint Files for Fast Startup

```
Purpose: Accelerate Keydir rebuild on startup
Content: key → [file_id, value_offset] (no value data)

Startup:
  1. Read all hint files → rebuild Keydir
  2. Scan only the last data file
```

#### Merge (Compaction)

Triggers:
- Fragmentation rate exceeds threshold
- Dead bytes exceed threshold

Process:
  1. Scan old files, identify valid data
  2. Write new merged file
  3. Generate hint file
  4. Atomic switch, delete old files

---

### MySQL InnoDB Redo Log

#### Group Commit Three Stages

```
Stage 1: FLUSH
  - Flush thread collects transactions
  - Write to log buffer
  - Update LSN

Stage 2: SYNC (serial, single thread)
  - Only one thread does actual fsync
  - Others wait on condition variable
  - Single fsync covers entire batch

Stage 3: COMMIT
  - Each thread commits independently
  - Can be parallelized with binlog group commit
```

**Key insight:** SYNC stage is serialized but very fast (single fsync for entire batch).

---

## io_uring for Concurrent I/O

### Rust Crate Selection

| Crate | Recommendation | Notes |
|-------|----------------|-------|
| **tokio-uring** | ⭐ Recommended | Tokio integration, type-safe |
| io-uring | Low-level | Manual resource management |
| rio | Not recommended | Potential use-after-free issues |

### Core Concepts

```
Submission Queue (SQ): Ring buffer of SQEs
Completion Queue (CQ): Ring buffer of CQEs

SQE: opcode, fd, addr, len, flags, user_data
CQE: user_data, res (result), flags
```

### Best Practices for WAL

#### 1. Batch Submission

```rust
const BATCH_SIZE: usize = 64;  // 32-128 recommended

for entry in entries {
    let sqe = prep_write(fd, entry.data, entry.offset);
    sq.push(&sqe)?;
}
ring.submit()?;  // Single syscall for all I/Os
```

#### 2. Registered Files

```rust
// Register file descriptors upfront
ring.submitter().register_files(&[wal_fd])?;

// Use index instead of fd
sqe.fd = types::Fixed(0);  // Index 0
```

**Benefit**: Reduces kernel overhead per I/O.

#### 3. Registered Buffers

```rust
#[repr(align(4096))]
struct AlignedBuffer { data: [u8; PAGE_SIZE] }

ring.submitter().register_buffers(&iovecs)?;
sqe.buf_index = buf_idx;
```

**Benefit**: Zero-copy I/O, no buffer mapping overhead.

#### 4. Linked Operations (Write + Fsync)

```rust
// Write with LINK flag
let write_sqe = prep_write(fd, data, offset);
write_sqe.flags |= IOSQE_IO_LINK;

// Fsync (only executes after write completes)
let fsync_sqe = prep_fsync(fd);

ring.submit()?;  // Chain submitted together
```

#### 5. Ring Size Recommendations

| Workload | SQ Size | CQ Size |
|----------|---------|---------|
| Low latency | 32-64 | 64-128 |
| High throughput | 256-512 | 512-1024 |
| Mixed | 128-256 | 256-512 |

#### 6. Error Handling

```rust
match cqe.result() {
    r if r >= 0 => { /* success */ }
    -125 => { /* ECANCELED: chain cancelled */ }
    e => { /* IO error: -e */ }
}
```

### Performance Comparison

| I/O Method | Throughput | Latency P99 |
|------------|------------|-------------|
| sync write | ~200K IOPS | ~50μs |
| libaio | ~400K IOPS | ~30μs |
| io_uring | ~800K+ IOPS | ~10μs |

---

## fsync/fdatasync Behavior

### Semantic Differences

| System Call | Flushes | Use Case |
|-------------|---------|----------|
| `fsync()` | Data + All metadata | Full durability |
| `fdatasync()` | Data + Essential metadata | Performance-sensitive |

### File System Differences

| FS | fsync Behavior | Notes |
|----|----------------|-------|
| **ext4** | Triggers journal commit | Default data=ordered |
| **XFS** | May flush large writeback | High throughput design |
| **Btrfs** | COW + checksum | May cause I/O storms |
| **ZFS** | Only writes to ZIL | Best for high fsync (add SLOG) |

### Common Pitfalls

#### 1. Directory Sync Required

```c
// Correct atomic file creation
fd = open("file.tmp", ...);
write(fd, data);
fsync(fd);           // Sync file
close(fd);
rename("file.tmp", "file.txt");
dirfd = open(dirname, ...);
fsync(dirfd);        // Sync directory! (often forgotten)
```

#### 2. Rename Trick

- `fsync(file)` does NOT guarantee directory entry is persisted
- Must `fsync(containing_directory)` for new files

### SSD and FUA

| Concept | Description |
|---------|-------------|
| **Write Barriers** | Replaced by FLUSH + FUA (kernel 2.6.37+) |
| **FUA** | Force Unit Access - ensures write to persistent media |
| **Consumer SSD Risk** | Some lie about data persistence |

### io_uring fsync

```rust
// fsync
io_uring_prep_fsync(sqe, fd, 0);

// fdatasync
io_uring_prep_fsync(sqe, fd, IORING_FSYNC_DATASYNC);

// Linked write + fsync
write_sqe.flags |= IOSQE_IO_LINK;
fsync_sqe;
```

---

## Compression for WAL

### Algorithm Comparison

| Metric | LZ4 | Snappy | Zstd (fast) | Zstd (default) |
|--------|-----|--------|-------------|----------------|
| **Compress** | 675 MB/s | 520 MB/s | 545 MB/s | 510 MB/s |
| **Decompress** | 3850 MB/s | 1500 MB/s | 1850 MB/s | 1550 MB/s |
| **Ratio** | 2.1x | 2.1x | 2.1-2.4x | 2.9x |
| **Latency** | ~μs | Low | Low | Medium |
| **Dictionary** | Yes | No | Yes |

### Rust Crate Recommendation

| Crate | Notes | Rating |
|-------|-------|--------|
| **lz4_flex** | Pure Rust, no unsafe | ⭐⭐⭐⭐⭐ |
| snap | Pure Rust, stable | ⭐⭐⭐⭐ |
| zstd | C binding, dictionary support | ⭐⭐⭐⭐⭐ |

### WAL Strategy

**Recommendation: LZ4 (lz4_flex)**
1. Fastest decompression - critical for recovery
2. Lowest compression overhead
3. Pure Rust - no FFI, memory safe

**Compression granularity:**
- Per-record: Low latency, low ratio
- **Batch (recommended)**: Better ratio, amortized overhead
- Trigger: Every 4-16KB or 1-5ms

**Dictionary compression (Zstd):**
- Train on 100-1000 typical WAL records
- Dictionary size: 8-64KB
- Small records: 2-3x better ratio

### CPU Overhead (100MB/s write rate, 3GHz CPU)

| Algorithm | Compress | Decompress | Total |
|-----------|----------|------------|-------|
| LZ4 | ~13% | ~2% | **~15%** |
| Snappy | ~18% | ~6% | ~24% |
| Zstd-fast | ~18% | ~6% | ~24% |

---

## CRC/Checksum Selection

### Algorithm Comparison

| Algorithm | Output | Speed | Hardware Accel | Rust Crate |
|-----------|--------|-------|----------------|------------|
| **CRC32** | 32-bit | ~30 GB/s | x86 SSE4.2, ARMv8 | `crc32fast` |
| CRC64 | 64-bit | ~2-5 GB/s | None | `crc64` |
| xxHash64 | 64-bit | ~12-20 GB/s | None | `twox-hash` |
| xxHash3 | 64/128-bit | ~20-40 GB/s | SIMD | `xxhash` |
| Blake3 | 256-bit | ~10-15 GB/s | AVX2/AVX-512 | `blake3` |

### Recommendation for WAL: CRC32 (crc32fast)

**Why:**
1. Hardware acceleration: SSE4.2 (2008+) and ARMv8
2. Performance: ~30 GB/s, essentially zero overhead
3. Detection: Excellent for bit-flip and burst errors
4. Compact: 4 bytes per record

### Implementation

```rust
use crc32fast::Hasher;

// Compute
let checksum = crc32fast::hash(&data);

// Verify
fn verify(data: &[u8], expected: u32) -> bool {
    crc32fast::hash(data) == expected
}
```

### Collision Trade-offs

- CRC32: 1 in 4 billion (acceptable for error detection)
- CRC64: 1 in 18 quintillion
- Blake3: Cryptographically negligible (for tamper-proofing)

---

## File Preallocation

### fallocate vs posix_fallocate

| Feature | `fallocate` (Linux) | `posix_fallocate` (POSIX) |
|---------|---------------------|---------------------------|
| Portability | Linux only | Cross-platform |
| Modes | Multiple (prealloc, punch hole, etc.) | Prealloc only |
| Fallback | Returns EOPNOTSUPP | May simulate with zero writes |

### Key Flags

| Flag | Purpose |
|------|---------|
| `FALLOC_FL_KEEP_SIZE` | Preallocate without changing logical size |
| `FALLOC_FL_PUNCH_HOLE` | Release blocks, create "hole" |
| `FALLOC_FL_ZERO_RANGE` | Zero range (metadata op) |

### Platform Differences

**Linux:**
```rust
use rustix::fs::{fallocate, FallocateFlags};
fallocate(fd, FallocateFlags::KEEP_SIZE, 0, 64 * 1024 * 1024)?;
```

**macOS:**
```rust
use nix::fcntl::posix_fallocate;
posix_fallocate(fd, 0, 64 * 1024 * 1024)?;
// Note: No PUNCH_HOLE equivalent
```

### WAL Strategy

1. **Preallocate at creation**: 64MB - 1GB
2. **Keep size**: Use `FALLOC_FL_KEEP_SIZE` for append-only
3. **Avoid frequent reallocation**: Realloc when remaining < 25%
4. **PUNCH_HOLE cautiously**: May trigger SSD GC

### SSD Considerations

- Preallocation reduces metadata writes (good)
- PUNCH_HOLE may increase write amplification via TRIM
- Align to SSD page size (4KB-16KB)

---

## Async Buffer Management

### Bytes vs Vec<u8> vs [u8]

| Type | Use Case | Clone Cost |
|------|----------|------------|
| `Bytes` | Cross async boundary | O(1) reference count |
| `BytesMut` | Building buffer | N/A (move) |
| `Vec<u8>` | Single-threaded | O(n) deep copy |
| `&[u8]` | Parsing | Zero (view) |

### Buffer Pool Pattern

```rust
use crossbeam_queue::ArrayQueue;
use bytes::BytesMut;

struct BufferPool {
    buffers: Arc<ArrayQueue<BytesMut>>,
}

impl BufferPool {
    fn acquire(&self) -> BytesMut {
        self.buffers.pop()
            .unwrap_or_else(|| BytesMut::with_capacity(SIZE))
    }
    
    fn release(&self, mut buf: BytesMut) {
        buf.clear();
        let _ = self.buffers.push(buf);
    }
}
```

### Zero-Copy Patterns

```rust
// Bytes slice - shared underlying storage
let data = Bytes::from("hello world");
let part1 = data.slice(0..5);  // Zero-copy

// BytesMut split
let mut buf = BytesMut::from("hello world");
let part1 = buf.split_to(5);  // buf = "world", part1 = "hello"
```

### Direct I/O Alignment

```rust
const ALIGNMENT: usize = 512;  // or 4096

// Requirements:
// - Buffer address aligned to block size
// - I/O size multiple of block size
// - File offset aligned
```

### Hot Path Optimization

```rust
// Reuse buffer, don't reallocate
struct Writer {
    buf: BytesMut,
}

impl Writer {
    fn write(&mut self, data: &[u8]) {
        self.buf.clear();  // Keep capacity
        self.buf.extend_from_slice(data);
    }
}
```

---

## Memory Ordering

### Ordering Types

| Ordering | Guarantee | Overhead |
|----------|-----------|----------|
| `Relaxed` | Atomic only, no ordering | Lowest |
| `Acquire` | Later ops can't reorder before | Low |
| `Release` | Earlier ops can't reorder after | Low |
| `AcqRel` | Acquire + Release | Medium |
| `SeqCst` | Global total order | Highest |

### Common Patterns

**Release-Acquire synchronization:**
```rust
// Producer
DATA.store(42, Ordering::Relaxed);
READY.store(true, Ordering::Release);

// Consumer
while !READY.load(Ordering::Acquire) {}
assert_eq!(DATA.load(Ordering::Relaxed), 42);
```

**SpinLock:**
```rust
impl SpinLock {
    fn lock(&self) {
        while self.locked.compare_exchange_weak(
            false, true,
            Ordering::Acquire,  // On success
            Ordering::Relaxed,  // On failure
        ).is_err() {
            std::hint::spin_loop();
        }
    }
    
    fn unlock(&self) {
        self.locked.store(false, Ordering::Release);
    }
}
```

**Atomic LSN Allocation:**
```rust
pub struct LSNAllocator {
    next_lsn: AtomicU64,
    next_offset: AtomicU64,
}

impl LSNAllocator {
    // Lock-free allocation
    pub fn allocate(&self, size: u64) -> (u64, u64) {
        let offset = self.next_offset.fetch_add(size, Ordering::Relaxed);
        let lsn = self.next_lsn.fetch_add(1, Ordering::Relaxed);
        (lsn, offset)
    }
}
```

### Performance (x86)

```
Relaxed ≈ Acquire ≈ Release < AcqRel < SeqCst
         (almost free)              (MFENCE needed)
```

### Pitfalls

```rust
// ❌ UB: Non-atomic data race
static mut DATA: i32 = 0;
DATA = 42;  // Non-atomic write
READY.store(true, Ordering::Release);

// ✅ Correct: Use atomic for all shared data
static DATA: AtomicI32 = AtomicI32::new(0);
```

**Key principles:**
1. Default to `SeqCst`, optimize later
2. Use `Release` (writer) + `Acquire` (reader) for synchronization
3. Use `Relaxed` for counters
4. **Never** mix atomic and non-atomic for shared data

---

## Implementation Recommendations

### Crate Selection

| Purpose | Crate | Reason |
|---------|-------|--------|
| Async I/O (Linux) | `tokio-uring` | Tokio integration, type-safe |
| Checksum | `crc32fast` | Hardware acceleration (~30 GB/s) |
| Compression | `lz4_flex` | Pure Rust, fastest decompression |
| Buffer | `bytes::BytesMut` | Pooling, zero-copy clone |
| Atomic ops | `std::sync::atomic` | Release/Acquire pattern |

### Core Design Decisions

```
Write Path:
  1. Atomic fetch_add for LSN/offset allocation (Relaxed)
  2. Prepare SQE for write
  3. Batch submit to io_uring
  4. Linked fsync (optional)

Sync Strategy:
  - Always: fsync after each write
  - Batch: fsync every N bytes or T ms
  - Never: rely on OS (not crash-safe)

Recovery:
  - Scan files, CRC32 validation
  - Skip PENDING records
  - Return parallel or ordered by LSN
```

### Implementation Priority

```
1. Core write path: Atomic LSN + append-only
2. Record format: Header (CRC + LSN + type) + Payload
3. Rolling files: Size/time threshold
4. Recovery: Sequential scan + CRC validation
5. io_uring integration: Replace tokio::fs
6. Group commit: Batch writes (optional)
7. Compression: Optional feature
8. Encryption: Optional feature
```

### Techniques to Adopt

1. **Atomic LSN Allocation**: Lock-free, no serialization
2. **io_uring Parallel I/O**: True concurrent writes
3. **Batch fsync**: Amortize fsync cost
4. **File Recycling**: Reduce metadata I/O
5. **CRC32 Validation**: Essential for integrity
6. **Configurable Sync Policy**: Balance safety and performance

### Techniques to Avoid

1. **Single-threaded write pipeline**: Bottleneck
2. **Per-record fsync**: Too slow
3. **Fixed-size records**: Wasteful for variable data
4. **Forgetting directory sync**: May lose files on crash
5. **Non-atomic data in concurrent access**: Undefined behavior

### Performance Targets

| Metric | Target | Notes |
|--------|--------|-------|
| Write throughput | 200K+ ops/s | With sync |
| Write latency P99 | < 10ms | With batch fsync |
| Recovery speed | 100MB/s+ | Depends on storage |
| CPU overhead | < 20% | Compression enabled |

---

## References

1. RocksDB Wiki - WAL: https://github.com/facebook/rocksdb/wiki/Write-Ahead-Log
2. SQLite WAL Documentation: https://www.sqlite.org/wal.html
3. LMDB Source: https://github.com/LMDB/lmdb
4. io_uring Documentation: https://kernel.dk/io_uring.pdf
5. Lord of the io_uring: https://unixism.net/loti/
6. X-Engine Paper: https://www.vldb.org/pvldb/vol12/p1731-wang.pdf
7. PolarDB Technical Overview: https://www.alibabacloud.com/blog/polardb

---

*Document version: 2.0*
*Last updated: 2026-03-18*
*Total lines: ~1000*
