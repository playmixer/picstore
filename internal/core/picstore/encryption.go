package picstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"

	"golang.org/x/crypto/pbkdf2"
)

const (
	aesKeySize   = 32 // AES-256
	gcmNonceSize = 12 // recommended nonce size for GCM
	saltSize     = 16 // размер соли для PBKDF2
)

// generateRandomBytes генерирует случайные байты заданного размера.
func generateRandomBytes(size int) ([]byte, error) {
	buf := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return nil, fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return buf, nil
}

// deriveKeyFromPassword создаёт ключ AES из пароля и соли с помощью PBKDF2.
func deriveKeyFromPassword(password string, salt []byte) []byte {
	return pbkdf2.Key([]byte(password), salt, 10000, aesKeySize, sha256.New)
}

// encryptData encrypts plaintext using AES-GCM with the provided key.
// Returns ciphertext, nonce, and error.
func encryptData(plaintext, key []byte) (ciphertext, nonce []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce = make([]byte, gcmNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext = gcm.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

// decryptData decrypts ciphertext using AES-GCM with the provided key and nonce.
func decryptData(ciphertext, key, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}
	return plaintext, nil
}

// EncryptFile encrypts file data using a user-provided password.
// Returns encrypted data, nonce, salt, and error.
func (p *PicStore) encryptFile(data []byte, password string) (encryptedData, nonce, salt []byte, err error) {
	if password == "" || !p.cfg.EnableEncryption {
		// encryption disabled or no password, return original data with empty metadata
		return data, nil, nil, nil
	}

	// Generate random salt
	salt, err = generateRandomBytes(saltSize)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	// Derive key from password and salt
	key := deriveKeyFromPassword(password, salt)

	// Encrypt data with derived key
	encryptedData, nonce, err = encryptData(data, key)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to encrypt data: %w", err)
	}

	return encryptedData, nonce, salt, nil
}

// DecryptFile decrypts file data using stored salt, nonce and user-provided password.
func (p *PicStore) decryptFile(encryptedData, fileNonce, salt []byte, password string) ([]byte, error) {
	if password == "" || !p.cfg.EnableEncryption || len(salt) == 0 {
		// not encrypted, return as is
		return encryptedData, nil
	}

	// Derive key from password and salt
	key := deriveKeyFromPassword(password, salt)

	// Decrypt data
	plaintext, err := decryptData(encryptedData, key, fileNonce)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt file data: %w", err)
	}
	return plaintext, nil
}
