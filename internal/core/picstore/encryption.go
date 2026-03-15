package picstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
)

const (
	aesKeySize   = 32 // AES-256
	gcmNonceSize = 12 // recommended nonce size for GCM
)

// generateRandomBytes генерирует случайные байты заданного размера.
func generateRandomBytes(size int) ([]byte, error) {
	buf := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return nil, fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return buf, nil
}

// deriveKeyFromPassword создаёт ключ AES из пароля с помощью SHA-256.
func deriveKeyFromPassword(password string) []byte {
	hash := sha256.Sum256([]byte(password))
	return hash[:]
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

// encryptFile encrypts file data using a user-provided password.
// Returns encrypted data, nonce, salt, and error.
// Salt is kept as nil for compatibility.
func (p *PicStore) encryptFile(data []byte, password string) (encryptedData, nonce, salt []byte, err error) {
	if password == "" || !p.cfg.EnableEncryption {
		// encryption disabled or no password, return original data with empty metadata
		return data, nil, nil, nil
	}

	// Derive key from password using SHA-256
	key := deriveKeyFromPassword(password)

	// Encrypt data with derived key
	encryptedData, nonce, err = encryptData(data, key)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to encrypt data: %w", err)
	}

	// Return nil salt for compatibility
	return encryptedData, nonce, nil, nil
}

// decryptFile decrypts file data using stored salt, nonce and user-provided password.
// Salt is ignored in this simplified version.
func (p *PicStore) decryptFile(encryptedData, fileNonce, salt []byte, password string) ([]byte, error) {
	if password == "" || !p.cfg.EnableEncryption {
		// not encrypted, return as is
		return encryptedData, nil
	}

	// Derive key from password using SHA-256
	key := deriveKeyFromPassword(password)

	// Decrypt data
	plaintext, err := decryptData(encryptedData, key, fileNonce)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt file data: %w", err)
	}
	return plaintext, nil
}
