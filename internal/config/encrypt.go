package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type credentialPayload struct {
	Login    string `json:"login"`
	Password string `json:"password"`
	Server   string `json:"server"`
}

// getEncryptionKey membaca ENCRYPTION_KEY dari environment.
// Jika tidak ada atau ukurannya bukan 32 byte, program dihentikan dengan pesan error.
func getEncryptionKey() []byte {
	key := os.Getenv("ENCRYPTION_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "FATAL: ENCRYPTION_KEY environment variable belum di-set.")
		fmt.Fprintln(os.Stderr, "       Set key 32-byte di Secrets untuk menggunakan enkripsi kredensial.")
		os.Exit(1)
	}
	b := []byte(key)
	if len(b) != 32 {
		fmt.Fprintf(os.Stderr, "FATAL: ENCRYPTION_KEY harus tepat 32 byte, ditemukan %d byte.\n", len(b))
		os.Exit(1)
	}
	return b
}

// EncryptCredentials mengenkripsi login, password, dan server menggunakan AES-256-GCM.
// Hasilnya adalah string base64 yang aman disimpan di file konfigurasi.
func EncryptCredentials(login, password, server string) (string, error) {
	key := getEncryptionKey()

	payload := credentialPayload{Login: login, Password: password, Server: server}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal credentials: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptCredentials mendekripsi ciphertext hasil EncryptCredentials.
// Mengembalikan login, password, dan server yang asli.
func DecryptCredentials(ciphertext string) (login, password, server string, err error) {
	key := getEncryptionKey()

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", "", "", fmt.Errorf("base64 decode: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", "", fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", "", "", fmt.Errorf("ciphertext terlalu pendek")
	}

	nonce, ct := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", "", "", fmt.Errorf("decrypt: %w", err)
	}

	var p credentialPayload
	if err = json.Unmarshal(plaintext, &p); err != nil {
		return "", "", "", fmt.Errorf("unmarshal: %w", err)
	}

	return p.Login, p.Password, p.Server, nil
}
