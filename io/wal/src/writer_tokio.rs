//! WAL writer implementation (tokio::fs backend).

use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Instant;

use tokio::fs;
use tokio::sync::Mutex;

use crate::config::{SyncMode, WalConfig};
use crate::error::{Error, Result};
use crate::file::{self as wal_file, WalFile, FILE_HEADER_SIZE};
use crate::record::{Record, RecordHeader, HEADER_SIZE};

#[derive(Debug)]
pub struct Wal {
    dir: PathBuf,
    inner: Arc<Mutex<Inner>>,
}

#[derive(Debug)]
struct Inner {
    current_file: Option<WalFile>,
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
        fs::create_dir_all(&dir).await?;

        let files = wal_file::list_wal_files(&dir).await?;

        let (start_lsn, start_offset, next_seq) = if let Some((_, path)) = files.last() {
            let mut wal_file = WalFile::open(path.clone()).await?;
            let file_size = wal_file.write_offset;
            let last_lsn = recover_last_lsn(&mut wal_file).await?;
            (last_lsn.map_or(0, |lsn| lsn + 1), file_size, 0)
        } else {
            (0, FILE_HEADER_SIZE, 0)
        };

        let inner = Inner {
            current_file: None,
            next_lsn: AtomicU64::new(start_lsn),
            next_offset: start_offset,
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
            let wal_file = WalFile::create(&self.dir, seq, base_lsn).await?;
            inner.next_offset = FILE_HEADER_SIZE;
            inner.current_file = Some(wal_file);
            inner.next_seq += 1;
            inner.file_created_at = Instant::now();
        }

        Ok(())
    }

    pub async fn write(&self, data: &[u8]) -> Result<u64> {
        let record = Record::new(0, data.to_vec());
        let encoded = record.encode()?;
        let record_size = encoded.len() as u64;

        let mut inner = self.inner.lock().await;

        let size_exceeded = inner.next_offset + record_size > inner.max_file_size;
        let time_exceeded = inner
            .max_file_age
            .map(|age| inner.file_created_at.elapsed() >= age)
            .unwrap_or(false);
        let needs_rotation = size_exceeded || time_exceeded;

        if needs_rotation {
            self.rotate_file(&mut inner).await?;
        }

        let lsn = inner.next_lsn.fetch_add(1, Ordering::Relaxed);
        let offset = inner.next_offset;
        let sync_mode = inner.sync_mode.clone();
        let bytes_threshold = if let SyncMode::Batch { bytes, .. } = &sync_mode {
            Some(*bytes)
        } else {
            None
        };
        let time_threshold = if let SyncMode::Batch { time, .. } = &sync_mode {
            Some(*time)
        } else {
            None
        };

        inner
            .current_file
            .as_mut()
            .ok_or(Error::Closed)?
            .write(&encoded, offset)
            .await?;

        inner.next_offset += record_size;
        inner.bytes_since_sync += record_size;

        let should_sync = match &sync_mode {
            SyncMode::Always => true,
            SyncMode::Batch { .. } => {
                inner.bytes_since_sync >= bytes_threshold.unwrap()
                    || inner.last_sync.elapsed() >= time_threshold.unwrap()
            }
            SyncMode::Never => false,
        };

        if should_sync {
            inner
                .current_file
                .as_mut()
                .ok_or(Error::Closed)?
                .sync()
                .await?;
            inner.bytes_since_sync = 0;
            inner.last_sync = Instant::now();
        }

        Ok(lsn)
    }

    async fn rotate_file(&self, inner: &mut Inner) -> Result<()> {
        if let Some(mut current_file) = inner.current_file.take() {
            current_file.sync().await?;
        }

        let seq = inner.next_seq;
        let base_lsn = inner.next_lsn.load(Ordering::Relaxed);
        let wal_file = WalFile::create(&self.dir, seq, base_lsn).await?;

        inner.next_offset = FILE_HEADER_SIZE;
        inner.current_file = Some(wal_file);
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
        if let Some(current_file) = inner.current_file.as_mut() {
            current_file.sync().await?;
            inner.bytes_since_sync = 0;
            inner.last_sync = Instant::now();
        }
        Ok(())
    }

    pub async fn truncate(&self, lsn: u64) -> Result<()> {
        let files = wal_file::list_wal_files(&self.dir).await?;

        for (_, path) in &files {
            let mut wal_file = WalFile::open(path.clone()).await?;
            if let Some(last) = recover_last_lsn(&mut wal_file).await? {
                if last < lsn {
                    drop(wal_file);
                    fs::remove_file(path).await?;
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
        if let Some(mut current_file) = inner.current_file.take() {
            current_file.sync().await?;
        }
        Ok(())
    }
}

async fn recover_last_lsn(wal_file: &mut WalFile) -> Result<Option<u64>> {
    let file_size = wal_file.write_offset;
    let mut offset = FILE_HEADER_SIZE;
    let mut last_lsn = None;

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

        last_lsn = Some(header.lsn);
        offset = record_end;
    }

    Ok(last_lsn)
}

#[derive(Debug)]
pub struct WalSync {
    runtime: tokio::runtime::Runtime,
    wal: Wal,
}

impl WalSync {
    pub fn open(config: WalConfig) -> Result<Self> {
        let runtime = tokio::runtime::Runtime::new()?;
        let wal = runtime.block_on(Wal::open(config))?;
        Ok(Self { runtime, wal })
    }

    pub fn write(&self, data: &[u8]) -> Result<u64> {
        self.runtime.block_on(self.wal.write(data))
    }

    pub fn truncate(&self, lsn: u64) -> Result<()> {
        self.runtime.block_on(self.wal.truncate(lsn))
    }

    pub fn rotate(&self) -> Result<()> {
        self.runtime.block_on(self.wal.rotate())
    }

    pub fn sync(&self) -> Result<()> {
        self.runtime.block_on(self.wal.sync())
    }

    pub fn reader(&self) -> Result<crate::WalReader> {
        self.wal.reader()
    }

    pub fn close(self) -> Result<()> {
        self.runtime.block_on(self.wal.close())
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

    #[tokio::test]
    async fn test_write_multiple_records() {
        let dir = tempdir().unwrap();
        let config = WalConfig::new(dir.path()).sync_mode(SyncMode::Never);

        let wal = Wal::open(config).await.unwrap();

        for i in 0..100 {
            let data = format!("record {}", i);
            wal.write(data.as_bytes()).await.unwrap();
        }

        wal.sync().await.unwrap();
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

    #[tokio::test]
    async fn test_file_rotation_by_size() {
        let dir = tempdir().unwrap();
        let config = WalConfig::new(dir.path())
            .sync_mode(SyncMode::Always)
            .max_file_size(1024);

        let wal = Wal::open(config).await.unwrap();

        for i in 0..50 {
            let data = format!("record {:04}", i);
            wal.write(data.as_bytes()).await.unwrap();
        }

        wal.sync().await.unwrap();
        wal.close().await.unwrap();

        let file_count = std::fs::read_dir(dir.path())
            .unwrap()
            .filter(|e| {
                e.as_ref()
                    .map(|e| e.file_name().to_string_lossy().starts_with("wal."))
                    .unwrap_or(false)
            })
            .count();
        assert!(file_count > 1);
    }

    #[tokio::test]
    async fn test_file_rotation_by_time() {
        use std::time::Duration;

        let dir = tempdir().unwrap();
        let config = WalConfig::new(dir.path())
            .sync_mode(SyncMode::Always)
            .max_file_size(1024 * 1024)
            .max_file_age(Duration::from_millis(10));

        let wal = Wal::open(config).await.unwrap();

        wal.write(b"first file").await.unwrap();

        tokio::time::sleep(Duration::from_millis(50)).await;

        wal.write(b"second file after rotation").await.unwrap();

        wal.sync().await.unwrap();
        wal.close().await.unwrap();

        let file_count = std::fs::read_dir(dir.path())
            .unwrap()
            .filter(|e| {
                e.as_ref()
                    .map(|e| e.file_name().to_string_lossy().starts_with("wal."))
                    .unwrap_or(false)
            })
            .count();
        assert!(file_count >= 2);
    }
}
