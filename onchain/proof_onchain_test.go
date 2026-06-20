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

func TestComputeOnChainExpectedHash_MatchesKeccak256(t *testing.T) {
	piece := []byte("plutus-storage-escrow-test-piece")
	if len(piece) != 32 {
		t.Fatalf("fixture length drift: piece must be 32 bytes")
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
