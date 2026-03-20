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

---

## 11. Review Notes

This section captures review findings against the current repository state. The optimization direction is reasonable, but the current plan is not yet implementable as written.

### 11.1 Correctness Blockers

- The lock-free reservation flow needs a file generation or epoch in `PendingWrite`. Without it, a write that reserves an offset before rotation can later be submitted against the new active file and corrupt file layout.
- Rotation must be fenced with allocation state. Resetting allocator offset after swapping files is not sufficient when older reserved writes are still in flight.

### 11.2 Runtime and API Constraints

- The current `tokio_uring::fs::File` API is ownership-returning: `write_all_at` and `sync_all` consume the file handle and return a new one. The Phase 1 pseudo-code assumes shared read access to a stable file object, which does not match the current API shape.
- If true concurrent submissions are required, the implementation may need direct ownership of the raw `io_uring` ring on Linux rather than an incremental refactor around `tokio-uring`.

### 11.3 Scope Gaps in `uring_advanced.rs`

- `RegisteredFiles`, `LinkedOps`, and `BatchSubmit` are currently placeholders and test helpers, not integrated submission primitives.
- Phase 2 should be treated as new implementation work, not as a small wiring task over existing helpers.

### 11.4 Error Handling Gaps

- The batch submitter pseudo-code can report success after an earlier per-write failure because it sends `Ok(())` unconditionally during final drain.
- The fsync failure path is unspecified. The design needs explicit failure fanout semantics for both write and sync failures.

### 11.5 Evidence Quality

- The throughput tables in Sections 1.4 and 6 should be treated as hypotheses until backed by actual benchmark results from this repository.
- The current repository contains benchmark design documents, but not measured output that validates the projected 4-5x improvement.

### 11.6 Recommended Adjustments Before Implementation

- Add a benchmark and profiling baseline for the current `writer_uring.rs` implementation.
- Redesign the reservation model so every in-flight write is bound to a specific file generation.
- Decide explicitly whether advanced Linux optimization work will stay on `tokio-uring` or move to a raw `io-uring` execution path.

---

## 12. Revision Plan (Addressing Review Feedback)

This section documents the planned revisions to address the review findings in Section 11.

### 12.1 Technology Decision: Raw io-uring

**Decision**: Migrate to raw `io-uring` crate instead of `tokio-uring`.

**Rationale**:

1. `tokio-uring`'s ownership-returning API (`write_all_at` consumes and returns `File`) prevents concurrent submissions from multiple tasks.
2. Raw `io-uring` allows direct SQE manipulation for true batch submission.
3. Required for implementing `LinkedOps` (write + fsync chain) with `IOSQE_IO_LINK` flag.
4. Required for `RegisteredFiles` with `IORING_REGISTER_FILES`.

**Trade-offs**:

| Aspect | tokio-uring | raw io-uring |
|--------|-------------|--------------|
| Implementation complexity | Lower | Higher |
| Concurrent submissions | Not possible | Full support |
| Resource management | Automatic | Manual |
| Integration with tokio | Native | Requires bridge |

**Implementation Approach**:

- Create a dedicated `UringBackend` struct that owns the `io_uring::IoUring` instance.
- Use `tokio::task::spawn_blocking` for ring operations, or implement a custom reactor.
- Keep `tokio::fs` fallback for non-Linux platforms.

### 12.2 Revised Architecture: Generation-Based Reservation

#### 12.2.1 Data Structures

```rust
// New architecture using raw io-uring

struct ConcurrentInner {
    ring: io_uring::IoUring,
    allocator: Allocator,
    file_state: RwLock<FileState>,
    bytes_since_sync: AtomicU64,
    last_sync_time: AtomicU64,
    pending_writes: mpsc::Sender<PendingWrite>,
    shutdown_flag: AtomicBool,
}

struct FileState {
    fd: RawFd,                    // Raw fd, not File object
    generation: AtomicU64,        // Incremented on each rotation
    seq: u32,
    path: PathBuf,
    file_size: u64,
    is_registered: bool,          // Whether fd is registered with io_uring
}

struct PendingWrite {
    generation: u64,              // Bound to specific file generation
    lsn: u64,
    offset: u64,
    data: BytesMut,
    tx: oneshot::Sender<Result<()>>,
}
```

#### 12.2.2 Revised Write Flow

```
write(data) [caller task]
    │
    ├─→ 1. Snapshot current generation
    │       let gen = file_state.read().generation.load();
    │
    ├─→ 2. Atomic LSN + Offset allocation
    │       let lsn = allocator.allocate_lsn();
    │       let offset = allocator.allocate_offset(size);
    │
    ├─→ 3. Encode Record
    │       let data = record.encode();
    │
    ├─→ 4. Submit to queue with generation
    │       pending_writes.send(PendingWrite { generation: gen, ... });
    │
    └─→ 5. Await completion (optional)
            tx.await
```

#### 12.2.3 Generation Validation in Submitter

```rust
async fn batch_submitter(
    pending: mpsc::Receiver<PendingWrite>,
    inner: Arc<ConcurrentInner>,
) {
    const MAX_BATCH: usize = 64;
    let mut batch: Vec<PendingWrite> = Vec::with_capacity(MAX_BATCH);
    
    loop {
        // Collect batch with timeout
        while batch.len() < MAX_BATCH {
            match tokio::time::timeout(Duration::from_micros(100), pending.recv()).await {
                Ok(Some(write)) => batch.push(write),
                _ => break,
            }
        }
        
        if batch.is_empty() {
            if inner.shutdown_flag.load(Ordering::Relaxed) {
                break;
            }
            continue;
        }
        
        let state = inner.file_state.read().await;
        let current_gen = state.generation.load(Ordering::Acquire);
        
        // Track results for each write
        let mut results: Vec<Result<()>> = batch.iter().map(|_| Ok(())).collect();
        let mut valid_writes: Vec<(usize, &PendingWrite)> = Vec::new();
        
        // Validate generations and collect valid writes
        for (i, write) in batch.iter().enumerate() {
            if write.generation != current_gen {
                results[i] = Err(Error::StaleGeneration {
                    expected: current_gen,
                    actual: write.generation,
                });
            } else {
                valid_writes.push((i, write));
            }
        }
        
        // Submit valid writes to io_uring
        {
            let mut sq = inner.ring.submission();
            
            for (i, write) in &valid_writes {
                let sqe = io_uring::opcode::Write::new(
                    types::Fd(state.fd),
                    write.data.as_ptr(),
                    write.data.len() as u32,
                )
                .offset(write.offset as u64)
                .build();
                
                // Store index as user_data for completion matching
                let sqe = sqe.user_data(*i as u64);
                
                unsafe { sq.push(&sqe).expect("submission queue full"); }
            }
        }
        
        // Submit batch
        inner.ring.submit().expect("submit failed");
        
        // Wait for completions
        let mut cq = inner.ring.completion();
        for cqe in &mut cq {
            let idx = cqe.user_data() as usize;
            if cqe.result() < 0 {
                results[idx] = Err(Error::Io(std::io::Error::from_raw_os_error(-cqe.result())));
            }
        }
        
        // Batch fsync if needed
        if should_sync(&inner.sync_config) {
            let fsync_result = submit_fsync(&inner.ring, state.fd);
            if let Err(e) = fsync_result {
                // Fan out fsync failure to all successful writes
                for (i, result) in results.iter_mut().enumerate() {
                    if result.is_ok() {
                        *result = Err(Error::SyncFailed(e.to_string()));
                    }
                }
            }
        }
        
        // Notify all waiters
        for (write, result) in batch.drain(..).zip(results) {
            let _ = write.tx.send(result);
        }
    }
}
```

### 12.3 Revised Rotation Flow

```
rotate() [requires write lock]
    │
    ├─→ 1. Acquire write lock on file_state
    │
    ├─→ 2. Increment generation
    │       let old_gen = generation.fetch_add(1) + 1;
    │
    ├─→ 3. Drain pending writes with old generation
    │       // Wait for all in-flight writes with generation < old_gen
    │       // They will complete against old fd
    │
    ├─→ 4. Sync old file
    │       fsync(old_fd);
    │
    ├─→ 5. Create new file and update fd
    │       new_fd = open(new_path, ...);
    │       fd = new_fd;
    │
    ├─→ 6. Reset allocator offset
    │       allocator.set_offset(FILE_HEADER_SIZE);
    │
    ├─→ 7. Register new fd with io_uring (if using registered files)
    │       ring.submitter().register_files(&[fd])?;
    │
    └─→ 8. Release write lock
```

### 12.4 Error Handling Specification

#### 12.4.1 Error Types

```rust
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("stale generation: expected {expected}, got {actual}")]
    StaleGeneration { expected: u64, actual: u64 },
    
    #[error("sync failed: {0}")]
    SyncFailed(String),
    
    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),
    
    #[error("submission queue full")]
    QueueFull,
    
    #[error("WAL shutdown in progress")]
    ShuttingDown,
}
```

#### 12.4.2 Failure Fanout Semantics

| Failure Type | Affected Writes | Notification |
|--------------|-----------------|--------------|
| Generation mismatch | Single write | `Err(StaleGeneration)` |
| Write I/O error | Single write | `Err(Io(...))` |
| Fsync error | All writes in batch | `Err(SyncFailed)` |
| Queue full | All pending writes | `Err(QueueFull)` |
| Shutdown | All pending writes | `Err(ShuttingDown)` |

### 12.5 Performance Data Disclaimer

Sections 1.4 and 6 contain estimated throughput numbers. These are **hypotheses** that require validation:

```markdown
| Configuration | Current (estimated*) | Target (goal) | Improvement |
|---------------|----------------------|---------------|-------------|
| SyncMode::Never, 64B | ~200K ops/s | ~800K ops/s | 4x |
| SyncMode::Batch, 64B | ~100K ops/s | ~500K ops/s | 5x |
| SyncMode::Always, 64B | ~2K ops/s | ~10K ops/s | 5x |

*Estimated based on theoretical analysis. Baseline benchmark required.
```

**Action Required**: Run existing benchmarks to establish baseline before implementation.

### 12.6 Revised Implementation Timeline

| Phase | Tasks | Estimate | Notes |
|-------|-------|----------|-------|
| Phase 0 | Baseline benchmark | 1 day | Run existing benchmarks, document results |
| Phase 1 | Raw io-uring backend | 3-4 days | New `UringBackend` with direct ring access |
| Phase 2 | Generation-based reservation | 2 days | File generation tracking, validation |
| Phase 3 | Registered files integration | 1-2 days | `IORING_REGISTER_FILES` optimization |
| Phase 4 | Linked operations | 1 day | `IOSQE_IO_LINK` for write+sync |
| Phase 5 | Error handling + shutdown | 1-2 days | Proper fanout, graceful shutdown |
| Testing | Unit + benchmark + stress | 2 days | Compare before/after |
| **Total** | | **11-13 days** | |

### 12.7 Scope Clarification for uring_advanced.rs

The existing `uring_advanced.rs` contains helper structs but no integration code:

| Struct | Current State | Implementation Work Required |
|--------|---------------|------------------------------|
| `RegisteredFiles` | Placeholder with `Vec<Option<RawFd>>` | Implement `io_uring::register_files()` call |
| `LinkedOps` | Counter only | Implement SQE chain construction with `IOSQE_IO_LINK` |
| `BatchSubmit` | Threshold counter | Integrate with ring submission loop |

**Decision**: Treat as new implementation work, not simple wiring.

### 12.8 Open Questions for Next Review

1. **Ring size configuration**: Should `uring_queue_depth` be configurable, or fixed based on workload analysis?

2. **Buffer registration**: Should we implement `IORING_REGISTER_BUFFERS` for zero-copy I/O?

3. **Completion polling**: Use `IORING_SETUP_SQPOLL` for kernel-side submission polling, or stick to task-based submission?

4. **Cross-platform strategy**: How to share code between raw io-uring (Linux) and tokio::fs (other platforms)?

5. **Backpressure**: What should happen when `pending_writes` queue is full?
   - Block caller (current design)
   - Return error immediately
   - Drop oldest writes (not recommended for WAL)
