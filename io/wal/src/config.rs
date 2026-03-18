//! Configuration types for WAL.

use std::path::PathBuf;
use std::time::Duration;

use super::error::{Error, Result};

const DEFAULT_MAX_FILE_SIZE: u64 = 64 * 1024 * 1024;
const DEFAULT_SYNC_BYTES: u64 = 4 * 1024 * 1024;
const DEFAULT_SYNC_TIME_MS: u64 = 100;

#[derive(Debug, Clone)]
pub struct WalConfig {
    pub dir: PathBuf,
    pub max_file_size: u64,
    pub max_file_age: Option<Duration>,
    pub sync_mode: SyncMode,
    pub preallocate_size: u64,
    pub create_if_missing: bool,
    pub recovery_mode: RecoveryMode,
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
            preallocate_size: DEFAULT_MAX_FILE_SIZE,
            create_if_missing: true,
            recovery_mode: RecoveryMode::TolerateTailCorruption,
        }
    }
}

impl WalConfig {
    pub fn new(dir: impl Into<PathBuf>) -> Self {
        Self {
            dir: dir.into(),
            ..Default::default()
        }
    }

    pub fn max_file_size(mut self, size: u64) -> Self {
        self.max_file_size = size;
        self
    }

    pub fn max_file_age(mut self, age: Duration) -> Self {
        self.max_file_age = Some(age);
        self
    }

    pub fn sync_mode(mut self, mode: SyncMode) -> Self {
        self.sync_mode = mode;
        self
    }

    pub fn preallocate_size(mut self, size: u64) -> Self {
        self.preallocate_size = size;
        self
    }

    pub fn create_if_missing(mut self, create: bool) -> Self {
        self.create_if_missing = create;
        self
    }

    pub fn recovery_mode(mut self, mode: RecoveryMode) -> Self {
        self.recovery_mode = mode;
        self
    }

    pub fn validate(&self) -> Result<()> {
        if self.max_file_size == 0 {
            return Err(Error::InvalidConfig("max_file_size must be > 0".into()));
        }
        if self.preallocate_size == 0 {
            return Err(Error::InvalidConfig("preallocate_size must be > 0".into()));
        }
        Ok(())
    }
}

#[derive(Debug, Clone)]
pub enum SyncMode {
    Always,
    Batch { bytes: u64, time: Duration },
    Never,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RecoveryMode {
    TolerateTailCorruption,
    Strict,
}
