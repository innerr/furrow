# WAL Implementation Issues

**Review Date**: 2026-03-18
**Total Issues**: 10

| Severity | Count |
|----------|-------|
| Critical (P0) | 3 |
| Medium (P1) | 3 |
| Minor (P2-P3) | 4 |

---

## Critical (P0) - Must Fix Before Production

### 1. writer_uring.rs: Syntax Errors and Missing Methods

**Location**: `io/src/wal/writer_uring.rs:135-195`

**Impact**: Code cannot compile on Linux

**Issue**:
```
Line 142: .map_err(...) without preceding expression
Line 149: orphan '} else {' without matching 'if'
Line 151: double semicolon ';;'
Missing: write() method (called by tests but not defined)
```

**Root Cause**: Incomplete refactoring of `write_with_sync` method

**Fix**: Rewrite `write_with_sync` and add proper `write()` method

**Test Case**: `cargo build --target x86_64-unknown-linux-gnu`

---

### 2. LSN Not Written to Record Header

**Location**: `io/src/wal/writer_tokio.rs:94`

**Impact**: All records have `lsn=0` in file, recovery cannot sort by LSN

**Issue**:
```rust
let record = Record::new(0, data.to_vec());  // LSN always 0
let encoded = record.encode()?;               // encoded with LSN=0
let lsn = inner.next_lsn.fetch_add(1, ...);   // real LSN allocated AFTER encode
// encoded bytes [8:16] still contain 0, not the real LSN!
```

**Root Cause**: LSN allocated after record is encoded

**Fix Options**:
1. Modify buffer bytes [8:16] after LSN allocation
2. Allocate LSN before encoding
3. Use separate header encoding step

**Test Case**: Write records, close, reopen, verify LSN ordering

---

### 3. next_seq Incorrect After Recovery

**Location**: `io/src/wal/writer_tokio.rs:49-52`

**Impact**: After restart, new files start from `wal.000000.log`, overwriting existing files

**Issue**:
```rust
let (start_lsn, start_offset, next_seq) = if let Some((_, path)) = files.last() {
    // ...
    (last_lsn.map_or(0, |lsn| lsn + 1), file_size, 0)  // next_seq = 0 always!
} else {
    (0, FILE_HEADER_SIZE, 0)
};
```

**Root Cause**: `next_seq` hardcoded to 0 instead of extracting from existing files

**Fix**:
```rust
let (start_lsn, start_offset, next_seq) = if let Some((seq, path)) = files.last() {
    // ...
    (last_lsn.map_or(0, |lsn| lsn + 1), file_size, seq + 1)
} else {
    (0, FILE_HEADER_SIZE, 0)
};
```

**Test Case**: Write to WAL, close, reopen, write again, verify file sequence continues

---

## Medium (P1) - Should Fix Soon

### 4. truncate() Has Confusing Semantics

**Location**: `io/src/wal/writer_tokio.rs:191-204`

**Impact**: API behavior doesn't match typical truncate expectations

**Issue**:
```rust
if let Some(last) = recover_last_lsn(&mut wal_file).await? {
    if last < lsn {  // Deletes files where ALL records have LSN < threshold
        fs::remove_file(path).await?;
    }
}
```

**Problems**:
- Name suggests truncating records within a file
- Actually deletes entire files
- Logic `last < lsn` is confusing (deletes older files, not newer)

**Fix Options**:
1. Rename to `delete_files_before(lsn)`
2. Fix logic if intended to remove records within current file
3. Document current behavior clearly

---

### 5. String::leak() Causes Memory Leak

**Locations**:
- `io/src/wal/compress.rs:36`
- `io/src/wal/encrypt.rs:26, 58`

**Impact**: Every error with dynamic message leaks heap memory

**Issue**:
```rust
Error::InvalidRecord(format!("decompression failed: {}", e).leak())
```

**Fix Options**:
1. Change Error enum to accept `String`:
   ```rust
   InvalidRecord(String),
   InvalidConfig(String),
   ```
2. Use `Box<str>`:
   ```rust
   InvalidRecord(Box<str>),
   ```
3. Use `Cow<'static, str>` for flexibility

---

### 6. Missing Directory Sync After File Creation

**Location**: `io/src/wal/file.rs:163-164`

**Impact**: New file entry may be lost on crash (file content synced but directory not)

**Issue**:
```rust
file.write_all(&header_bytes).await?;
file.sync_all().await?;  // Syncs file content
// Missing: sync parent directory to persist the file entry
```

**Background**: To ensure a new file is crash-safe, both the file and its parent directory must be synced.

**Fix**:
```rust
file.sync_all().await?;

// Sync directory
let dir = std::fs::File::open(dir)?;
dir.sync_all()?;
```

---

## Minor (P2-P3) - Nice to Have

### 7. uring_advanced.rs Not Actually Used

**Location**: `io/src/wal/uring_advanced.rs`

**Issue**: Framework code for `RegisteredFiles`, `LinkedOps`, `BatchSubmit` exists but is never integrated into `writer_uring.rs`

**Options**:
1. Delete the file (it's incomplete anyway)
2. Actually integrate these utilities into the writer
3. Mark as `#[doc(hidden)]` with TODO note

---

### 8. preallocate_size Config Not Implemented

**Location**: `io/src/wal/config.rs:18`

**Issue**:
```rust
pub preallocate_size: u64,  // Config exists
// But never used in file.rs or writer_*.rs
```

**Options**:
1. Implement file preallocation (posix_fallocate on Linux)
2. Remove config option

---

### 9. create_if_missing Not Implemented

**Location**: `io/src/wal/config.rs:19`

**Issue**:
```rust
pub create_if_missing: bool,  // Config exists
// But directory is always created: fs::create_dir_all(&dir).await?
```

**Fix**:
```rust
if self.create_if_missing {
    fs::create_dir_all(&dir).await?;
} else {
    // Check if directory exists
}
```

---

### 10. Error Type Only Accepts &'static str

**Location**: `io/src/wal/error.rs:14-15, 26`

**Issue**:
```rust
InvalidRecord(&'static str),
InvalidConfig(&'static str),
// Cannot use format!() without .leak()
```

**Fix**:
```rust
InvalidRecord(Box<str>),  // or String
InvalidConfig(Box<str>),
```

---

## Summary

| # | Issue | Severity | Status |
|---|-------|----------|--------|
| 1 | writer_uring.rs syntax errors | P0 | ❌ Broken |
| 2 | LSN not written to record | P0 | ❌ Data loss |
| 3 | next_seq incorrect after recovery | P0 | ❌ File corruption |
| 4 | truncate() semantics confusing | P1 | ⚠️ API issue |
| 5 | String::leak() memory leak | P1 | ⚠️ Memory issue |
| 6 | Missing directory sync | P1 | ⚠️ Crash safety |
| 7 | uring_advanced.rs unused | P2 | 💤 Dead code |
| 8 | preallocate_size not implemented | P3 | 💤 Dead config |
| 9 | create_if_missing not implemented | P3 | 💤 Dead config |
| 10 | Error type limitation | P3 | 💤 Usability |

---

## Recommended Fix Order

1. **[P0]** Fix writer_uring.rs (required for Linux)
2. **[P0]** Fix LSN write (required for correct recovery)
3. **[P0]** Fix next_seq recovery (required for crash recovery)
4. **[P1]** Fix memory leak in error handling
5. **[P1]** Add directory sync
6. **[P1]** Clarify truncate() semantics
7. **[P2+]** Clean up dead code/configs
