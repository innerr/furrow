//! File management for WAL.

use std::path::{Path, PathBuf};

use tokio::fs::{self, File, OpenOptions};
use tokio::io::{AsyncReadExt, AsyncSeekExt, AsyncWriteExt};

use super::error::{Error, Result};

pub const FILE_HEADER_SIZE: u64 = 64;
pub const MAGIC: &[u8; 4] = b"WAL1";
pub const VERSION: u16 = 1;

pub const FILE_NAME_PREFIX: &str = "wal.";
pub const FILE_NAME_SUFFIX: &str = ".log";

#[derive(Debug, Clone)]
pub struct FileHeader {
    pub magic: [u8; 4],
    pub version: u16,
    pub seq: u32,
    pub base_lsn: u64,
    pub created_at: u64,
}

impl Default for FileHeader {
    fn default() -> Self {
        Self {
            magic: *MAGIC,
            version: VERSION,
            seq: 0,
            base_lsn: 0,
            created_at: 0,
        }
    }
}

impl FileHeader {
    pub fn new(seq: u32, base_lsn: u64) -> Self {
        Self {
            magic: *MAGIC,
            version: VERSION,
            seq,
            base_lsn,
            created_at: std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap_or_default()
                .as_millis() as u64,
        }
    }

    pub fn encode(&self) -> Vec<u8> {
        let mut buf = Vec::with_capacity(FILE_HEADER_SIZE as usize);
        buf.extend_from_slice(&self.magic);
        buf.extend_from_slice(&self.version.to_le_bytes());
        buf.extend_from_slice(&self.seq.to_le_bytes());
        buf.extend_from_slice(&self.base_lsn.to_le_bytes());
        buf.extend_from_slice(&self.created_at.to_le_bytes());
        buf.resize(FILE_HEADER_SIZE as usize, 0);
        buf
    }

    pub fn decode(buf: &[u8]) -> Result<Self> {
        if buf.len() < FILE_HEADER_SIZE as usize {
            return Err(Error::InvalidHeader("header too short"));
        }

        let magic = [buf[0], buf[1], buf[2], buf[3]];
        if &magic != MAGIC {
            return Err(Error::InvalidHeader("invalid magic"));
        }

        let version = u16::from_le_bytes([buf[4], buf[5]]);
        if version != VERSION {
            return Err(Error::InvalidHeader("unsupported version"));
        }

        let seq = u32::from_le_bytes([buf[6], buf[7], buf[8], buf[9]]);
        let base_lsn = u64::from_le_bytes([
            buf[10], buf[11], buf[12], buf[13], buf[14], buf[15], buf[16], buf[17],
        ]);
        let created_at = u64::from_le_bytes([
            buf[18], buf[19], buf[20], buf[21], buf[22], buf[23], buf[24], buf[25],
        ]);

        Ok(Self {
            magic,
            version,
            seq,
            base_lsn,
            created_at,
        })
    }
}

pub fn make_filename(seq: u32) -> String {
    format!("{}{:06}{}", FILE_NAME_PREFIX, seq, FILE_NAME_SUFFIX)
}

pub fn parse_filename(name: &str) -> Option<u32> {
    if !name.starts_with(FILE_NAME_PREFIX) || !name.ends_with(FILE_NAME_SUFFIX) {
        return None;
    }
    let seq_str = &name[FILE_NAME_PREFIX.len()..name.len() - FILE_NAME_SUFFIX.len()];
    seq_str.parse().ok()
}

pub async fn list_wal_files(dir: &Path) -> Result<Vec<(u32, PathBuf)>> {
    let mut entries = fs::read_dir(dir).await?;
    let mut files = Vec::new();

    while let Some(entry) = entries.next_entry().await? {
        let name = entry.file_name();
        let name = name.to_string_lossy();
        if let Some(seq) = parse_filename(&name) {
            files.push((seq, entry.path()));
        }
    }

    files.sort_by_key(|(seq, _)| *seq);
    Ok(files)
}

#[cfg(target_os = "linux")]
pub fn list_wal_files_sync(dir: &Path) -> Result<Vec<(u32, PathBuf)>> {
    let entries = std::fs::read_dir(dir)?;
    let mut files = Vec::new();

    for entry in entries {
        let entry = entry?;
        let name = entry.file_name();
        let name = name.to_string_lossy();
        if let Some(seq) = parse_filename(&name) {
            files.push((seq, entry.path()));
        }
    }

    files.sort_by_key(|(seq, _)| *seq);
    Ok(files)
}

#[derive(Debug)]
#[allow(dead_code)]
pub struct WalFile {
    pub file: File,
    pub header: FileHeader,
    pub path: PathBuf,
    pub write_offset: u64,
}

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

        Ok(Self {
            file,
            header,
            path,
            write_offset: FILE_HEADER_SIZE,
        })
    }

    pub async fn open(path: PathBuf) -> Result<Self> {
        let mut file = OpenOptions::new()
            .read(true)
            .write(true)
            .open(&path)
            .await?;

        let metadata = file.metadata().await?;
        let file_size = metadata.len();

        let mut header_buf = vec![0u8; FILE_HEADER_SIZE as usize];
        file.read_exact(&mut header_buf).await?;
        let header = FileHeader::decode(&header_buf)?;

        Ok(Self {
            file,
            header,
            path,
            write_offset: file_size,
        })
    }

    pub async fn write(&mut self, data: &[u8], offset: u64) -> Result<()> {
        self.file.seek(std::io::SeekFrom::Start(offset)).await?;
        self.file.write_all(data).await?;
        self.write_offset = self.write_offset.max(offset + data.len() as u64);
        Ok(())
    }

    pub async fn sync(&mut self) -> Result<()> {
        self.file.sync_all().await?;
        Ok(())
    }

    pub async fn read_at(&mut self, offset: u64, len: usize) -> Result<Vec<u8>> {
        self.file.seek(std::io::SeekFrom::Start(offset)).await?;
        let mut buf = vec![0u8; len];
        self.file.read_exact(&mut buf).await?;
        Ok(buf)
    }

    #[allow(dead_code)]
    pub fn seq(&self) -> u32 {
        self.header.seq
    }

    #[allow(dead_code)]
    pub fn base_lsn(&self) -> u64 {
        self.header.base_lsn
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::tempdir;

    #[test]
    fn test_filename_parsing() {
        assert_eq!(parse_filename("wal.000001.log"), Some(1));
        assert_eq!(parse_filename("wal.000042.log"), Some(42));
        assert_eq!(parse_filename("wal.123456.log"), Some(123456));
        assert_eq!(parse_filename("other.log"), None);
        assert_eq!(parse_filename("wal.abc.log"), None);
    }

    #[tokio::test]
    async fn test_file_header_encode_decode() {
        let header = FileHeader::new(42, 1000);
        let encoded = header.encode();
        let decoded = FileHeader::decode(&encoded).unwrap();

        assert_eq!(decoded.seq, 42);
        assert_eq!(decoded.base_lsn, 1000);
    }

    #[tokio::test]
    async fn test_create_and_open_wal_file() {
        let dir = tempdir().unwrap();
        let dir_path = dir.path();

        let wal_file = WalFile::create(dir_path, 1, 0).await.unwrap();
        assert_eq!(wal_file.seq(), 1);
        assert_eq!(wal_file.base_lsn(), 0);

        let path = wal_file.path.clone();
        drop(wal_file);

        let opened = WalFile::open(path).await.unwrap();
        assert_eq!(opened.seq(), 1);
        assert_eq!(opened.base_lsn(), 0);
    }
}
