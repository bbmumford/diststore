/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

// Package crypto contains the encryption + signing primitives used by
// the diststore cache, advertise, and onchain sub-packages.
//
// The Cipher interface is small and pluggable so callers can swap the
// default XChaCha20-Poly1305 impl for AES-256-GCM (or any other AEAD).
//
// Per-partition KEK isolation (tenant_kek.go) lets a single node host
// multiple partition caches whose chunks are structurally inaccessible
// across partition boundaries.
//
// Per-recipient asymmetric wrap (recipient_wrap.go) is the complementary
// model: a blob's data key is sealed to a single peer's public key with no
// shared KEK, so the wrap can ride the public advertise/gossip path and only
// the named recipient can open it.
package crypto

// Cipher abstracts the AEAD primitive used to encrypt + authenticate
// individual chunks on disk.
//
// EncryptForStorage / DecryptFromStorage MUST be deterministic per
// (key, cid) pair so the on-disk file is content-addressed under a
// stable nonce derivation — content-addressing makes (key, cid)
// safe-by-construction because changing the plaintext changes the cid.
type Cipher interface {
	// KeyLen returns the expected key length in bytes.
	KeyLen() int
	// EncryptForStorage seals plaintext under key, binding cid as AAD.
	EncryptForStorage(key []byte, cid string, plaintext []byte) ([]byte, error)
	// DecryptFromStorage opens ciphertext, returning the original
	// plaintext or an error on tag / key / cid mismatch.
	DecryptFromStorage(key []byte, cid string, ciphertext []byte) ([]byte, error)
}

// Default returns the recommended Cipher impl (XChaCha20-Poly1305).
func Default() Cipher { return XChaChaCipher{} }
