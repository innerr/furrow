//! WAL writer implementation.
//!
//! This module provides a platform-specific WAL writer:
//! - On Linux: Uses io_uring for high-performance async I/O
//! - On other platforms: Uses tokio::fs as fallback

#[cfg(not(target_os = "linux"))]
#[path = "writer_tokio.rs"]
mod writer_impl;

#[cfg(target_os = "linux")]
#[path = "writer_uring.rs"]
mod writer_impl;

pub use writer_impl::{Wal, WalSync};

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::{SyncMode, WalConfig};
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
