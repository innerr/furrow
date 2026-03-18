//! Atomic LSN and offset allocation.

use std::sync::atomic::{AtomicU64, Ordering};

#[allow(dead_code)]
#[derive(Debug)]
pub struct Allocator {
    next_lsn: AtomicU64,
    next_offset: AtomicU64,
}

#[allow(dead_code)]
impl Allocator {
    pub fn new(start_lsn: u64, start_offset: u64) -> Self {
        Self {
            next_lsn: AtomicU64::new(start_lsn),
            next_offset: AtomicU64::new(start_offset),
        }
    }

    pub fn allocate_lsn(&mut self) -> u64 {
        self.next_lsn.fetch_add(1, Ordering::Relaxed)
    }

    pub fn allocate_offset(&mut self, size: u64) -> u64 {
        self.next_offset.fetch_add(size, Ordering::Relaxed)
    }

    pub fn current_lsn(&self) -> u64 {
        self.next_lsn.load(Ordering::Relaxed)
    }

    pub fn current_offset(&self) -> u64 {
        self.next_offset.load(Ordering::Relaxed)
    }

    pub fn set_offset(&mut self, offset: u64) {
        self.next_offset.store(offset, Ordering::Relaxed);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_allocate() {
        let mut allocator = Allocator::new(0, 0);

        let lsn1 = allocator.allocate_lsn();
        let offset1 = allocator.allocate_offset(100);

        let lsn2 = allocator.allocate_lsn();
        let offset2 = allocator.allocate_offset(200);

        assert_eq!(lsn1, 0);
        assert_eq!(offset1, 0);
        assert_eq!(lsn2, 1);
        assert_eq!(offset2, 100);
    }
}
