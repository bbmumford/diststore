/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

// PartitionKEKContext is the HKDF context string for deriving a per-partition
// Key-Encryption-Key (KEK) from the node's identity material.
const PartitionKEKContext = "diststore-tenant-kek-v1"

// DeriveTenantKEK produces a 32-byte KEK scoped to a tenant ID. Cross-
// tenant chunk reads are structurally impossible because every tenant's
// per-blob data keys are wrapped under a distinct KEK.
func DerivePartitionKEK(identityPriv []byte, partitionID string) ([]byte, error) {
	if len(identityPriv) == 0 {
		return nil, errors.New("crypto: empty identity key")
	}
	if partitionID == "" {
		return nil, errors.New("crypto: empty tenant id")
	}
	info := []byte(PartitionKEKContext + "|" + partitionID)
	h := hkdf.New(sha256.New, identityPriv, nil, info)
	kek := make([]byte, chacha20poly1305.KeySize)
	if _, err := h.Read(kek); err != nil {
		return nil, fmt.Errorf("crypto: tenant hkdf: %w", err)
	}
	return kek, nil
}

// WrappedDataKey is the per-blob data key wrapped under a partition KEK.
// The wrap is an XChaCha20-Poly1305 seal so a leaked wrap on its own
// is useless without the KEK.
type WrappedDataKey struct {
	// Wrapped is the AEAD-sealed bytes of the 32-byte data key.
	Wrapped []byte
	// Nonce is the 24-byte nonce used to seal the data key.
	Nonce []byte
}

// NewWrappedDataKey generates a fresh random 32-byte data key, wraps it
// under the partition KEK, and returns both the wrapped form (for storage
// alongside the manifest) and the unwrapped key (for immediate use).
func NewWrappedDataKey(partitionKEK []byte) (key []byte, wrapped WrappedDataKey, err error) {
	if len(partitionKEK) != chacha20poly1305.KeySize {
		return nil, WrappedDataKey{}, fmt.Errorf("crypto: tenant KEK size %d", len(partitionKEK))
	}
	key = make([]byte, chacha20poly1305.KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, WrappedDataKey{}, fmt.Errorf("crypto: rand: %w", err)
	}
	aead, err := chacha20poly1305.NewX(partitionKEK)
	if err != nil {
		return nil, WrappedDataKey{}, fmt.Errorf("crypto: aead: %w", err)
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return nil, WrappedDataKey{}, fmt.Errorf("crypto: rand nonce: %w", err)
	}
	wrapped = WrappedDataKey{
		Wrapped: aead.Seal(nil, nonce, key, nil),
		Nonce:   nonce,
	}
	return key, wrapped, nil
}

// UnwrapDataKey unwraps a previously-wrapped data key.
func UnwrapDataKey(partitionKEK []byte, w WrappedDataKey) ([]byte, error) {
	if len(partitionKEK) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("crypto: tenant KEK size %d", len(partitionKEK))
	}
	if len(w.Nonce) != chacha20poly1305.NonceSizeX {
		return nil, fmt.Errorf("crypto: wrapped-key nonce size %d", len(w.Nonce))
	}
	aead, err := chacha20poly1305.NewX(partitionKEK)
	if err != nil {
		return nil, fmt.Errorf("crypto: aead: %w", err)
	}
	key, err := aead.Open(nil, w.Nonce, w.Wrapped, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: unwrap: %w", err)
	}
	return key, nil
}
