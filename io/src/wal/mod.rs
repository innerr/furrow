//! Write-Ahead Log (WAL) module for high-throughput concurrent writes.

mod allocator;
mod config;
mod error;
mod file;
mod reader;
mod record;
mod writer;

pub use config::{RecoveryMode, SyncMode, WalConfig};
pub use error::{Error, Result};
pub use reader::WalReader;
pub use record::Record;
pub use writer::{Wal, WalSync};
