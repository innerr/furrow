# io_uring WAL High-Concurrency Write Optimization Plan

## 1. Current Problem Analysis

### 1.1 Architecture Overview

```
writer.rs (entry)
    └── Linux: writer_uring.rs (io_uring impl)
    └── Other: writer_tokio.rs (fallback)
```

### 1.2 Current Write Flow

```
write(data) [writer_uring.rs:148-202]
    │
    ├─→ 1. Acquire Mutex lock
    │
    ├─→ 2. Check if file rotation needed
    │
    ├─→ 3. Allocate LSN (atomic fetch_add)
    │
    ├─→ 4. Encode Record (header + data + CRC)
    │
    ├─→ 5. io_uring write_all_at(offset)
    │
    ├─→ 6. Sync based on SyncMode
    │       - Always: sync after each write
    │       - Batch: sync after bytes/time threshold
    │       - Never: no sync
    │
    └─→ 7. Release lock, return LSN
```

### 1.3 Performance Bottlenecks

| Issue | Location | Severity |
|-------|----------|----------|
| Global Mutex serialization | `writer_uring.rs:159` | Critical |
| Unused Allocator | `allocator.rs` all dead_code | Medium |
| Unused uring_advanced | `uring_advanced.rs` not integrated | Medium |
| No batch submission | Each write submits separately | Medium |

### 1.4 Current Performance Estimates

| SyncMode | Throughput | Bottleneck |
|----------|------------|------------|
| Always | 1-5K ops/s | fsync |
| Batch(4MB/100ms) | 50-200K ops/s | Mutex contention |
| Never | 200-500K ops/s | Mutex + encoding |

### 1.5 Gap Between Design and Implementation

The `research.md:134` design goal specifies "parallel writes", but current implementation uses Mutex for serialization.

Key unused features:
- `BatchSubmit` in `uring_advanced.rs:118-155`
- `LinkedOps` in `uring_advanced.rs:70-115`
- `RegisteredFiles` in `uring_advanced.rs:17-68`
- `Allocator` in `allocator.rs`

---

## 2. Optimization Goals

```
Current:  ~50-200K ops/s (Batch mode)
Target:   ~500K-1M ops/s (Batch mode, small writes)
```

---

## 3. Implementation Phases

### Phase 1: Lock-Free Concurrent Write Architecture

#### 1.1 New Data Structures

```rust
// writer_uring.rs

struct ConcurrentInner {
    // Atomic allocator (enable existing code)
    allocator: Allocator,
    
    // File state (RwLock, write lock only on rotate)
    file_state: RwLock<FileState>,
    
    // Sync state (atomic operations)
    bytes_since_sync: AtomicU64,
    last_sync_time: AtomicU64,
    
    // Batch submission queue (MPSC)
    pending_writes: mpsc::Sender<PendingWrite>,
}

struct FileState {
    file: tokio_uring::fs::File,
    fd: RawFd,
    seq: u32,
    path: PathBuf,
}

struct PendingWrite {
    lsn: u64,
    offset: u64,
    data: BytesMut,
    tx: oneshot::Sender<Result<()>>,
}
```

#### 1.2 Refactored Write Flow

```
write(data) [caller thread - no lock]
    │
    ├─→ 1. Atomic LSN + Offset allocation (lock-free)
    │       allocator.allocate_lsn()
    │       allocator.allocate_offset(size)
    │
    ├─→ 2. Encode Record (lock-free, CPU-bound)
    │       record.encode()
    │
    ├─→ 3. Submit to batch queue (lock-free)
    │       pending_writes.send(PendingWrite{...})
    │
    └─→ 4. Wait for completion (optional)
            tx.await
```

#### 1.3 Background Batch Submitter Task

```rust
async fn batch_submitter(
    pending: mpsc::Receiver<PendingWrite>,
    file_state: Arc<RwLock<FileState>>,
    sync_config: SyncConfig,
) {
    const MAX_BATCH: usize = 64;
    let mut batch = Vec::with_capacity(MAX_BATCH);
    
    loop {
        // Collect batch requests
        while batch.len() < MAX_BATCH {
            match tokio::time::timeout(
                Duration::from_micros(100), 
                pending.recv()
            ).await {
                Ok(Some(write)) => batch.push(write),
                _ => break,
            }
        }
        
        if batch.is_empty() { continue; }
        
        // Acquire read lock (mutually exclusive with rotate)
        let state = file_state.read().await;
        
        // Batch submit to io_uring
        for write in &batch {
            let (res, _) = state.file.write_all_at(&write.data, write.offset).await;
            if let Err(e) = res {
                write.tx.send(Err(e.into()));
            }
        }
        
        // Batch fsync if needed
        if should_sync(&sync_config) {
            let (res, _) = state.file.sync_all().await;
            // handle error...
        }
        
        // Notify completion
        for write in batch.drain(..) {
            write.tx.send(Ok(()));
        }
    }
}
```

### Phase 2: Integrate uring_advanced Optimizations

#### 2.1 Enable RegisteredFiles

```rust
struct ConcurrentInner {
    // ...
    registered_files: RegisteredFiles,
}

impl ConcurrentInner {
    fn register_file(&mut self, fd: RawFd) -> Result<()> {
        unsafe { self.registered_files.register(0, fd) }
        // Call io_uring register_files
    }
}
```

**Benefit**: Reduces kernel overhead per I/O operation.

#### 2.2 Enable LinkedOps (write + fsync chain)

```rust
// For SyncMode::Always scenarios
fn submit_with_sync(&self, write: PendingWrite) {
    let ops = LinkedOps::new()
        .add_write()
        .add_fsync();
    // Use IOSQE_IO_LINK flag for chained submission
}
```

**Benefit**: Atomic write+sync with single syscall.

#### 2.3 Enable BatchSubmit

```rust
let mut batcher = BatchSubmit::new(64);

for write in pending_writes {
    if batcher.add() {
        ring.submit()?;  // Single syscall for all I/Os
        batcher.clear();
    }
}
```

**Benefit**: Amortizes syscall overhead across multiple writes.

### Phase 3: Lock-Free File Rotation

#### 3.1 Double Buffer Strategy

```rust
struct FileState {
    active: Arc<tokio_uring::fs::File>,
    sealing: Option<Arc<tokio_uring::fs::File>>,  // Old file being synced
}
```

#### 3.2 Rotation Flow

```
rotate() [write lock]
    │
    ├─→ 1. Acquire write lock (blocks new write file access)
    │
    ├─→ 2. sealing = active
    │
    ├─→ 3. Create new active file
    │
    ├─→ 4. Reset allocator offset
    │
    └─→ 5. Release write lock
    
    Background: sync(sealing) -> close(sealing)
```

---

## 4. File Changes

| File | Changes |
|------|---------|
| `writer_uring.rs` | Refactor to lock-free architecture |
| `allocator.rs` | Remove dead_code, add `&self` methods |
| `uring_advanced.rs` | Integrate RegisteredFiles, LinkedOps, BatchSubmit |
| `config.rs` | Add `batch_size`, `uring_queue_depth` config |
| `lib.rs` | Conditional compilation for `uring-advanced` feature |

---

## 5. Configuration Additions

```rust
pub struct WalConfig {
    // Existing fields...
    
    // New fields for optimization
    pub batch_size: usize,           // Default: 64
    pub uring_queue_depth: u32,      // Default: 256
    pub max_pending_writes: usize,   // Default: 1024
}
```

---

## 6. Performance Estimates

| Configuration | Current | Optimized | Improvement |
|---------------|---------|-----------|-------------|
| SyncMode::Never, 64B | ~200K ops/s | ~800K ops/s | 4x |
| SyncMode::Batch, 64B | ~100K ops/s | ~500K ops/s | 5x |
| SyncMode::Always, 64B | ~2K ops/s | ~10K ops/s | 5x |

---

## 7. Risks and Compatibility

### 7.1 API Compatibility

- `Wal::write()` signature unchanged
- `WalSync::write()` signature unchanged
- Internal implementation only changes

### 7.2 Memory Management

- Batch queue may accumulate, need queue size limit
- Configure `max_pending_writes` to prevent OOM

### 7.3 Error Handling

- Batch submission failure must notify all waiters
- Partial failure handling in batch operations

### 7.4 Shutdown Process

- Must wait for all pending writes to complete
- Graceful shutdown sequence:
  1. Stop accepting new writes
  2. Drain pending queue
  3. Final sync
  4. Close files

---

## 8. Testing Strategy

### 8.1 Unit Tests

- Concurrent write correctness
- LSN ordering under high concurrency
- Rotation during active writes
- Error propagation in batch operations

### 8.2 Benchmarks

- Compare before/after throughput
- Various payload sizes (64B, 256B, 1KB, 4KB)
- Various concurrency levels (1, 4, 16, 64 threads)
- All SyncMode variants

### 8.3 Stress Tests

- Long-running stability
- Memory usage under load
- Recovery after crash simulation

---

## 9. Implementation Timeline

| Phase | Tasks | Estimate |
|-------|-------|----------|
| Phase 1 | Lock-free architecture | 2-3 days |
| Phase 2 | uring_advanced integration | 1-2 days |
| Phase 3 | Lock-free rotation | 1 day |
| Testing | Unit + benchmark + stress | 1-2 days |
| **Total** | | **5-8 days** |

---

## 10. References

- `research.md` - Original research and design goals
- `design.md` - WAL design document
- `uring_advanced.rs` - Advanced io_uring helpers (currently unused)
- `allocator.rs` - Atomic LSN/offset allocator (currently unused)
