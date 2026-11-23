/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

// StorageKeyContext is the HKDF context string for deriving the on-disk
// storage key from the node's identity private key.
const StorageKeyContext = "plutus-blob-storage-v1"

// ErrShortCiphertext is returned when ciphertext is too small to contain
// the AEAD tag.
var ErrShortCiphertext = errors.New("crypto: ciphertext too short")

// DeriveStorageKey produces a 32-byte XChaCha20-Poly1305 key from a
// node's identity material via HKDF-SHA256. The input may be a 32-byte
// Ed25519 seed or the full 64-byte private key — HKDF treats it as
// opaque IKM.
func DeriveStorageKey(identityPriv []byte) ([]byte, error) {
	if len(identityPriv) == 0 {
		return nil, errors.New("crypto: empty identity key")
	}
	h := hkdf.New(sha256.New, identityPriv, nil, []byte(StorageKeyContext))
	key := make([]byte, chacha20poly1305.KeySize) // 32
	if _, err := h.Read(key); err != nil {
		return nil, fmt.Errorf("crypto: hkdf: %w", err)
	}
	return key, nil
}

// nonceForCID derives a 24-byte XChaCha20-Poly1305 nonce from the first
// 24 bytes of BLAKE2b-256(cid). Deterministic — content-addressed CIDs
// guarantee (key, cid) → nonce is safe.
func nonceForCID(cid string) []byte {
	raw, err := hex.DecodeString(cid)
	if err != nil || len(raw) == 0 {
		raw = []byte(cid)
	}
	sum := blake2b.Sum256(raw)
	nonce := make([]byte, chacha20poly1305.NonceSizeX) // 24
	copy(nonce, sum[:])
	return nonce
}

// XChaChaCipher is the default Cipher impl using XChaCha20-Poly1305.
type XChaChaCipher struct{}

// KeyLen implements Cipher.
func (XChaChaCipher) KeyLen() int { return chacha20poly1305.KeySize }

// EncryptForStorage seals plaintext using XChaCha20-Poly1305 with CID
// bound as AAD.
func (XChaChaCipher) EncryptForStorage(key []byte, cid string, plaintext []byte) ([]byte, error) {
	return EncryptForStorage(key, cid, plaintext)
}

// DecryptFromStorage reverses EncryptForStorage.
func (XChaChaCipher) DecryptFromStorage(key []byte, cid string, ciphertext []byte) ([]byte, error) {
	return DecryptFromStorage(key, cid, ciphertext)
}

// EncryptForStorage is the package-level helper for callers that
// have not constructed a Cipher value and want one-shot XChaCha20-
// Poly1305 encryption keyed by CID-derived nonce.
func EncryptForStorage(key []byte, cid string, plaintext []byte) ([]byte, error) {
	if len(key) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("crypto: bad key size %d", len(key))
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: aead: %w", err)
	}
	nonce := nonceForCID(cid)
	return aead.Seal(nil, nonce, plaintext, []byte(cid)), nil
}

// DecryptFromStorage reverses EncryptForStorage.
func DecryptFromStorage(key []byte, cid string, ciphertext []byte) ([]byte, error) {
	if len(key) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("crypto: bad key size %d", len(key))
	}
	if len(ciphertext) < chacha20poly1305.Overhead {
		return nil, ErrShortCiphertext
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: aead: %w", err)
	}
	nonce := nonceForCID(cid)
	pt, err := aead.Open(nil, nonce, ciphertext, []byte(cid))
	if err != nil {
		return nil, fmt.Errorf("crypto: decrypt: %w", err)
	}
	return pt, nil
}
