/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package onchain

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// ComputeOnChainExpectedHash must produce the bytes the escrow contract's
// openChallenge checks: keccak256(abi.encodePacked(piece, nonce)), where
// abi.encodePacked of two bytes arguments is their concatenation. The fixture
// is 32 ASCII bytes of piece and a nonce of 0x00..0x1F; its digest regenerates
// with sha3.NewLegacyKeccak256 written piece then nonce. A change of hash
// primitive changes this test and the contract together.
func TestComputeOnChainExpectedHash_MatchesKeccak256(t *testing.T) {
	piece := []byte("plutus-storage-escrow-test-piece")
	if len(piece) != 32 {
		t.Fatalf("fixture length drift: piece must be 32 bytes, got %d", len(piece))
	}
	nonce := make([]byte, 32)
	for i := range nonce {
		nonce[i] = byte(i)
	}
	got := ComputeOnChainExpectedHash(piece, nonce)
	expectedHex := "df42b8c35ba6dce52262fed2a414bf470ac0b69981e3a24bcaf6bc86f3625d41"
	want, err := hex.DecodeString(expectedHex)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bytes.Equal(got[:], want) {
		t.Fatalf("keccak256 mismatch:\n got  %s\n want %s",
			hex.EncodeToString(got[:]), expectedHex)
	}
}

// The on-chain hash is keccak256 and the off-chain proof hash is BLAKE2b-256;
// rewiring ComputeOnChainExpectedHash to BLAKE2b would break every on-chain
// challenge while every off-chain check still passed.
func TestComputeOnChainExpectedHash_NotBLAKE2b(t *testing.T) {
	piece := []byte("the quick brown fox")
	nonce := []byte("nonce--32-bytes-for-a-nonce-valu")
	if len(nonce) != 32 {
		t.Fatalf("fixture drift")
	}
	onChain := ComputeOnChainExpectedHash(piece, nonce)
	offChain := ComputeProofHash(piece, nonce)
	if bytes.Equal(onChain[:], offChain) {
		t.Fatal("keccak256 and BLAKE2b produced identical bytes")
	}
}

func TestComputeOnChainExpectedHash_Deterministic(t *testing.T) {
	piece := []byte("abc")
	nonce := []byte("nonce12345nonce12345nonce12345xx") // 32 bytes
	h1 := ComputeOnChainExpectedHash(piece, nonce)
	h2 := ComputeOnChainExpectedHash(piece, nonce)
	if h1 != h2 {
		t.Fatalf("non-deterministic output")
	}
}
