/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

// Known-answer and cross-implementation vectors that pin the recipient-wrap
// primitives to their canonical references: the Ed25519→X25519 birational map
// against curve25519 scalar-basepoint mult, and the RFC-3394 AES Key Wrap
// against its published test vector.

package crypto

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"golang.org/x/crypto/curve25519"
)

// edPubToX25519Pub (Edwards→Montgomery map) must agree with the canonical
// definition X25519-public = clamp(SHA512(seed)[:32]) · basepoint, so the keys
// edPrivToX25519Priv / edPubToX25519Pub derive form a valid X25519 pair.
func TestEdToX25519MatchesCanonical(t *testing.T) {
	for i := 0; i < 50; i++ {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		xPriv, err := edPrivToX25519Priv(priv)
		if err != nil {
			t.Fatal(err)
		}
		xPub, err := edPubToX25519Pub(pub)
		if err != nil {
			t.Fatal(err)
		}
		derivedPub, err := curve25519.X25519(xPriv[:], curve25519.Basepoint)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(derivedPub, xPub[:]) {
			t.Fatalf("iter %d: birational pub %x != scalar*basepoint %x", i, xPub[:], derivedPub)
		}
	}
}

// RFC-3394 §4.6 known-answer vector: 256-bit KEK wrapping 256-bit key data.
func TestAESKW_RFC3394Vector(t *testing.T) {
	kek, _ := hex.DecodeString("000102030405060708090A0B0C0D0E0F101112131415161718191A1B1C1D1E1F")
	key, _ := hex.DecodeString("00112233445566778899AABBCCDDEEFF000102030405060708090A0B0C0D0E0F")
	want, _ := hex.DecodeString("28C9F404C4B810F4CBCCB35CFB87F8263F5786E2D80ED326CBC7F0E71A99F43BFB988B9B7A02DD21")

	got, err := aesKeyWrap(kek, key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("AES-KW wrap mismatch:\n got  %x\n want %x", got, want)
	}
	if len(got) != 40 {
		t.Fatalf("32-byte key should wrap to 40 bytes, got %d", len(got))
	}
	back, err := aesKeyUnwrap(kek, got)
	if err != nil {
		t.Fatalf("unwrap KAT: %v", err)
	}
	if !bytes.Equal(back, key) {
		t.Fatalf("unwrap mismatch: %x != %x", back, key)
	}
	// A single flipped byte must fail the RFC-3394 integrity check.
	bad := append([]byte(nil), got...)
	bad[10] ^= 0x01
	if _, err := aesKeyUnwrap(kek, bad); err == nil {
		t.Fatal("tampered AES-KW must fail integrity check")
	}
}

// Exercise the production NaCl path many times to catch any low-probability
// sign/cofactor edge in the birational map.
func TestNaClRoundtripMany(t *testing.T) {
	for i := 0; i < 200; i++ {
		pub, priv, _ := ed25519.GenerateKey(rand.Reader)
		dek := make([]byte, 32)
		if _, err := rand.Read(dek); err != nil {
			t.Fatal(err)
		}
		w, err := WrapDataKeyForRecipient(dek, pub)
		if err != nil {
			t.Fatal(err)
		}
		got, err := UnwrapToRecipient(w, pub, priv, EntitledAllowAll)
		if err != nil {
			t.Fatalf("iter %d unwrap: %v", i, err)
		}
		if !bytes.Equal(got, dek) {
			t.Fatalf("iter %d DEK mismatch", i)
		}
	}
}
