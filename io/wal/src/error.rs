//! Error types for WAL operations.

use std::io;
use thiserror::Error;

#[derive(Debug, Error)]
pub enum Error {
    #[error("IO error: {0}")]
    Io(#[from] io::Error),

    #[error("CRC mismatch")]
    CrcMismatch,

    #[error("Invalid record: {0}")]
    InvalidRecord(String),

    #[error("Invalid file header: {0}")]
    InvalidHeader(String),

    #[error("File not found: {0}")]
    FileNotFound(String),

    #[error("WAL is closed")]
    Closed,

    #[error("Invalid configuration: {0}")]
    InvalidConfig(String),

    #[error("Record too large: size={size}, max={max}")]
    RecordTooLarge { size: usize, max: usize },

    #[error("Corrupted record at offset {offset}")]
    CorruptedRecord { offset: u64 },

    #[error("No valid WAL files found")]
    NoWalFiles,

    #[error("Invalid LSN: {0}")]
    InvalidLsn(String),

    #[error("Transaction not found: {0}")]
    TransactionNotFound(u64),
}

impl From<Error> for io::Error {
    fn from(err: Error) -> Self {
        match err {
            Error::Io(e) => e,
            _ => io::Error::other(err),
        }
    }
}

pub type Result<T> = std::result::Result<T, Error>;
