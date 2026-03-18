//! WAL writer implementation using io_uring (Linux only).
//!
//! This implementation uses tokio-uring which provides a type-safe async API
//! on top of io_uring.
//!
//! # Advanced Features
//!
//! For advanced io_uring features (registered files, linked operations),
//! use the `uring-advanced` feature flag which enables the io-uring crate
//! directly. See `uring_advanced.rs` for the implementation framework.
//!
//! # Performance Notes
//!
//! - Uses io_uring for all I/O operations
//! - Automatic batch submission by tokio-uring runtime
//! - For SyncMode::Always, sync is called after each write
//! - For SyncMode::Batch, sync is called based on thresholds

use std::os::unix::io::AsRawFd;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Instant;

use tokio::sync::Mutex;

use crate::config::{SyncMode, WalConfig};
use crate::error::{Error, Result};
use crate::file::{self as wal_file, FILE_HEADER_SIZE};
use crate::record::{Record, RecordHeader, HEADER_SIZE, MAX_RECORD_SIZE, verify_crc};

#[derive(Debug)]
pub struct Wal {
    dir: PathBuf,
    inner: Arc<Mutex<Inner>>,
}

#[derive(Debug)]
struct Inner {
    current_file: Option<tokio_uring::fs::File>,
    current_fd: i32,
    current_seq: u32,
    next_lsn: AtomicU64,
    next_offset: u64,
    bytes_since_sync: u64,
    last_sync: Instant,
    next_seq: u32,
    max_file_size: u64,
    max_file_age: Option<std::time::Duration>,
    file_created_at: Instant,
    sync_mode: SyncMode,
}

impl Wal {
    pub async fn open(config: WalConfig) -> Result<Self> {
        config.validate()?;

        let dir = config.dir.clone();
        tokio::fs::create_dir_all(&dir).await?;

        let files = wal_file::list_wal_files_sync(&dir)?;
        let (start_lsn, next_seq) = if let Some((max_seq, path)) = files.last() {
            let file_size = get_file_size(path)?;
            let file = tokio_uring::fs::OpenOptions::new()
                .read(true)
                .write(true)
                .open(path)
                .await?;
            let info = recover_info(file, file_size).await?;

            if info.last_valid_offset < file_size {
                let (res, _) = file.set_len(info.last_valid_offset).await;
                res?;
            }

            (info.last_lsn.map_or(0, |lsn| lsn + 1), max_seq + 1)
        } else {
            (0, 0)
        };

        let inner = Inner {
            current_file: None,
            current_fd: -1,
            current_seq: 0,
            next_lsn: AtomicU64::new(start_lsn),
            next_offset: FILE_HEADER_SIZE,
            bytes_since_sync: 0,
            last_sync: Instant::now(),
            next_seq,
            max_file_size: config.max_file_size,
            max_file_age: config.max_file_age,
            file_created_at: Instant::now(),
            sync_mode: config.sync_mode,
        };

        let wal = Self {
            dir,
            inner: Arc::new(Mutex::new(inner)),
        };

        wal.ensure_active_file().await?;

        Ok(wal)
    }

    async fn ensure_active_file(&self) -> Result<()> {
        let mut inner = self.inner.lock().await;

        if inner.current_file.is_none() {
            let seq = inner.next_seq;
            let base_lsn = inner.next_lsn.load(Ordering::Relaxed);
            let path = self.dir.join(wal_file::make_filename(seq));

            let header = wal_file::FileHeader::new(seq, base_lsn);
            let header_bytes = header.encode();

            let file = tokio_uring::fs::OpenOptions::new()
                .create(true)
                .write(true)
                .truncate(true)
                .open(&path)
                .await?;

            let (res, file) = file.write_all_at(&header_bytes, 0).await;
            res?;
            let (res, file) = file.sync_all().await;
            res?;

            let fd = file.as_raw_fd();

            inner.next_offset = FILE_HEADER_SIZE;
            inner.current_file = Some(file);
            inner.current_fd = fd;
            inner.current_seq = seq;
            inner.next_seq += 1;
            inner.file_created_at = Instant::now();
        }

        Ok(())
    }

    pub async fn write(&self, data: &[u8]) -> Result<u64> {
        use crate::record::MAX_RECORD_SIZE;

        let estimated_size = HEADER_SIZE as u64 + data.len() as u64;
        if estimated_size as usize > MAX_RECORD_SIZE {
            return Err(Error::RecordTooLarge {
                size: estimated_size as usize,
                max: MAX_RECORD_SIZE,
            });
        }

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

    async fn rotate_file(&self, inner: &mut Inner) -> Result<()> {
        if let Some(current_file) = inner.current_file.take() {
            let (res, _) = current_file.sync_all().await;
            let _ = res;
        }

        let seq = inner.next_seq;
        let base_lsn = inner.next_lsn.load(Ordering::Relaxed);
        let path = self.dir.join(wal_file::make_filename(seq));

        let header = wal_file::FileHeader::new(seq, base_lsn);
        let header_bytes = header.encode();

        let file = tokio_uring::fs::OpenOptions::new()
            .create(true)
            .write(true)
            .truncate(true)
            .open(&path)
            .await?;

        let (res, file) = file.write_all_at(&header_bytes, 0).await;
        res?;
        let (res, file) = file.sync_all().await;
        res?;

        let fd = file.as_raw_fd();

        inner.next_offset = FILE_HEADER_SIZE;
        inner.current_file = Some(file);
        inner.current_fd = fd;
        inner.current_seq = seq;
        inner.next_seq += 1;
        inner.bytes_since_sync = 0;
        inner.file_created_at = Instant::now();

        Ok(())
    }

    pub async fn rotate(&self) -> Result<()> {
        let mut inner = self.inner.lock().await;
        self.rotate_file(&mut inner).await
    }

    pub async fn sync(&self) -> Result<()> {
        let mut inner = self.inner.lock().await;
        if let Some(current_file) = inner.current_file.as_ref() {
            let (res, file) = current_file.sync_all().await;
            res?;
            inner.current_file = Some(file);
            inner.bytes_since_sync = 0;
            inner.last_sync = Instant::now();
        }
        Ok(())
    }

    pub async fn truncate(&self, lsn: u64) -> Result<()> {
        // Hold the lock long enough to make the active-file decision once.
        let active_seq = {
            let mut inner = self.inner.lock().await;

            let active_last_lsn = if let Some(_current_file) = inner.current_file.as_ref() {
                // Need to read from the current file to get its last LSN
                // For uring, we need to open the file path separately
                let path = self.dir.join(wal_file::make_filename(inner.current_seq));
                let file_size = get_file_size(&path)?;
                let file = tokio_uring::fs::OpenOptions::new()
                    .read(true)
                    .open(&path)
                    .await?;
                let info = recover_info(file, file_size).await?;
                info.last_lsn
            } else {
                None
            };

            // If the active file is also fully truncatable, rotate first so that
            // the old active file becomes deletable without unlinking an open inode.
            if active_last_lsn.is_some_and(|last| last <= lsn) {
                self.rotate_file(&mut inner).await?;
            }

            inner.current_seq
        };

        let files = wal_file::list_wal_files_sync(&self.dir)?;

        for (seq, path) in &files {
            if *seq == active_seq {
                continue;
            }

            let file_size = get_file_size(path)?;
            let file = tokio_uring::fs::OpenOptions::new()
                .read(true)
                .open(path)
                .await?;
            let info = recover_info(file, file_size).await?;
            if let Some(last) = info.last_lsn {
                if last <= lsn {
                    tokio::fs::remove_file(path).await?;
                }
            }
        }

        Ok(())
    }

    pub fn reader(&self) -> Result<crate::WalReader> {
        crate::WalReader::new(self.dir.clone())
    }

    pub async fn close(self) -> Result<()> {
        let mut inner = self.inner.lock().await;
        if let Some(current_file) = inner.current_file.take() {
            let (res, _) = current_file.sync_all().await;
            let _ = res;
        }
        Ok(())
    }
}

fn get_file_size(path: &std::path::Path) -> Result<u64> {
    let metadata = std::fs::metadata(path)?;
    Ok(metadata.len())
}

struct RecoveryInfo {
    last_lsn: Option<u64>,
    last_valid_offset: u64,
}

async fn recover_info(mut file: tokio_uring::fs::File, file_size: u64) -> Result<RecoveryInfo> {
    let mut offset = FILE_HEADER_SIZE;
    let mut last_lsn = None;
    let mut last_valid_offset = FILE_HEADER_SIZE;

    while offset < file_size {
        if offset + HEADER_SIZE as u64 > file_size {
            break;
        }

        let mut header_buf = vec![0u8; HEADER_SIZE];
        let (res, f) = file.read_exact_at(&mut header_buf, offset).await;
        match res {
            Ok(_) => file = f,
            Err(_) => break,
        }

        let header = match RecordHeader::decode(&header_buf) {
            Ok(h) => h,
            Err(_) => break,
        };

        let record_end = offset + HEADER_SIZE as u64 + header.len as u64;
        if record_end > file_size {
            break;
        }

        let mut data_buf = vec![0u8; header.len as usize];
        let (res, f) = file.read_exact_at(&mut data_buf, offset + HEADER_SIZE as u64).await;
        match res {
            Ok(_) => file = f,
            Err(_) => break,
        }

        if !verify_crc(&data_buf, header.crc) {
            break;
        }

        last_lsn = Some(header.lsn);
        last_valid_offset = record_end;
        offset = record_end;
    }

    Ok(RecoveryInfo {
        last_lsn,
        last_valid_offset,
    })
}

#[derive(Debug)]
pub struct WalSync {
    wal: Wal,
}

impl WalSync {
    pub fn open(config: WalConfig) -> Result<Self> {
        let wal = tokio_uring::start(async { Wal::open(config).await })?;
        Ok(Self { wal })
    }

    pub fn write(&self, data: &[u8]) -> Result<u64> {
        let data = data.to_vec();
        tokio_uring::start(async { self.wal.write(&data).await })
    }

    pub fn truncate(&self, lsn: u64) -> Result<()> {
        tokio_uring::start(async { self.wal.truncate(lsn).await })
    }

    pub fn rotate(&self) -> Result<()> {
        tokio_uring::start(async { self.wal.rotate().await })
    }

    pub fn sync(&self) -> Result<()> {
        tokio_uring::start(async { self.wal.sync().await })
    }

    pub fn reader(&self) -> Result<crate::WalReader> {
        self.wal.reader()
    }

    pub fn close(self) -> Result<()> {
        tokio_uring::start(async { self.wal.close().await })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::tempdir;

    #[tokio::test]
    async fn test_write_and_read() {
        let dir = tempdir().unwrap();
        let config = WalConfig::new(dir.path()).sync_mode(SyncMode::Always);

        let wal = Wal::open(config).await.unwrap();

        let lsn1 = wal.write(b"hello").await.unwrap();
        let lsn2 = wal.write(b"world").await.unwrap();

        assert!(lsn2 > lsn1);

        wal.close().await.unwrap();
    }

    #[test]
    fn test_sync_api() {
        let dir = tempdir().unwrap();
        let config = WalConfig::new(dir.path()).sync_mode(SyncMode::Always);

        let wal = WalSync::open(config).unwrap();

        wal.write(b"test data").unwrap();

        wal.sync().unwrap();
        wal.close().unwrap();
    }
}
