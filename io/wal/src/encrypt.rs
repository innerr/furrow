//! AES-GCM encryption support for WAL records.
//!
//! This module provides encryption functionality using AES-256-GCM.
//! Only available when the `encryption` feature is enabled.

use crate::error::{Error, Result};

#[cfg(feature = "encryption")]
use aes_gcm::{
    aead::{Aead, KeyInit},
    Aes256Gcm, Nonce,
};

/// Encryption key (32 bytes for AES-256).
pub type EncryptionKey = [u8; 32];

/// Nonce size for AES-GCM (12 bytes).
pub const NONCE_SIZE: usize = 12;

/// Encrypts data using AES-256-GCM.
///
/// Returns encrypted data with nonce prepended (12 bytes nonce + ciphertext + 16 bytes tag).
#[cfg(feature = "encryption")]
pub fn encrypt(data: &[u8], key: &EncryptionKey) -> Result<Vec<u8>> {
    let cipher = Aes256Gcm::new_from_slice(key)
        .map_err(|e| Error::InvalidConfig(format!("invalid key: {}", e).leak()))?;

    let nonce_bytes: [u8; NONCE_SIZE] = rand::random();
    let nonce = Nonce::from_slice(&nonce_bytes);

    let ciphertext = cipher
        .encrypt(nonce, data)
        .map_err(|e| Error::InvalidRecord(format!("encryption failed: {}", e).leak()))?;

    let mut result = Vec::with_capacity(NONCE_SIZE + ciphertext.len());
    result.extend_from_slice(&nonce_bytes);
    result.extend_from_slice(&ciphertext);

    Ok(result)
}

/// Encrypts data (stub when encryption feature is disabled).
#[cfg(not(feature = "encryption"))]
pub fn encrypt(_data: &[u8], _key: &EncryptionKey) -> Result<Vec<u8>> {
    Err(Error::InvalidConfig("encryption feature not enabled"))
}

/// Decrypts data using AES-256-GCM.
///
/// Expects nonce prepended (12 bytes nonce + ciphertext + 16 bytes tag).
#[cfg(feature = "encryption")]
pub fn decrypt(encrypted: &[u8], key: &EncryptionKey) -> Result<Vec<u8>> {
    if encrypted.len() < NONCE_SIZE + 16 {
        return Err(Error::InvalidRecord("encrypted data too short"));
    }

    let cipher = Aes256Gcm::new_from_slice(key)
        .map_err(|e| Error::InvalidConfig(format!("invalid key: {}", e).leak()))?;

    let (nonce_bytes, ciphertext) = encrypted.split_at(NONCE_SIZE);
    let nonce = Nonce::from_slice(nonce_bytes);

    cipher
        .decrypt(nonce, ciphertext)
        .map_err(|e| Error::InvalidRecord(format!("decryption failed: {}", e).leak()))
}

/// Decrypts data (stub when encryption feature is disabled).
#[cfg(not(feature = "encryption"))]
pub fn decrypt(_encrypted: &[u8], _key: &EncryptionKey) -> Result<Vec<u8>> {
    Err(Error::InvalidConfig("encryption feature not enabled"))
}

/// Generates a random encryption key.
#[cfg(feature = "encryption")]
pub fn generate_key() -> EncryptionKey {
    use rand::RngCore;
    let mut key = [0u8; 32];
    rand::thread_rng().fill_bytes(&mut key);
    key
}

/// Generates a random encryption key (stub when encryption feature is disabled).
#[cfg(not(feature = "encryption"))]
pub fn generate_key() -> EncryptionKey {
    [0u8; 32]
}

#[cfg(test)]
mod tests {
    use super::*;

    #[cfg(feature = "encryption")]
    #[test]
    fn test_encrypt_decrypt() {
        let key = generate_key();
        let data = b"hello world! this is secret data.";

        let encrypted = encrypt(data, &key).unwrap();
        assert_ne!(encrypted.as_slice(), data.as_slice());
        assert!(encrypted.len() > data.len());

        let decrypted = decrypt(&encrypted, &key).unwrap();
        assert_eq!(decrypted.as_slice(), data.as_slice());
    }

    #[cfg(feature = "encryption")]
    #[test]
    fn test_encrypt_different_nonce() {
        let key = generate_key();
        let data = b"same data";

        let encrypted1 = encrypt(data, &key).unwrap();
        let encrypted2 = encrypt(data, &key).unwrap();

        // Same data should produce different ciphertext (different nonce)
        assert_ne!(encrypted1, encrypted2);

        let decrypted1 = decrypt(&encrypted1, &key).unwrap();
        let decrypted2 = decrypt(&encrypted2, &key).unwrap();
        assert_eq!(decrypted1, decrypted2);
    }

    #[cfg(feature = "encryption")]
    #[test]
    fn test_decrypt_wrong_key() {
        let key1 = generate_key();
        let key2 = generate_key();
        let data = b"secret";

        let encrypted = encrypt(data, &key1).unwrap();
        assert!(decrypt(&encrypted, &key2).is_err());
    }

    #[cfg(not(feature = "encryption"))]
    #[test]
    fn test_encrypt_disabled() {
        let key = [0u8; 32];
        let data = b"data";
        assert!(encrypt(data, &key).is_err());
    }
}
