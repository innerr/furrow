//! LZ4 compression support for WAL records.
//!
//! This module provides compression functionality using LZ4 algorithm.
//! Only available when the `compression` feature is enabled.

use crate::wal::error::{Error, Result};

#[cfg(feature = "compression")]
use lz4_flex::{compress_prepend_size, decompress_size_prepended};

/// Compresses data using LZ4 algorithm.
///
/// Returns the compressed data with size prepended.
/// Returns None if compression doesn't reduce size by at least 10%.
#[cfg(feature = "compression")]
pub fn compress(data: &[u8]) -> Result<Option<Vec<u8>>> {
    let compressed = compress_prepend_size(data);

    if compressed.len() < data.len() * 9 / 10 {
        Ok(Some(compressed))
    } else {
        Ok(None)
    }
}

/// Compresses data (stub when compression feature is disabled).
#[cfg(not(feature = "compression"))]
pub fn compress(_data: &[u8]) -> Result<Option<Vec<u8>>> {
    Ok(None)
}

/// Decompresses LZ4 compressed data.
#[cfg(feature = "compression")]
pub fn decompress(compressed: &[u8]) -> Result<Vec<u8>> {
    decompress_size_prepended(compressed)
        .map_err(|e| Error::InvalidRecord(format!("decompression failed: {}", e).leak()))
}

/// Decompresses data (stub when compression feature is disabled).
#[cfg(not(feature = "compression"))]
pub fn decompress(_compressed: &[u8]) -> Result<Vec<u8>> {
    Err(Error::InvalidRecord("compression feature not enabled"))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[cfg(feature = "compression")]
    #[test]
    fn test_compress_decompress() {
        let data: Vec<u8> =
            b"hello world! this is a test string that should compress well. ".repeat(10);
        let compressed = compress(&data).unwrap().unwrap();
        assert!(compressed.len() < data.len());

        let decompressed = decompress(&compressed).unwrap();
        assert_eq!(decompressed.as_slice(), data.as_slice());
    }

    #[cfg(feature = "compression")]
    #[test]
    fn test_compress_small_data() {
        let data = b"short";
        let result = compress(data).unwrap();
        if let Some(compressed) = result {
            let decompressed = decompress(&compressed).unwrap();
            assert_eq!(decompressed.as_slice(), data.as_slice());
        }
    }

    #[cfg(not(feature = "compression"))]
    #[test]
    fn test_compress_disabled() {
        let data = b"some data";
        assert!(compress(data).unwrap().is_none());
    }
}
