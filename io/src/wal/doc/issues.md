# WAL Implementation Issues

**Review Date**: 2026-03-18
**Total Issues**: 11

| Severity | Count |
|----------|-------|
| Critical (P0) | 5 |
| Medium (P1) | 4 |
| Minor (P2-P3) | 2 |

---

## Critical (P0) - Must Fix Before Production

### 1. Linux Backend Is Syntactically Broken

**Location**:
- `io/src/wal/writer.rs:11-15`
- `io/src/wal/writer_uring.rs:137-195`
- `io/src/wal/writer_uring.rs:330-357`

**Impact**: The crate cannot build on Linux, which is the platform that selects the io_uring backend by default.

**Evidence**:
- `writer.rs` routes Linux builds to `writer_uring.rs`
- `writer_uring.rs` contains invalid syntax (`.map_err(...)` without a receiver, stray `} else {`)
- `rustfmt --edition 2021 --check wal/writer_uring.rs` fails before formatting with parse errors
- The file also lacks a valid `Wal::write()` implementation even though tests call `wal.write(...)`

**Fix**: Reconstruct the Linux writer around a complete `write()` path, then add a Linux-targeted build/test job.

---

### 2. Persisted Record Headers Store `lsn = 0`

**Location**:
- `io/src/wal/writer_tokio.rs:93-112`
- `io/src/wal/record.rs:102-108`

**Impact**: Records written by the tokio backend are persisted with `lsn = 0`, so recovery cannot rely on on-disk LSN ordering.

**Evidence**:
```rust
let record = Record::new(0, data.to_vec());
let encoded = record.encode()?;
let lsn = inner.next_lsn.fetch_add(1, Ordering::Relaxed);
```

`Record::encode()` serializes `self.lsn`, so allocating the real LSN after encoding leaves bytes `8..16` in the header as zero.

**Fix**: Allocate the LSN before encoding, or patch the encoded buffer before writing.

---

### 3. Recovery Resets `next_seq` to Zero and Reuses Old Filenames

**Location**:
- `io/src/wal/writer_tokio.rs:45-52`
- `io/src/wal/writer_uring.rs:62-72`

**Impact**: Reopening an existing WAL starts naming new files from `wal.000000.log` again, which can overwrite previously recovered data.

**Evidence**:
```rust
if let Some((_, path)) = files.last() {
    ...
    (last_lsn.map_or(0, |lsn| lsn + 1), file_size, 0)
}
```

The recovered sequence number from the last file is discarded instead of advancing to `last_seq + 1`.

**Fix**: Carry forward the highest recovered file sequence and create the next file with `seq + 1`.

---

### 4. `truncate()` Can Delete the Active File While Writers Still Append to It

**Location**:
- `io/src/wal/writer_tokio.rs:191-200`
- `io/src/wal/writer_uring.rs:252-264`

**Impact**: After `truncate()` removes the currently open WAL file, later writes continue to the unlinked inode and disappear from directory-based recovery.

**Evidence**:
- `truncate()` scans filenames from disk and removes matching paths
- It does not lock writer state long enough to exclude the active file
- It does not compare against `inner.current_file` / `current_seq`
- On Unix, removing an open file succeeds; the process can keep writing to a file that is no longer reachable by path

**Fix**: Never unlink the active file. Rotate first or explicitly exclude the current file from truncation.

---

### 5. Recovery Appends After a Corrupted Tail Instead of After the Last Valid Record

**Location**:
- `io/src/wal/writer_tokio.rs:45-49`
- `io/src/wal/writer_tokio.rs:220-245`
- `io/src/wal/writer_uring.rs:62-69`
- `io/src/wal/writer_uring.rs:290-321`
- `io/src/wal/reader.rs:136-170`

**Impact**: If the previous process crashed with a torn or corrupt tail, new records are appended after the bad bytes and become unreadable because recovery stops at the first bad tail.

**Evidence**:
- `open()` uses the physical file size as the next append offset
- `recover_last_lsn()` returns only the last valid LSN, not the last valid offset
- `reader.rs` stops scanning the current file on truncated header/data or CRC failure

That combination means post-restart writes can land beyond corrupt tail bytes, but the reader never reaches them.

**Fix**: Recovery must return both last valid LSN and last valid offset, then truncate the file back to that offset before accepting new writes.

---

## Medium (P1) - Should Fix Soon

### 6. `truncate(lsn)` Uses the Wrong Boundary Condition

**Location**:
- `io/src/wal/writer_tokio.rs:196-199`
- `io/src/wal/writer_uring.rs:261-264`
- `io/src/wal/doc/design.md:32-35`

**Impact**: Files whose last record is exactly `lsn` are retained even though the design says `truncate(lsn)` should discard records with `LSN <= lsn`.

**Evidence**:
```rust
if last < lsn {
    remove_file(...)
}
```

The condition should be `last <= lsn` if the implementation is following the design contract.

**Fix**: Align the implementation with the documented `<=` semantics, or rename the API if the contract is meant to be exclusive.

---

### 7. Recovery Mode Configuration Is Ignored, and Ordered Iteration Drops Errors

**Location**:
- `io/src/wal/config.rs:20`
- `io/src/wal/writer_tokio.rs:207-208`
- `io/src/wal/writer_uring.rs:271-272`
- `io/src/wal/reader.rs:17-21`
- `io/src/wal/reader.rs:32-35`

**Impact**: Callers cannot actually enforce strict recovery through `WalConfig`, and `iter_ordered()` can silently hide corruption by discarding `Err` items.

**Evidence**:
- `WalConfig` stores `recovery_mode`, but neither writer passes it into `WalReader`
- `Wal::reader()` always calls `WalReader::new(...)`, which hardcodes `TolerateTailCorruption`
- `iter_ordered()` does `self.iter().filter_map(|r| r.ok())`, which drops every error before sorting

**Fix**: Persist recovery mode in `Wal` and make `iter_ordered()` preserve errors instead of filtering them out.

---

### 8. File Creation and Rotation Miss Directory `fsync`

**Location**:
- `io/src/wal/file.rs:152-165`
- `io/src/wal/writer_uring.rs:110-120`
- `io/src/wal/writer_uring.rs:210-220`

**Impact**: A crash after file creation can lose the directory entry even when the file contents themselves were synced.

**Evidence**:
- Newly created WAL files are `sync_all()`'d
- Their parent directory is never synced afterward

**Fix**: After creating or rotating a WAL file, open the parent directory and `fsync` it as part of the durability path.

---

### 9. Dynamic Error Messages Leak Memory

**Location**:
- `io/src/wal/error.rs:14-27`
- `io/src/wal/compress.rs:34-36`
- `io/src/wal/encrypt.rs:25-33`
- `io/src/wal/encrypt.rs:57-65`

**Impact**: Every dynamic compression/encryption error leaks heap memory for the lifetime of the process.

**Evidence**:
- `Error::InvalidRecord` and `Error::InvalidConfig` require `&'static str`
- Feature code converts `String` to `&'static str` with `.leak()` to satisfy the type

**Fix**: Change those error variants to own their message (`String`, `Box<str>`, or `Cow<'static, str>`).

---

## Minor (P2-P3) - Nice to Have

### 10. Configuration Surface Includes Unimplemented Options

**Location**:
- `io/src/wal/config.rs:18-20`
- `io/src/wal/writer_tokio.rs:40-42`
- `io/src/wal/writer_uring.rs:58-60`

**Impact**: Callers can set options that have no effect, which makes the API misleading.

**Evidence**:
- `preallocate_size` is validated and stored but never used during file creation
- `create_if_missing` is stored but both writers always call `create_dir_all(...)`
- `recovery_mode` is stored but ignored by `Wal::reader()` (see issue 7)

**Fix**: Either implement these options or remove them from the public config until they are supported.

---

### 11. `uring_advanced.rs` Is Framework Code With No Integration Path

**Location**:
- `io/src/wal/uring_advanced.rs`

**Impact**: The module adds maintenance surface and implies optimizations that the actual Linux writer never uses.

**Evidence**:
- `RegisteredFiles`, `LinkedOps`, and `BatchSubmit` are not referenced by `writer_uring.rs`
- The current Linux writer does not integrate any of the advanced helpers

**Fix**: Either wire the helpers into the Linux writer, or remove the module until there is a real integration plan.

---

## Summary

| # | Issue | Severity | Status |
|---|-------|----------|--------|
| 1 | Linux backend does not parse/build | P0 | Confirmed |
| 2 | Persisted LSN stays zero in tokio writer | P0 | Confirmed |
| 3 | Recovery resets file sequence | P0 | Confirmed |
| 4 | `truncate()` can unlink the active WAL file | P0 | Confirmed |
| 5 | Recovery appends after corrupted tail | P0 | Confirmed |
| 6 | `truncate(lsn)` uses `<` instead of `<=` | P1 | Confirmed |
| 7 | Recovery mode config ignored; ordered iteration drops errors | P1 | Confirmed |
| 8 | Missing directory `fsync` on create/rotate | P1 | Confirmed |
| 9 | Error API forces `.leak()` memory leaks | P1 | Confirmed |
| 10 | `WalConfig` exposes unimplemented options | P3 | Confirmed |
| 11 | `uring_advanced.rs` is unused | P3 | Confirmed |
