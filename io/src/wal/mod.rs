//! Write-Ahead Log (WAL) module for high-throughput concurrent writes.

mod allocator;
mod config;
mod error;
mod file;
mod reader;
mod record;
mod writer;

#[cfg(feature = "compression")]
mod compress;

#[cfg(feature = "encryption")]
mod encrypt;

pub use config::{RecoveryMode, SyncMode, WalConfig};
pub use error::{Error, Result};
pub use reader::WalReader;
pub use record::Record;
pub use writer::{Wal, WalSync};

#[cfg(feature = "compression")]
pub use compress::{compress, decompress};

#[cfg(feature = "encryption")]
pub use encrypt::{decrypt, encrypt, generate_key, EncryptionKey};
