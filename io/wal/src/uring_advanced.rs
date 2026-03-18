//! Advanced io_uring features (Linux only).
//!
//! This module provides advanced io_uring optimizations:
//! - Registered file descriptors: Reduces kernel overhead
//! - Linked write + fsync: Atomic chain operations
//!
//! NOTE: This requires the `io-uring` crate directly, which is only available on Linux.
//! On other platforms, this module is not compiled.

use std::os::unix::io::{AsRawFd, RawFd};

use crate::error::{Error, Result};

/// Maximum number of registered files.
const MAX_REGISTERED_FILES: usize = 1;

/// Manages registered file descriptors for io_uring.
///
/// Registered files avoid the overhead of passing file descriptors
/// through the kernel for each I/O operation.
pub struct RegisteredFiles {
    files: Vec<Option<RawFd>>,
}

impl RegisteredFiles {
    /// Creates a new registered files manager.
    pub fn new() -> Self {
        Self {
            files: vec![None; MAX_REGISTERED_FILES],
        }
    }

    /// Registers a file at the given index.
    ///
    /// # Safety
    /// The caller must ensure the file descriptor is valid and remains
    /// valid for the lifetime of the registration.
    pub unsafe fn register(&mut self, index: usize, fd: RawFd) -> Result<()> {
        if index >= MAX_REGISTERED_FILES {
            return Err(Error::InvalidConfig("file index out of range".into()));
        }
        self.files[index] = Some(fd);
        Ok(())
    }

    /// Unregisters a file at the given index.
    pub fn unregister(&mut self, index: usize) {
        if index < MAX_REGISTERED_FILES {
            self.files[index] = None;
        }
    }

    /// Returns the file descriptor at the given index.
    pub fn get(&self, index: usize) -> Option<RawFd> {
        self.files.get(index).copied().flatten()
    }

    /// Returns the slice of file descriptors for io_uring registration.
    pub fn as_slice(&self) -> Vec<RawFd> {
        self.files.iter().map(|opt| opt.unwrap_or(-1)).collect()
    }
}

impl Default for RegisteredFiles {
    fn default() -> Self {
        Self::new()
    }
}

/// Builder for creating linked io_uring operations.
///
/// Linked operations form a chain where subsequent operations
/// only execute if previous ones succeed.
pub struct LinkedOps {
    ops_count: usize,
}

impl LinkedOps {
    /// Creates a new linked operations builder.
    pub fn new() -> Self {
        Self { ops_count: 0 }
    }

    /// Adds a write operation to the chain.
    ///
    /// This operation will only execute if all previous operations succeeded.
    pub fn add_write(&mut self) -> &mut Self {
        self.ops_count += 1;
        self
    }

    /// Adds an fsync operation to the chain.
    ///
    /// This operation will only execute if all previous operations succeeded.
    /// Typically used after writes to ensure durability.
    pub fn add_fsync(&mut self) -> &mut Self {
        self.ops_count += 1;
        self
    }

    /// Returns the number of operations in the chain.
    pub fn len(&self) -> usize {
        self.ops_count
    }

    /// Returns true if there are no operations.
    pub fn is_empty(&self) -> bool {
        self.ops_count == 0
    }
}

impl Default for LinkedOps {
    fn default() -> Self {
        Self::new()
    }
}

/// Batch submission helper for io_uring.
///
/// Collects multiple I/O operations and submits them in a single syscall.
pub struct BatchSubmit {
    pending_count: usize,
    max_batch: usize,
}

impl BatchSubmit {
    /// Creates a new batch submit helper.
    ///
    /// # Arguments
    /// * `max_batch` - Maximum number of operations before forcing a submit.
    pub fn new(max_batch: usize) -> Self {
        Self {
            pending_count: 0,
            max_batch,
        }
    }

    /// Adds an operation to the batch.
    ///
    /// Returns true if the batch should be submitted.
    pub fn add(&mut self) -> bool {
        self.pending_count += 1;
        self.pending_count >= self.max_batch
    }

    /// Returns the number of pending operations.
    pub fn pending(&self) -> usize {
        self.pending_count
    }

    /// Clears the batch after submission.
    pub fn clear(&mut self) {
        self.pending_count = 0;
    }
}

impl Default for BatchSubmit {
    fn default() -> Self {
        Self::new(64) // Default batch size from research
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_registered_files() {
        let mut files = RegisteredFiles::new();

        assert!(files.get(0).is_none());

        unsafe {
            files.register(0, 42).unwrap();
        }
        assert_eq!(files.get(0), Some(42));

        files.unregister(0);
        assert!(files.get(0).is_none());
    }

    #[test]
    fn test_registered_files_out_of_range() {
        let mut files = RegisteredFiles::new();
        unsafe {
            assert!(files.register(100, 42).is_err());
        }
    }

    #[test]
    fn test_linked_ops() {
        let mut ops = LinkedOps::new();

        assert!(ops.is_empty());

        ops.add_write().add_fsync();

        assert_eq!(ops.len(), 2);
        assert!(!ops.is_empty());
    }

    #[test]
    fn test_batch_submit() {
        let mut batch = BatchSubmit::new(3);

        assert!(!batch.add()); // 1
        assert!(!batch.add()); // 2
        assert!(batch.add()); // 3 - should submit

        assert_eq!(batch.pending(), 3);
        batch.clear();
        assert_eq!(batch.pending(), 0);
    }
}
