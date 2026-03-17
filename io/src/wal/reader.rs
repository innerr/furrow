//! WAL reader for recovery.

use std::path::PathBuf;

use super::error::Result;
use super::file::{self, FILE_HEADER_SIZE};
use super::record::{Record, RecordHeader, HEADER_SIZE};

#[derive(Debug)]
pub struct WalReader {
    dir: PathBuf,
}

impl WalReader {
    pub fn new(dir: PathBuf) -> Result<Self> {
        Ok(Self { dir })
    }

    pub fn iter(&self) -> RecordIter {
        RecordIter::new(self.dir.clone())
    }

    pub fn iter_ordered(&self) -> impl Iterator<Item = Result<Record>> {
        let mut records: Vec<_> = self.iter().filter_map(|r| r.ok()).collect();
        records.sort_by_key(|r| r.lsn);
        records.into_iter().map(Ok)
    }
}

pub struct RecordIter {
    files: Vec<(u32, PathBuf)>,
    file_index: usize,
    current_file: Option<std::fs::File>,
    current_file_size: u64,
    current_offset: u64,
}

impl RecordIter {
    fn new(dir: PathBuf) -> Self {
        let files: Vec<(u32, PathBuf)> = std::fs::read_dir(&dir)
            .ok()
            .map(|entries| {
                entries
                    .filter_map(|e| e.ok())
                    .filter_map(|e| {
                        let name = e.file_name().to_string_lossy().to_string();
                        file::parse_filename(&name).map(|seq| (seq, e.path()))
                    })
                    .collect()
            })
            .unwrap_or_default();

        let mut files: Vec<_> = files.into_iter().collect();
        files.sort_by_key(|(seq, _)| *seq);

        Self {
            files,
            file_index: 0,
            current_file: None,
            current_file_size: 0,
            current_offset: 0,
        }
    }
}

impl Iterator for RecordIter {
    type Item = Result<Record>;

    fn next(&mut self) -> Option<Self::Item> {
        loop {
            if self.current_file.is_none() {
                if self.file_index >= self.files.len() {
                    return None;
                }

                let (_, path) = &self.files[self.file_index];
                match std::fs::File::open(path) {
                    Ok(f) => {
                        if let Ok(m) = f.metadata() {
                            self.current_file = Some(f);
                            self.current_file_size = m.len();
                            self.current_offset = FILE_HEADER_SIZE;
                            self.file_index += 1;
                        } else {
                            self.file_index += 1;
                            continue;
                        }
                    }
                    Err(_) => {
                        self.file_index += 1;
                        continue;
                    }
                }
            }

            let file = self.current_file.as_mut()?;

            if self.current_offset + HEADER_SIZE as u64 > self.current_file_size {
                self.current_file = None;
                continue;
            }

            use std::io::{Read, Seek, SeekFrom};

            let mut header_buf = [0u8; HEADER_SIZE];
            if file.seek(SeekFrom::Start(self.current_offset)).is_err() {
                self.current_file = None;
                continue;
            }

            if file.read_exact(&mut header_buf).is_err() {
                self.current_file = None;
                continue;
            }

            let header = match RecordHeader::decode(&header_buf) {
                Ok(h) => h,
                Err(_) => {
                    self.current_file = None;
                    continue;
                }
            };

            let record_end = self.current_offset + HEADER_SIZE as u64 + header.len as u64;
            if record_end > self.current_file_size {
                self.current_file = None;
                continue;
            }

            let mut data = vec![0u8; header.len as usize];
            if file.read_exact(&mut data).is_err() {
                self.current_file = None;
                continue;
            }

            self.current_offset = record_end;

            return Some(Ok(Record::new(header.lsn, data)));
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::wal::config::{SyncMode, WalConfig};
    use crate::wal::writer::Wal;
    use tempfile::tempdir;

    #[tokio::test]
    async fn test_reader_iter() {
        let dir = tempdir().unwrap();
        let config = WalConfig::new(dir.path()).sync_mode(SyncMode::Always);

        let wal = Wal::open(config).await.unwrap();

        wal.write(b"first").await.unwrap();
        wal.write(b"second").await.unwrap();
        wal.write(b"third").await.unwrap();

        wal.close().await.unwrap();

        let reader = WalReader::new(dir.path().to_path_buf()).unwrap();
        let records: Vec<_> = reader.iter().collect();

        assert_eq!(records.len(), 3);
        assert_eq!(records[0].as_ref().unwrap().data, b"first");
        assert_eq!(records[1].as_ref().unwrap().data, b"second");
        assert_eq!(records[2].as_ref().unwrap().data, b"third");
    }

    #[tokio::test]
    async fn test_reader_ordered() {
        let dir = tempdir().unwrap();
        let config = WalConfig::new(dir.path()).sync_mode(SyncMode::Always);

        let wal = Wal::open(config).await.unwrap();

        wal.write(b"a").await.unwrap();
        wal.write(b"b").await.unwrap();
        wal.write(b"c").await.unwrap();

        wal.close().await.unwrap();

        let reader = WalReader::new(dir.path().to_path_buf()).unwrap();
        let records: Vec<_> = reader.iter_ordered().map(|r| r.unwrap()).collect();

        assert_eq!(records.len(), 3);
        assert!(records[0].lsn <= records[1].lsn);
        assert!(records[1].lsn <= records[2].lsn);
    }
}
