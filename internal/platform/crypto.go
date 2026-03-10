package platform

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

type SealedSecret struct {
	ContentSHA256 string
	Ciphertext    []byte
	CipherNonce   []byte
	EncryptedDEK  []byte
	DEKNonce      []byte
	Algorithm     string
	KeyVersion    string
}

func SealSecret(masterKey, plaintext []byte) (SealedSecret, error) {
	if len(masterKey) == 0 {
		return SealedSecret{}, fmt.Errorf("platform: master key is empty")
	}
	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return SealedSecret{}, fmt.Errorf("platform: read random DEK: %w", err)
	}
	ciphertext, cipherNonce, err := sealWithKey(dek, plaintext)
	if err != nil {
		return SealedSecret{}, err
	}
	encryptedDEK, dekNonce, err := sealWithKey(deriveKey(masterKey), dek)
	if err != nil {
		return SealedSecret{}, err
	}
	hash := sha256.Sum256(plaintext)
	return SealedSecret{
		ContentSHA256: hex.EncodeToString(hash[:]),
		Ciphertext:    ciphertext,
		CipherNonce:   cipherNonce,
		EncryptedDEK:  encryptedDEK,
		DEKNonce:      dekNonce,
		Algorithm:     "aes-256-gcm",
		KeyVersion:    "env-sha256-v1",
	}, nil
}

func OpenSecret(masterKey []byte, secret SealedSecret) ([]byte, error) {
	dek, err := openWithKey(deriveKey(masterKey), secret.EncryptedDEK, secret.DEKNonce)
	if err != nil {
		return nil, err
	}
	return openWithKey(dek, secret.Ciphertext, secret.CipherNonce)
}

func deriveKey(masterKey []byte) []byte {
	sum := sha256.Sum256(masterKey)
	out := make([]byte, len(sum))
	copy(out, sum[:])
	return out
}

func sealWithKey(key, plaintext []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("platform: create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("platform: create gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("platform: read nonce: %w", err)
	}
	return gcm.Seal(nil, nonce, plaintext, nil), nonce, nil
}

func openWithKey(key, ciphertext, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("platform: create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("platform: create gcm: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("platform: open secret: %w", err)
	}
	return plaintext, nil
}
