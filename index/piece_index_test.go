/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package index

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/bbmumford/diststore/chunker"
)

func TestSignAndValidateManifest(t *testing.T) {
	data := make([]byte, 512*1024)
	rand.Read(data)
	m, _, err := chunker.Split(data, 0)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sig, err := SignManifest(m, priv)
	if err != nil {
		t.Fatalf("SignManifest: %v", err)
	}
	if err := ValidateManifest(m, sig, pub); err != nil {
		t.Fatalf("ValidateManifest: %v", err)
	}
}

func TestValidateManifestTamperedRoot(t *testing.T) {
	data := make([]byte, 512*1024)
	rand.Read(data)
	m, _, _ := chunker.Split(data, 0)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	sig, _ := SignManifest(m, priv)
	orig := m.RootCID
	runes := []byte(m.RootCID)
	runes[0] ^= 0x01
	m.RootCID = string(runes)
	if err := ValidateManifest(m, sig, pub); err == nil {
		t.Fatal("expected failure on tampered root, got nil")
	}
	m.RootCID = orig
}

func TestValidateManifestTamperedSignature(t *testing.T) {
	data := make([]byte, 200*1024)
	rand.Read(data)
	m, _, _ := chunker.Split(data, 0)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	sig, _ := SignManifest(m, priv)
	sig[0] ^= 0x01
	if err := ValidateManifest(m, sig, pub); err == nil {
		t.Fatal("expected failure on tampered sig, got nil")
	}
}

func TestEncodeDecodeRoundtrip(t *testing.T) {
	data := make([]byte, 100*1024)
	rand.Read(data)
	m, _, _ := chunker.Split(data, 0)
	enc, err := EncodeManifest(m)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := DecodeManifest(enc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.RootCID != m.RootCID || got.TotalBytes != m.TotalBytes {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, m)
	}
}
