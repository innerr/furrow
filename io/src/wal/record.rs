//! Record format for WAL.

use bytes::{BufMut, BytesMut};
use crc32fast::Hasher;

use super::error::{Error, Result};

pub const HEADER_SIZE: usize = 26;
pub const MAX_RECORD_SIZE: usize = 16 * 1024 * 1024;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(u8)]
pub enum RecordType {
    Full = 0,
    First = 1,
    Middle = 2,
    Last = 3,
}

impl TryFrom<u8> for RecordType {
    type Error = Error;

    fn try_from(value: u8) -> Result<Self> {
        match value {
            0 => Ok(RecordType::Full),
            1 => Ok(RecordType::First),
            2 => Ok(RecordType::Middle),
            3 => Ok(RecordType::Last),
            _ => Err(Error::InvalidRecord("invalid record type")),
        }
    }
}

bitflags::bitflags! {
    #[derive(Debug, Clone, Copy, PartialEq, Eq)]
    pub struct RecordFlags: u8 {
        const NONE = 0;
        const COMPRESSED = 1 << 0;
        const ENCRYPTED = 1 << 1;
    }
}

#[derive(Debug, Clone, Copy)]
#[allow(dead_code)]
pub struct RecordHeader {
    pub crc: u32,
    pub len: u32,
    pub lsn: u64,
    pub record_type: RecordType,
    pub flags: RecordFlags,
}

impl RecordHeader {
    pub fn decode(buf: &[u8]) -> Result<Self> {
        if buf.len() < HEADER_SIZE {
            return Err(Error::InvalidRecord("header too short"));
        }

        let crc = u32::from_le_bytes([buf[0], buf[1], buf[2], buf[3]]);
        let len = u32::from_le_bytes([buf[4], buf[5], buf[6], buf[7]]);
        let lsn = u64::from_le_bytes([
            buf[8], buf[9], buf[10], buf[11], buf[12], buf[13], buf[14], buf[15],
        ]);
        let record_type = RecordType::try_from(buf[16])?;
        let flags = RecordFlags::from_bits_truncate(buf[17]);

        Ok(Self {
            crc,
            len,
            lsn,
            record_type,
            flags,
        })
    }
}

#[derive(Debug, Clone)]
pub struct Record {
    pub lsn: u64,
    pub data: Vec<u8>,
}

impl Record {
    pub fn new(lsn: u64, data: Vec<u8>) -> Self {
        Self { lsn, data }
    }

    pub fn encode(&self) -> Result<BytesMut> {
        let total_size = HEADER_SIZE + self.data.len();
        if total_size > MAX_RECORD_SIZE {
            return Err(Error::RecordTooLarge {
                size: total_size,
                max: MAX_RECORD_SIZE,
            });
        }

        let mut buf = BytesMut::with_capacity(total_size);

        let crc = compute_crc(&self.data);
        let len = self.data.len() as u32;

        buf.put_u32_le(crc);
        buf.put_u32_le(len);
        buf.put_u64_le(self.lsn);
        buf.put_u8(RecordType::Full as u8);
        buf.put_u8(RecordFlags::NONE.bits());
        buf.put_slice(&[0u8; 8]);
        buf.put_slice(&self.data);

        Ok(buf)
    }
}

fn compute_crc(data: &[u8]) -> u32 {
    let mut hasher = Hasher::new();
    hasher.update(data);
    hasher.finalize()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_record_encode() {
        let record = Record::new(12345, b"hello world".to_vec());
        let encoded = record.encode().unwrap();

        assert!(encoded.len() >= HEADER_SIZE);
        assert_eq!(encoded.len(), HEADER_SIZE + b"hello world".len());
    }

    #[test]
    fn test_record_too_large() {
        let large_data = vec![0u8; MAX_RECORD_SIZE + 1];
        let record = Record::new(1, large_data);
        assert!(record.encode().is_err());
    }

    #[test]
    fn test_record_header_decode() {
        let record = Record::new(12345, b"test".to_vec());
        let encoded = record.encode().unwrap();
        let header = RecordHeader::decode(&encoded).unwrap();

        assert_eq!(header.lsn, 12345);
        assert_eq!(header.len, 4);
    }
}
