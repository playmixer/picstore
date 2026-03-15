package picstore

import (
	"fmt"
	"testing"
)

func TestEncryption(t *testing.T) {
	// Создаём mock PicStore с включённым шифрованием
	cfg := Config{
		EnableEncryption: true,
	}
	p := &PicStore{
		cfg: cfg,
	}

	password := "mysecret"
	data := []byte("Hello, this is a test image data!")

	// Шифруем
	encrypted, nonce, salt, err := p.encryptFile(data, password)
	if err != nil {
		t.Fatalf("encryptFile failed: %v", err)
	}
	if len(encrypted) == 0 {
		t.Fatal("encrypted data is empty")
	}
	if len(nonce) != gcmNonceSize {
		t.Fatalf("nonce size mismatch: got %d, want %d", len(nonce), gcmNonceSize)
	}
	if salt != nil {
		t.Fatal("salt should be nil in simplified version")
	}

	// Дешифруем
	decrypted, err := p.decryptFile(encrypted, nonce, nil, password)
	if err != nil {
		t.Fatalf("decryptFile failed: %v", err)
	}
	if string(decrypted) != string(data) {
		t.Fatalf("decrypted data mismatch: got %q, want %q", decrypted, data)
	}

	// Неверный пароль
	_, err = p.decryptFile(encrypted, nonce, nil, "wrongpassword")
	if err == nil {
		t.Fatal("decryptFile with wrong password should fail")
	}

	// Пустой пароль (без шифрования)
	encrypted2, nonce2, salt2, err := p.encryptFile(data, "")
	if err != nil {
		t.Fatalf("encryptFile with empty password failed: %v", err)
	}
	if string(encrypted2) != string(data) {
		t.Fatal("empty password should return original data")
	}
	if nonce2 != nil || salt2 != nil {
		t.Fatal("nonce and salt should be nil when password empty")
	}

	fmt.Println("All tests passed")
}
