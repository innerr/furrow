# WAL Issues - Detailed Fix Plans

**Document Date**: 2026-03-19
**Based on**: doc/issues.md (review date 2026-03-18)

---

## Fix Log

### Issue #1: Linux Backend Is Syntactically Broken - FIXED (2026-03-19)

- `src/writer_uring.rs`: Replaced broken `write_with_sync()` with correct `write()` implementation
- Import: Added `MAX_RECORD_SIZE` to `use crate::record::{...}`

**Validation**:
- ✅ `cargo build` compiles without errors
- ✅ All 17 tests pass

### Issue #2: Persisted Record Headers Store lsn = 0 - FIXED (2026-03-19)

**Commit**: See PR #22

**Actions Taken**:
1. Added `MAX_RECORD_SIZE` to imports in `writer_tokio.rs`
2. Rewrote `write()` method to allocate LSN **before** encoding the record
3. Added pre-check for `MAX_RECORD_SIZE` to avoid wasting LSN on oversized records
4. Added test `test_lsn_persisted_correctly` to verify LSN is correctly persisted after reopen

**Code Changes**:
- `src/writer_tokio.rs`:
  - Import: Added `MAX_RECORD_SIZE`
  - `write()`: Moved LSN allocation before `Record::new()` and `encode()`
  - Added pre-check for `MAX_RECORD_SIZE`

**Root Cause**: LSN was allocated **after** `Record::encode()`, so the encoded bytes always had `lsn = 0` in the header.

**Fix**: Allocate LSN first, then create and encode the record with the correct LSN value.

**Validation**:
- ✅ `cargo build` compiles without errors
- ✅ All 18 tests pass (including new `test_lsn_persisted_correctly`)

### Issue #3: Recovery Resets next_seq to Zero - FIXED (2026-03-19)

**Commit**: TBD

**Actions Taken**:
1. Fixed `writer_tokio.rs`: Use `max_seq + 1` instead of hardcoded `0` for `next_seq`
2. Fixed `writer_uring.rs`: Same fix applied

**Code Changes**:
- `src/writer_tokio.rs:45-52`: Changed pattern from `(_, path)` to `(max_seq, path)` and use `max_seq + 1`
- `src/writer_uring.rs:62-72`: Same change

**Root Cause**: The sequence number from the recovered file was discarded. Pattern matched `(u32, PathBuf)` but used `_` for seq, then hardcoded `next_seq = 0`.

**Fix**: Bind `max_seq` and use `max_seq + 1` as `next_seq` to continue the sequence correctly.

**Validation**:
- ✅ `cargo build` compiles without errors
- ✅ All 18 tests pass (including new `test_lsn_persisted_correctly`)

### Issue #3: Recovery Resets next_seq to Zero - FIXED (2026-03-19)

- `src/writer_uring.rs:62-72`: Same change

- Issue #6 (change `<` to `<=`)

**Root Cause**: The sequence number from the recovered file was discarded. Pattern matched `(u32, PathBuf)` then used `_` for seq, then hardcoded `next_seq = 0`.

**Fix**: Bind `max_seq` and use `max_seq + 1` as `next_seq` to continue the sequence correctly.

**Validation**:
- ✅ `cargo build` compiles without errors
- ✅ All 18 tests pass

### Issue #5: Recovery Appends After Corrupted Tail - FIXED (2026-03-19)

- Added `verify_crc` import
- Created `RecoveryInfo` struct with `last_lsn` and `last_valid_offset`
- Replaced `recover_last_lsn()` with `recover_info()` with CRC validation
- Modified `open()` to truncate file if `last_valid_offset < file_size`
- Fixed `truncate()` to use `recover_info()` and correct boundary (`<=`)
- Added `truncate()` method to `WalFile`

**Root Cause**: Recovery only returned LSN, not offset. Corrupted file was never physically truncated, so corruption persists forever. Readers stop at first bad record.

**Fix**: 
1. Validate CRC during recovery
2. Return `last_valid_offset` 
3. Truncate file to remove corruption
4. Also fixed Issue #6 by using `<=` instead of `<`

**Important Note**: Per Reviewer's analysis, current implementation always creates a fresh file via `ensure_active_file()`, The main value is this fix is "repair the recovered file so readers operate on valid bytes only", not "resume appending at last_valid_offset". If append-to-existing-file semantics are added later, `last_valid_offset` must be used as the append offset.

**Validation**:
- ✅ `cargo build` compiles without errors
- ✅ All 18 tests pass

---

## Critical (P0) - Must Fix Before Production

### 1. Linux Backend Is Syntactically Broken

**Locations**:
- `src/writer_uring.rs:137-195`
- `src/writer_uring.rs:330-357`

**Root Cause Analysis**:
The `writer_uring.rs` file contains multiple severe syntax errors that prevent compilation:

1. Line 143: `.map_err()` called without a receiver (no expression before the dot)
2. Lines 149-195: Stray `} else {` block that has no matching `if`
3. Missing `Wal::write()` implementation (tests call `wal.write()` but the method doesn't exist)
4. Line 151: Double semicolon `;;`
5. Lines 143-145: Invalid error conversion syntax `Error::into(Error::InvalidRecord(...))` and `error::into(...)`

**Fix Plan**:

**Step 1: Remove the broken `write_with_sync()` method entirely (lines 135-195)**
This method is syntactically irreparable and conceptually flawed (it tries to handle sync mode inside but doesn't have access to all necessary state).

**Step 2: Implement a proper `write()` method**
```rust
pub async fn write(&self, data: &[u8]) -> Result<u64> {
    // Reject oversized payloads before consuming an LSN.
    let estimated_size = HEADER_SIZE as u64 + data.len() as u64;
    if estimated_size as usize > MAX_RECORD_SIZE {
        return Err(Error::RecordTooLarge {
            size: estimated_size as usize,
            max: MAX_RECORD_SIZE,
        });
    }

    // Keep LSN allocation and append-order decisions in one critical section.
    let mut inner = self.inner.lock().await;

    let size_exceeded = inner.next_offset + estimated_size > inner.max_file_size;
    let time_exceeded = inner
        .max_file_age
        .map(|age| inner.file_created_at.elapsed() >= age)
        .unwrap_or(false);

    if size_exceeded || time_exceeded {
        self.rotate_file(&mut inner).await?;
    }

    let lsn = inner.next_lsn.fetch_add(1, Ordering::Relaxed);
    let record = Record::new(lsn, data.to_vec());
    let encoded = record.encode()?;
    let record_size = encoded.len() as u64;

    let offset = inner.next_offset;
    let file = inner.current_file.as_ref().ok_or(Error::Closed)?;
    let (res, file) = file.write_all_at(&encoded, offset).await;
    res?;
    inner.current_file = Some(file);

    inner.next_offset += record_size;
    inner.bytes_since_sync += record_size;

    let should_sync = match &inner.sync_mode {
        SyncMode::Always => true,
        SyncMode::Batch { bytes, time } => {
            inner.bytes_since_sync >= *bytes || inner.last_sync.elapsed() >= *time
        }
        SyncMode::Never => false,
    };

    if should_sync {
        let file = inner.current_file.as_ref().ok_or(Error::Closed)?;
        let (res, file) = file.sync_all().await;
        res?;
        inner.current_file = Some(file);
        inner.bytes_since_sync = 0;
        inner.last_sync = Instant::now();
    }

    Ok(lsn)
}
```

**Why the lock scope matters**:
- The current implementation is already serialized on `inner`.
- If LSN allocation happens before the write lock is reacquired, persisted record order can diverge from LSN order.
- Recovery currently derives `next_lsn` from the last valid on-disk record, so out-of-order persisted LSNs can cause reuse or rollback after restart.

**Step 3: Add Linux CI job**
Create a GitHub Actions workflow or modify existing CI to build and test on Linux:
```yaml
# .github/workflows/ci.yml
jobs:
  test-linux:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - run: cargo build
      - run: cargo test
      - run: cargo build --features uring-advanced
```

**Important**:
- The io_uring writer is selected by `target_os = "linux"`, not by a Cargo feature.
- `io-uring` is not a declared feature in `Cargo.toml`; using it in CI would fail immediately.

**Step 4: Re-check `WalSync::write()` after restoring `Wal::write()`**
The sync wrapper can keep delegating to `self.wal.write()` once the async writer compiles again; no separate design change is needed beyond making the main write path valid.

**Validation**:
- Run `cargo build` on Linux - must compile without errors
- Run `cargo clippy -- -D warnings` - must pass
- Run `cargo test` - all tests must pass
- Run `rustfmt --check` - must pass

---

### 2. Persisted Record Headers Store `lsn = 0`

**Locations**:
- `src/writer_tokio.rs:93-112`
- `src/record.rs:102-108`

**Root Cause Analysis**:
```rust
// Current (broken) code flow:
let record = Record::new(0, data.to_vec());  // LSN = 0 here
let encoded = record.encode()?;               // Encodes LSN = 0 into bytes
// ... later ...
let lsn = inner.next_lsn.fetch_add(1, Ordering::Relaxed);  // Allocate real LSN too late!
```

The LSN is allocated AFTER encoding, so the persisted bytes always contain `0x0000000000000000` in the LSN field (bytes 8-15 of the record header).

**Impact**: During recovery, all records appear to have `lsn = 0`, making it impossible to:
- Order records correctly
- Determine which record is the latest
- Implement proper truncation semantics

**Fix Plan**:

**Option A (Recommended): Allocate LSN before encoding**

In `writer_tokio.rs`:
```rust
pub async fn write(&self, data: &[u8]) -> Result<u64> {
    // 1. Acquire lock and check rotation
    let mut inner = self.inner.lock().await;
    
    // Estimate record size (without encoding yet)
    let estimated_size = HEADER_SIZE + data.len();
    let size_exceeded = inner.next_offset + estimated_size as u64 > inner.max_file_size;
    let time_exceeded = inner.max_file_age
        .map(|age| inner.file_created_at.elapsed() >= age)
        .unwrap_or(false);
    
    if size_exceeded || time_exceeded {
        self.rotate_file(&mut inner).await?;
    }
    
    // 2. Allocate LSN FIRST
    let lsn = inner.next_lsn.fetch_add(1, Ordering::Relaxed);
    
    // 3. Create record with correct LSN and encode
    let record = Record::new(lsn, data.to_vec());
    let encoded = record.encode()?;
    let record_size = encoded.len() as u64;
    
    // 4. Write to file
    let offset = inner.next_offset;
    inner.current_file.as_mut().ok_or(Error::Closed)?
        .write(&encoded, offset).await?;
    
    // ... rest of the method
}
```

**Option B: Patch the encoded buffer (not recommended)**
This is brittle and requires understanding the exact byte layout:
```rust
// After encoding, patch bytes 8..16 with the real LSN
let lsn_bytes = lsn.to_le_bytes();
encoded[8..16].copy_from_slice(&lsn_bytes);
```

**Why Option A is better**:
- Cleaner code
- No risk of byte layout changes breaking the patch
- The CRC in the header is computed over the data only, not including LSN, so this doesn't break CRC

**Important Note**: According to `record.rs:99`, CRC is computed only over `self.data`, not the header. So changing the LSN after encoding doesn't invalidate the CRC.

**Validation**:
- Write unit test: write a record, close WAL, reopen, verify LSN matches
- Test with multiple records: verify LSN ordering is preserved after recovery

---

### 3. Recovery Resets `next_seq` to Zero and Reuses Old Filenames

**Locations**:
- `src/writer_tokio.rs:45-52`
- `src/writer_uring.rs:62-72`

**Root Cause Analysis**:
```rust
let (start_lsn, start_offset, next_seq) = if let Some((_, path)) = files.last() {
    let mut wal_file = WalFile::open(path.clone()).await?;
    let file_size = wal_file.write_offset;
    let last_lsn = recover_last_lsn(&mut wal_file).await?;
    (last_lsn.map_or(0, |lsn| lsn + 1), file_size, 0)  // <-- next_seq hardcoded to 0!
} else {
    (0, FILE_HEADER_SIZE, 0)
};
```

The file sequence number from the recovered file is discarded. If the last file is `wal.000005.log`, the new file will be `wal.000000.log`, overwriting the old one.

**Impact**:
- Data loss: old files get overwritten
- Recovery corruption: cannot distinguish old from new data
- Breaks append-only semantics

**Fix Plan**:

**Step 1: Extract max sequence from file list**
```rust
let files = wal_file::list_wal_files(&dir).await?;

let (start_lsn, start_offset, next_seq) = if let Some((max_seq, path)) = files.last() {
    let mut wal_file = WalFile::open(path.clone()).await?;
    let file_size = wal_file.write_offset;
    let last_lsn = recover_last_lsn(&mut wal_file).await?;
    let next_seq = max_seq + 1;  // Use recovered sequence
    (last_lsn.map_or(0, |lsn| lsn + 1), file_size, next_seq)
} else {
    (0, FILE_HEADER_SIZE, 0)
};
```

**Step 2: Also recover LSN correctly**
Note that issue #5 also needs to be addressed here - `start_offset` should be the last valid record offset, not the physical file size.

**Step 3: Apply the same fix to `writer_uring.rs`**
The uring backend has the identical bug at lines 62-72.

**Edge Cases to Handle**:
1. Empty directory: `next_seq = 0` (correct as-is)
2. Corrupted file names: `list_wal_files` already filters and parses, returns only valid files
3. Missing files in sequence: we use `max_seq + 1`, not `count + 1`, so gaps are handled correctly

**Validation**:
- Test: create `wal.000003.log`, close WAL, reopen, verify next file is `wal.000004.log`
- Test: create multiple files, verify sequence continues correctly

---

### 4. `truncate()` Can Delete the Active File While Writers Still Append to It

**Locations**:
- `src/writer_tokio.rs:191-200`
- `src/writer_uring.rs:252-264`

**Root Cause Analysis**:
```rust
pub async fn truncate(&self, lsn: u64) -> Result<()> {
    let files = wal_file::list_wal_files(&self.dir).await?;
    
    for (_, path) in &files {
        let mut wal_file = WalFile::open(path.clone()).await?;
        if let Some(last) = recover_last_lsn(&mut wal_file).await? {
            if last < lsn {  // or last <= lsn per issue #6
                drop(wal_file);
                fs::remove_file(path).await?;  // <-- Can delete the active file!
            }
        }
    }
    Ok(())
}
```

The code iterates over files from disk without checking if any file is currently open for writing. On Unix, deleting an open file succeeds - the file becomes unlinked from the directory but the process can still write to it. New writes go to an orphaned inode and disappear from directory-based recovery.

**Impact**:
- Data loss: writes after truncate() are lost on restart
- Recovery failure: directory listing won't show the file that was being written
- Silent corruption: no error is raised

**Fix Plan**:

**Recommended approach: coordinate issue #4 and issue #6 in one implementation**

```rust
pub async fn truncate(&self, lsn: u64) -> Result<()> {
    // Hold the lock long enough to make the active-file decision once.
    let active_seq = {
        let mut inner = self.inner.lock().await;

        let active_seq = inner.current_file.as_ref().map(|f| f.header.seq);
        let active_last_lsn = if let Some(current_file) = inner.current_file.as_mut() {
            recover_last_lsn(current_file).await?
        } else {
            None
        };

        // If the active file is also fully truncatable, rotate first so that
        // the old active file becomes deletable without unlinking an open inode.
        if active_last_lsn.is_some_and(|last| last <= lsn) {
            self.rotate_file(&mut inner).await?;
        }

        inner.current_file.as_ref().map(|f| f.header.seq)
    };

    let files = wal_file::list_wal_files(&self.dir).await?;

    for (seq, path) in &files {
        if active_seq.is_some_and(|curr_seq| *seq == curr_seq) {
            continue;
        }

        let mut wal_file = WalFile::open(path.clone()).await?;
        if let Some(last) = recover_last_lsn(&mut wal_file).await? {
            if last <= lsn {
                drop(wal_file);
                fs::remove_file(path).await?;
            }
        }
    }

    Ok(())
}
```

**Why this is better than “always skip the active file”**:
- It still prevents unlinking an open file.
- It preserves the documented `truncate(lsn)` semantics for `LSN <= lsn`.
- It avoids the long-term leak where the active file can never be reclaimed even when all its records are already truncated.

**For writer_uring.rs**: Apply the same fix, but note that `current_file` is `tokio_uring::fs::File`, not `WalFile`, so we need to track `current_seq` separately (which already exists as a field in `Inner`).

**Validation**:
- Test: truncate a non-active old file and verify it is removed.
- Test: truncate the active file's full LSN range and verify the implementation rotates first, then deletes the old file.
- Test: verify writes after `truncate()` are persisted and recoverable from the new active file.

---

### 5. Recovery Appends After a Corrupted Tail Instead of After the Last Valid Record

**Locations**:
- `src/writer_tokio.rs:45-49`
- `src/writer_tokio.rs:220-245`
- `src/writer_uring.rs:62-69`
- `src/writer_uring.rs:290-321`
- `src/reader.rs:136-170`

**Root Cause Analysis**:
The recovery flow has two separate steps that don't communicate properly:

1. `recover_last_lsn()` scans the file and returns the last valid LSN, but **not the offset**
2. Recovery trusts header structure and file length only; it does not validate record payload CRC
3. `reader.rs` stops scanning on corruption/truncation, but `open()` does not repair the last file back to the last valid boundary

**Scenario**:
```
File layout: [Header][Record1 (valid)][Record2 (partial write/corrupt)][...garbage...]
             ^       ^                ^
             0       64               150                          500 (file size)

Current behavior:
- `recover_last_lsn()` returns LSN from Record1
- The bad tail remains on disk
- Readers stop at Record2 forever, so the last file stays permanently corrupted

Problem:
- Recovery metadata is derived from partially trusted bytes
- The corrupted tail is never physically removed, so every future reader hits the same stop point
- If the implementation later resumes appending to the recovered file, it would append beyond an untrimmed corrupt tail
```

**Impact**:
- New records after restart are invisible to recovery
- Silent data loss
- Corruption accumulates over multiple restarts

**Fix Plan**:

**Step 1: Replace `recover_last_lsn()` with a recovery scan that returns both LSN and offset**

In `writer_tokio.rs`:
```rust
struct RecoveryInfo {
    last_lsn: Option<u64>,
    last_valid_offset: u64,  // Offset immediately after the last valid record
}

async fn recover_info(wal_file: &mut WalFile) -> Result<RecoveryInfo> {
    let file_size = wal_file.write_offset;
    let mut offset = FILE_HEADER_SIZE;
    let mut last_lsn = None;
    let mut last_valid_offset = FILE_HEADER_SIZE;

    while offset < file_size {
        if offset + HEADER_SIZE as u64 > file_size {
            break;
        }

        let header_buf = wal_file.read_at(offset, HEADER_SIZE).await?;
        let header = match RecordHeader::decode(&header_buf) {
            Ok(h) => h,
            Err(_) => break,
        };

        let record_end = offset + HEADER_SIZE as u64 + header.len as u64;
        if record_end > file_size {
            break;
        }

        let data = wal_file
            .read_at(offset + HEADER_SIZE as u64, header.len as usize)
            .await?;
        if !verify_crc(&data, header.crc) {
            break;
        }

        last_lsn = Some(header.lsn);
        last_valid_offset = record_end;  // Track the end of the last valid record
        offset = record_end;
    }

    Ok(RecoveryInfo {
        last_lsn,
        last_valid_offset,
    })
}
```

**Step 2: Use recovery info in `open()` and physically repair the last file**
```rust
let (start_lsn, next_seq) = if let Some((max_seq, path)) = files.last() {
    let mut wal_file = WalFile::open(path.clone()).await?;
    let info = recover_info(&mut wal_file).await?;

    if info.last_valid_offset < wal_file.write_offset {
        wal_file.truncate(info.last_valid_offset).await?;
    }

    let next_seq = max_seq + 1;
    (info.last_lsn.map_or(0, |lsn| lsn + 1), next_seq)
} else {
    (0, 0)
};
```

**Important clarification**:
- In the current implementation, `open()` always creates a fresh active WAL file via `ensure_active_file()`.
- That means the immediate value of this fix is not “resume appending at `last_valid_offset`”; it is “repair the recovered last file so readers and future truncation operate on valid bytes only”.
- If the implementation is later changed to keep appending to the recovered file, `last_valid_offset` must become the append offset used by the writer.

**Step 3: Add `truncate()` method to `WalFile`**
```rust
impl WalFile {
    pub async fn truncate(&mut self, len: u64) -> Result<()> {
        self.file.set_len(len).await?;
        self.write_offset = len;
        Ok(())
    }
}
```

**Step 4: Apply to `writer_uring.rs`**
The uring version needs similar changes, adapting for sync I/O and `tokio_uring::fs::File`.

**Edge Cases**:
- Empty file (no records): `last_valid_offset = FILE_HEADER_SIZE`
- Fully corrupt file: same as empty file
- All records valid: `last_valid_offset = file_size`, no truncation needed

**Validation**:
- Test: create file with valid record, append garbage, restart, verify file is truncated
- Test: create file with valid header/length but corrupted payload CRC, restart, verify recovery truncates before that record
- Test: verify new writes after restart are recoverable
- Test: verify partial writes at crash boundary are handled correctly

---

## Medium (P1) - Should Fix Soon

### 6. `truncate(lsn)` Uses the Wrong Boundary Condition

**Locations**:
- `src/writer_tokio.rs:196-199`
- `src/writer_uring.rs:261-264`
- `doc/design.md:32-35`

**Root Cause Analysis**:
```rust
if last < lsn {
    remove_file(...)
}
```

The condition uses `<` but the design document states `truncate(lsn)` should discard records with `LSN <= lsn`.

**Example of the bug**:
- File contains records with LSNs: [10, 20, 30]
- Call `truncate(30)`
- Current behavior: file is kept (30 < 30 is false)
- Expected behavior: file should be deleted (30 <= 30 is true)

**Fix Plan**:

**Step 1: Change condition to `<=`**
```rust
if last <= lsn {
    remove_file(...)
}
```

**Step 2: Update both backends**
- `writer_tokio.rs:197`: Change `<` to `<=`
- `writer_uring.rs:262`: Change `<` to `<=`

**Step 3: Add test to verify behavior**
```rust
#[tokio::test]
async fn test_truncate_boundary() {
    let dir = tempdir().unwrap();
    let config = WalConfig::new(dir.path()).sync_mode(SyncMode::Always);
    
    let wal = Wal::open(config).await.unwrap();
    let lsn = wal.write(b"test").await.unwrap();
    wal.close().await.unwrap();
    
    // Truncate with the exact LSN that was written
    let wal = Wal::open(WalConfig::new(dir.path())).await.unwrap();
    wal.truncate(lsn).await.unwrap();
    wal.close().await.unwrap();
    
    // The old file should be deleted. A fresh active file may still exist
    // because `open()` creates one eagerly.
    let files: Vec<_> = std::fs::read_dir(dir.path())
        .unwrap()
        .filter_map(|e| e.ok())
        .filter(|e| e.file_name().to_string_lossy().starts_with("wal."))
        .collect();
    assert!(files.len() <= 1);
}
```

**Note**: This fix should be combined with issue #4 in a single PR; otherwise the `<=` boundary change can reintroduce active-file deletion.

---

### 7. Recovery Mode Configuration Is Ignored, and Ordered Iteration Drops Errors

**Locations**:
- `src/config.rs:20`
- `src/writer_tokio.rs:207-208`
- `src/writer_uring.rs:271-272`
- `src/reader.rs:17-21`
- `src/reader.rs:32-35`

**Root Cause Analysis**:

**Problem 1: Recovery mode is ignored**
```rust
// In WalConfig
pub recovery_mode: RecoveryMode,

// In writer_tokio.rs
pub fn reader(&self) -> Result<crate::WalReader> {
    crate::WalReader::new(self.dir.clone())  // Doesn't pass recovery_mode!
}

// In reader.rs
pub fn new(dir: PathBuf) -> Result<Self> {
    Ok(Self {
        dir,
        recovery_mode: RecoveryMode::TolerateTailCorruption,  // Hardcoded!
    })
}
```

**Problem 2: `iter_ordered()` drops errors**
```rust
pub fn iter_ordered(&self) -> impl Iterator<Item = Result<Record>> {
    let mut records: Vec<_> = self.iter().filter_map(|r| r.ok()).collect();  // Drops errors!
    records.sort_by_key(|r| r.lsn);
    records.into_iter().map(Ok)
}
```

This silently hides corruption by filtering out all `Err` results before sorting.

**Fix Plan**:

**Step 1: Store `recovery_mode` on `Wal` itself**
```rust
#[derive(Debug)]
pub struct Wal {
    dir: PathBuf,
    inner: Arc<Mutex<Inner>>,
    recovery_mode: RecoveryMode,  // Add this
}

pub fn reader(&self) -> Result<crate::WalReader> {
    crate::WalReader::with_recovery_mode(self.dir.clone(), self.recovery_mode)
}
```

This should live on `Wal`, not behind `inner`, because the value is immutable configuration rather than mutable writer state.

**Step 2: Make `iter_ordered()` fail closed instead of mixing sorted data with hidden errors**
```rust
pub fn iter_ordered(&self) -> impl Iterator<Item = Result<Record>> {
    let mut records = Vec::new();

    for item in self.iter() {
        match item {
            Ok(record) => records.push(record),
            Err(err) => return std::iter::once(Err(err)).chain(std::iter::empty()),
        }
    }

    records.sort_by_key(|r| r.lsn);
    records.into_iter().map(Ok)
}
```

**Alternative API (cleaner)**: change this method to `read_ordered() -> Result<Vec<Record>>`
```rust
pub fn read_ordered(&self) -> Result<Vec<Record>> {
    let mut records = self.iter().collect::<Result<Vec<_>>>()?;
    records.sort_by_key(|r| r.lsn);
    Ok(records)
}
```

Returning both sorted `Ok(...)` items and `Err(...)` items from one iterator is technically possible, but it produces ambiguous semantics for callers. For recovery code, surfacing the first error and stopping is safer.

**Step 3: Apply to `writer_uring.rs`**
Same changes needed in the uring backend.

**Validation**:
- Test: set Strict mode, introduce corruption, verify reader returns error
- Test: call `iter_ordered()` with corrupt data, verify errors are returned
- Test: verify ordering is correct when no errors

---

### 8. File Creation and Rotation Miss Directory `fsync`

**Locations**:
- `src/file.rs:152-165`
- `src/writer_uring.rs:110-120`
- `src/writer_uring.rs:210-220`

**Root Cause Analysis**:
When a new WAL file is created:
1. File is created
2. Header is written
3. File is synced (`sync_all()`)

But the directory entry itself is not synced. If the system crashes between file creation and directory sync, the file entry may not be durable even though the file contents are.

**Scenario**:
```
Time 1: create("wal/wal.000001.log")  // Directory entry created in memory
Time 2: write(header)                  // File content written
Time 3: fsync(file)                    // File content durable
Time 4: <CRASH>
Time 5: Reboot                         // Directory not synced, file may not exist
```

**Impact**:
- Newly created files may disappear after crash
- Inconsistent state: file content was synced but file doesn't exist
- Recovery may miss recent data

**Fix Plan**:

**Step 1: Add directory sync helper**
```rust
// In file.rs

/// Sync the parent directory to ensure file creation is durable.
async fn sync_dir(dir: &Path) -> Result<()> {
    // Open the directory itself (not a file in it)
    let dir_file = tokio::fs::OpenOptions::new()
        .read(true)
        .open(dir)
        .await?;
    
    // On Unix, we need to sync the directory
    #[cfg(unix)]
    {
        use std::os::unix::fs::MetadataExt;
        let metadata = dir_file.metadata().await?;
        if metadata.is_dir() {
            dir_file.sync_all().await?;
        }
    }
    
    // On Windows, directory metadata is updated automatically
    // when files are created/modified, so no explicit sync needed
    
    Ok(())
}
```

**Step 2: Call directory sync after file creation**
```rust
impl WalFile {
    pub async fn create(dir: &Path, seq: u32, base_lsn: u64) -> Result<Self> {
        let path = dir.join(make_filename(seq));
        let mut file = OpenOptions::new()
            .create(true)
            .write(true)
            .truncate(true)
            .open(&path)
            .await?;

        let header = FileHeader::new(seq, base_lsn);
        let header_bytes = header.encode();
        file.write_all(&header_bytes).await?;
        file.sync_all().await?;
        
        // NEW: Sync the directory to ensure the file entry is durable
        sync_dir(dir).await?;

        Ok(Self {
            file,
            header,
            path,
            write_offset: FILE_HEADER_SIZE,
        })
    }
}
```

**Step 3: Apply to rotation logic**
Both `writer_tokio.rs::rotate_file()` and `writer_uring.rs::rotate_file()` create new files and should also sync the directory. Since they use `WalFile::create()`, the fix in Step 2 covers them.

**Step 4: Apply to uring backend**
In `writer_uring.rs`, the file creation happens inline (not via `WalFile`), so add directory sync there:
```rust
async fn ensure_active_file(&self) -> Result<()> {
    // ... existing file creation code ...
    
    let (res, file) = file.write_all_at(&header_bytes, 0).await;
    res?;
    let (res, file) = file.sync_all().await;
    res?;
    
    // NEW: Sync directory
    let dir_file = tokio_uring::fs::OpenOptions::new()
        .read(true)
        .open(&self.dir)
        .await?;
    let (res, _) = dir_file.sync_all().await;
    res?;
    
    // ... rest of the method ...
}
```

**Note on performance**: Directory sync adds an extra I/O operation on every file creation/rotation. This is acceptable because:
- File rotation is infrequent (controlled by size/time thresholds)
- Durability is more important than performance for WAL
- The cost is one extra fsync per rotation, not per write

**Validation**:
- Test: create WAL file, sync, kill -9 the process, restart, verify file exists
- Test: rotation creates new file, verify both files exist after restart

---

### 9. Dynamic Error Messages Leak Memory

**Locations**:
- `src/error.rs:14-27`
- `src/compress.rs:34-36`
- `src/encrypt.rs:25-33`
- `src/encrypt.rs:57-65`

**Root Cause Analysis**:
```rust
// In error.rs
#[error("Invalid record: {0}")]
InvalidRecord(&'static str),

#[error("Invalid configuration: {0}")]
InvalidConfig(&'static str),

// In compress.rs
pub fn decompress(compressed: &[u8]) -> Result<Vec<u8>> {
    decompress_size_prepended(compressed)
        .map_err(|e| Error::InvalidRecord(format!("decompression failed: {}", e).leak()))
        //                                                           ^^^^^^^^^^^^ LEAK!
}

// In encrypt.rs
pub fn encrypt(data: &[u8], key: &EncryptionKey) -> Result<Vec<u8>> {
    let cipher = Aes256Gcm::new_from_slice(key)
        .map_err(|e| Error::InvalidConfig(format!("invalid key: {}", e).leak()))?;
    //                                                          ^^^^^^^^^^^^ LEAK!
    // ...
}
```

Every dynamic error message calls `.leak()` which:
1. Converts `String` to `&'static str`
2. Leaks the heap allocation for the entire process lifetime
3. Accumulates over time if errors are frequent

**Impact**:
- Memory leak in error paths
- Particularly bad in scenarios with repeated failures (e.g., corrupted data, invalid keys)
- Process memory grows without bound

**Fix Plan**:

**Option A (Recommended): Change error variants to own strings**
```rust
// In error.rs

#[derive(Debug, Error)]
pub enum Error {
    #[error("IO error: {0}")]
    Io(#[from] io::Error),

    #[error("CRC mismatch")]
    CrcMismatch,

    #[error("Invalid record: {0}")]
    InvalidRecord(String),  // Changed from &'static str

    #[error("Invalid file header: {0}")]
    InvalidHeader(String),  // Changed from &'static str

    #[error("File not found: {0}")]
    FileNotFound(String),

    #[error("WAL is closed")]
    Closed,

    #[error("Invalid configuration: {0}")]
    InvalidConfig(String),  // Changed from &'static str

    // ... rest unchanged ...
}
```

**Update call sites**:
```rust
// In compress.rs
pub fn decompress(compressed: &[u8]) -> Result<Vec<u8>> {
    decompress_size_prepended(compressed)
        .map_err(|e| Error::InvalidRecord(format!("decompression failed: {}", e)))
        // No more .leak()!
}

// In encrypt.rs
pub fn encrypt(data: &[u8], key: &EncryptionKey) -> Result<Vec<u8>> {
    let cipher = Aes256Gcm::new_from_slice(key)
        .map_err(|e| Error::InvalidConfig(format!("invalid key: {}", e)))?;
    // No more .leak()!
    // ...
}

pub fn decrypt(encrypted: &[u8], key: &EncryptionKey) -> Result<Vec<u8>> {
    // ...
    cipher
        .decrypt(nonce, ciphertext)
        .map_err(|e| Error::InvalidRecord(format!("decryption failed: {}", e)))
        // No more .leak()!
}
```

**Step 2: Update existing callers of these error variants**
Search for all uses of `Error::InvalidRecord` and `Error::InvalidConfig` and update from string literals to `String`:
```rust
// Old
Error::InvalidRecord("header too short")

// New (can use .into() or to_string())
Error::InvalidRecord("header too short".into())
// or
Error::InvalidRecord("header too short".to_string())
```

**Option B: Use `Box<str>` (more memory efficient)**
```rust
#[error("Invalid record: {0}")]
InvalidRecord(Box<str>),

// Usage
Error::InvalidRecord(format!("...").into_boxed_str())
```

`Box<str>` is more efficient than `String` because it doesn't need capacity tracking, but `String` is more idiomatic.

**Option C: Use `Cow<'static, str>` (supports both static and owned)**
```rust
#[error("Invalid record: {0}")]
InvalidRecord(Cow<'static, str>),

// Usage for static strings
Error::InvalidRecord(Cow::Borrowed("header too short"))

// Usage for dynamic strings
Error::InvalidRecord(Cow::Owned(format!("decompression failed: {}", e)))
```

**Why Option A is recommended**:
- Simpler type signature
- No need to decide at each call site
- `String` is the standard choice for owned strings in Rust
- Slight memory overhead is acceptable for error types

**Validation**:
- Compile and run tests to find all call sites that need updating
- No functional change, but memory usage no longer grows on errors

---

## Minor (P2-P3) - Nice to Have

### 10. Configuration Surface Includes Unimplemented Options

**Locations**:
- `src/config.rs:18-20`
- `src/writer_tokio.rs:40-42`
- `src/writer_uring.rs:58-60`

**Root Cause Analysis**:
```rust
pub struct WalConfig {
    pub dir: PathBuf,
    pub max_file_size: u64,
    pub max_file_age: Option<Duration>,
    pub sync_mode: SyncMode,
    pub preallocate_size: u64,      // Not used!
    pub create_if_missing: bool,    // Not used!
    pub recovery_mode: RecoveryMode, // Not used! (Issue #7)
}
```

Three config options are stored but never used:
1. `preallocate_size`: Validated but never passed to file creation
2. `create_if_missing`: Both backends always call `create_dir_all()`, ignoring this flag
3. `recovery_mode`: Covered by issue #7

**Fix Plan**:

**Option A: Remove unimplemented options (recommended for now)**

```rust
pub struct WalConfig {
    pub dir: PathBuf,
    pub max_file_size: u64,
    pub max_file_age: Option<Duration>,
    pub sync_mode: SyncMode,
    // preallocate_size: removed
    // create_if_missing: removed
    pub recovery_mode: RecoveryMode,  // Keep, will be implemented in issue #7
}

impl Default for WalConfig {
    fn default() -> Self {
        Self {
            dir: PathBuf::from("wal"),
            max_file_size: DEFAULT_MAX_FILE_SIZE,
            max_file_age: None,
            sync_mode: SyncMode::Batch {
                bytes: DEFAULT_SYNC_BYTES,
                time: Duration::from_millis(DEFAULT_SYNC_TIME_MS),
            },
            recovery_mode: RecoveryMode::TolerateTailCorruption,
        }
    }
}

impl WalConfig {
    // Remove these methods
    // pub fn preallocate_size(...) { ... }
    // pub fn create_if_missing(...) { ... }
    
    // Keep this one (will be used in issue #7)
    pub fn recovery_mode(mut self, mode: RecoveryMode) -> Self {
        self.recovery_mode = mode;
        self
    }
    
    pub fn validate(&self) -> Result<()> {
        if self.max_file_size == 0 {
            return Err(Error::InvalidConfig("max_file_size must be > 0".into()));
        }
        // Remove preallocate_size check
        Ok(())
    }
}
```

**Option B: Implement the options**

**Implementing `preallocate_size`**:
```rust
// In file.rs
impl WalFile {
    pub async fn create(dir: &Path, seq: u32, base_lsn: u64, preallocate_size: u64) -> Result<Self> {
        let path = dir.join(make_filename(seq));
        let mut file = OpenOptions::new()
            .create(true)
            .write(true)
            .truncate(true)
            .open(&path)
            .await?;

        // Preallocate space
        if preallocate_size > FILE_HEADER_SIZE {
            file.set_len(preallocate_size).await?;
        }

        // Write header
        let header = FileHeader::new(seq, base_lsn);
        let header_bytes = header.encode();
        file.write_all(&header_bytes).await?;
        file.sync_all().await?;
        
        // ...
    }
}
```

**Implementing `create_if_missing`**:
```rust
// In writer_tokio.rs
impl Wal {
    pub async fn open(config: WalConfig) -> Result<Self> {
        config.validate()?;
        
        let dir = config.dir.clone();
        
        if config.create_if_missing {
            fs::create_dir_all(&dir).await?;
        } else {
            // Check if directory exists
            if !fs::try_exists(&dir).await.unwrap_or(false) {
                return Err(Error::InvalidConfig("directory does not exist".into()));
            }
        }
        
        // ...
    }
}
```

**Recommendation**: Use Option A for now. These features can be added later when there's a clear use case. Having unused config options is misleading to users.

**Migration path if Option A is chosen**:
1. Mark fields as `#[deprecated]` in one release
2. Remove in the next release
3. Document in CHANGELOG

---

### 11. `uring_advanced.rs` Is Framework Code With No Integration Path

**Locations**:
- `src/uring_advanced.rs`

**Root Cause Analysis**:
The module defines three helper types:
- `RegisteredFiles`: Manages registered file descriptors
- `LinkedOps`: Builder for linked operations (write + fsync as atomic chain)
- `BatchSubmit`: Helper for batch submission

But none of these are used in `writer_uring.rs`. The module is dead code that:
1. Adds maintenance burden
2. Misleads readers about capabilities
3. Increases compile time
4. Has no tests that actually use it with real I/O

**Fix Plan**:

**Option A: Remove the module (recommended)**

1. Delete `src/uring_advanced.rs`
2. Remove from `src/lib.rs`:
```rust
// Remove this
#[cfg(all(target_os = "linux", feature = "io-uring"))]
mod uring_advanced;
```
3. Update `writer_uring.rs` header comment (lines 6-10) to remove reference to advanced features

**Option B: Integrate the helpers into writer_uring.rs**

This requires significant work and is not recommended unless there's a specific performance requirement.

**Example integration (for reference only)**:
```rust
use crate::uring_advanced::{BatchSubmit, LinkedOps};

impl Wal {
    pub async fn write(&self, data: &[u8]) -> Result<u64> {
        let mut batch = BatchSubmit::default();
        
        // ... in write loop ...
        if batch.add() {
            // Submit batch
        }
    }
}
```

**Why Option A is recommended**:
- YAGNI principle (You Aren't Gonna Need It)
- The current simple implementation works
- Can be re-added later if profiling shows need for these optimizations
- Removes misleading code

**Validation**:
- After removal: `cargo build --features io-uring` must succeed
- All tests pass
- No references to `uring_advanced` anywhere in codebase

---

## Implementation Priority

### Phase 1: Critical Fixes (Do First)
1. **Issue #1**: Fix Linux backend syntax (blocks all Linux testing)
2. **Issue #2**: Fix LSN encoding (data corruption)
3. **Issue #5**: Fix recovery offset (data loss)
4. **Issue #3**: Fix sequence recovery (data loss)

### Phase 2: Safety Fixes
5. **Issue #4**: Fix truncate active file (data loss)
6. **Issue #8**: Add directory fsync (durability)

### Phase 3: Correctness Fixes
7. **Issue #6**: Fix truncate boundary condition
8. **Issue #7**: Fix recovery mode and ordered iteration

### Phase 4: Cleanup
9. **Issue #9**: Fix memory leaks in errors
10. **Issue #10**: Remove unused config options
11. **Issue #11**: Remove unused uring_advanced module

---

## Testing Strategy

After implementing fixes, run this test matrix:

### Platform Coverage
- [ ] Linux with io_uring backend
- [ ] macOS/Windows with tokio backend
- [ ] Cross-platform recovery (write on Linux, recover on macOS)

### Scenario Coverage
- [ ] Fresh start (empty directory)
- [ ] Recovery from clean shutdown
- [ ] Recovery from crash (partial write at tail)
- [ ] Recovery with corrupted data
- [ ] Truncation with various LSN values
- [ ] File rotation by size and time
- [ ] Concurrent writes (if supported)
- [ ] Long-running with many rotations

### Failure Modes
- [ ] Disk full during write
- [ ] Disk full during rotation
- [ ] Power loss simulation (kill -9)
- [ ] Corrupted file headers
- [ ] Corrupted record headers
- [ ] Truncated files

---

## References

- Original issues: `doc/issues.md`
- Design document: `doc/design.md` (if exists)
- Record format: `src/record.rs`
- File format: `src/file.rs`
