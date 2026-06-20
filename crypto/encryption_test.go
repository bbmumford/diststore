/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"golang.org/x/crypto/blake2b"
)

func cidOf(b []byte) string {
	sum := blake2b.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	seed := make([]byte, 32)
	rand.Read(seed)
	key, err := DeriveStorageKey(seed)
	if err != nil {
		t.Fatalf("DeriveStorageKey: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("key size: got %d want 32", len(key))
	}

	plaintext := make([]byte, 4096)
	rand.Read(plaintext)
	cid := cidOf(plaintext)

	ct, err := EncryptForStorage(key, cid, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Equal(ct, plaintext) {
		t.Fatal("ciphertext equals plaintext")
	}

	pt, err := DecryptFromStorage(key, cid, ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Fatal("decrypted bytes differ from plaintext")
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	seed := make([]byte, 32)
	rand.Read(seed)
	key, _ := DeriveStorageKey(seed)
	pt := []byte("hello world")
	cid := cidOf(pt)
	ct, _ := EncryptForStorage(key, cid, pt)
	ct[0] ^= 0x01
	if _, err := DecryptFromStorage(key, cid, ct); err == nil {
		t.Fatal("expected AEAD failure, got nil")
	}
}

func TestDecryptRejectsWrongCID(t *testing.T) {
	seed := make([]byte, 32)
	rand.Read(seed)
	key, _ := DeriveStorageKey(seed)
	pt := []byte("plaintext")
	cid := cidOf(pt)
	ct, _ := EncryptForStorage(key, cid, pt)
	otherCID := cidOf([]byte("other"))
	if _, err := DecryptFromStorage(key, otherCID, ct); err == nil {
		t.Fatal("expected failure with wrong CID, got nil")
	}
}

func TestAESGCMRoundtrip(t *testing.T) {
	key := make([]byte, AESGCMKeySize)
	rand.Read(key)
	pt := []byte("aes-gcm-plaintext")
	cid := cidOf(pt)
	c := AESGCMCipher{}
	ct, err := c.EncryptForStorage(key, cid, pt)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	round, err := c.DecryptFromStorage(key, cid, ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(round, pt) {
		t.Fatal("AES-GCM round-trip mismatch")
	}
}

func TestTenantKEKIsolation(t *testing.T) {
	seed := make([]byte, 32)
	rand.Read(seed)
	kekA, err := DeriveTenantKEK(seed, "tenant-A")
	if err != nil {
		t.Fatalf("DeriveTenantKEK A: %v", err)
	}
	kekB, err := DeriveTenantKEK(seed, "tenant-B")
	if err != nil {
		t.Fatalf("DeriveTenantKEK B: %v", err)
	}
	if bytes.Equal(kekA, kekB) {
		t.Fatal("tenant KEKs collide")
	}
	dataKey, wrapped, err := NewWrappedDataKey(kekA)
	if err != nil {
		t.Fatalf("NewWrappedDataKey: %v", err)
	}
	got, err := UnwrapDataKey(kekA, wrapped)
	if err != nil {
		t.Fatalf("UnwrapDataKey: %v", err)
	}
	if !bytes.Equal(got, dataKey) {
		t.Fatal("unwrap mismatch")
	}
	if _, err := UnwrapDataKey(kekB, wrapped); err == nil {
		t.Fatal("cross-tenant unwrap should fail")
	}
}
