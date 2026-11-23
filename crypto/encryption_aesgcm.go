/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"

	"golang.org/x/crypto/blake2b"
)

// AESGCMCipher is an alternative Cipher impl using AES-256-GCM. Provided
// for environments where AES-NI is preferred over ChaCha (e.g. server
// CPUs with hardware acceleration).
type AESGCMCipher struct{}

// AESGCMKeySize is the 256-bit key length required by AES-256.
const AESGCMKeySize = 32

// KeyLen implements Cipher.
func (AESGCMCipher) KeyLen() int { return AESGCMKeySize }

// aesGCMNonceFor derives a 12-byte AES-GCM nonce from BLAKE2b(cid).
// AES-GCM nonces are 96 bits; we take the first 12 bytes of the hash.
func aesGCMNonceFor(cid string) []byte {
	sum := blake2b.Sum256([]byte(cid))
	nonce := make([]byte, 12)
	copy(nonce, sum[:12])
	return nonce
}

// EncryptForStorage implements Cipher.
func (AESGCMCipher) EncryptForStorage(key []byte, cid string, plaintext []byte) ([]byte, error) {
	if len(key) != AESGCMKeySize {
		return nil, fmt.Errorf("crypto: bad AES-GCM key size %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: aes: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: gcm: %w", err)
	}
	return aead.Seal(nil, aesGCMNonceFor(cid), plaintext, []byte(cid)), nil
}

// DecryptFromStorage implements Cipher.
func (AESGCMCipher) DecryptFromStorage(key []byte, cid string, ciphertext []byte) ([]byte, error) {
	if len(key) != AESGCMKeySize {
		return nil, fmt.Errorf("crypto: bad AES-GCM key size %d", len(key))
	}
	if len(ciphertext) < 16 {
		return nil, errors.New("crypto: AES-GCM ciphertext too short")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: aes: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: gcm: %w", err)
	}
	pt, err := aead.Open(nil, aesGCMNonceFor(cid), ciphertext, []byte(cid))
	if err != nil {
		return nil, fmt.Errorf("crypto: AES-GCM decrypt: %w", err)
	}
	return pt, nil
}
