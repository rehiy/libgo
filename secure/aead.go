package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
)

// NewAESGCM creates an AES-GCM cipher with a 16-, 24- or 32-byte key.
func NewAESGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// AEADSeal encrypts plaintext and returns nonce followed by ciphertext and tag.
// additionalData is authenticated but is not included in the result.
func AEADSeal(aead cipher.AEAD, plaintext, additionalData []byte) ([]byte, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, additionalData), nil
}

// AEADOpen decrypts a nonce-prefixed payload produced by AEADSeal.
func AEADOpen(aead cipher.AEAD, payload, additionalData []byte) ([]byte, error) {
	n := aead.NonceSize()
	if len(payload) < n+aead.Overhead() {
		return nil, errors.New("invalid encrypted payload length")
	}
	return aead.Open(nil, payload[:n], payload[n:], additionalData)
}
