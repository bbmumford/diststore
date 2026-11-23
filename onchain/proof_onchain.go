/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package onchain

import "golang.org/x/crypto/sha3"

// ComputeOnChainExpectedHash computes keccak256(piece || nonce) as a
// 32-byte array suitable for EVM submission.
//
// Do NOT mix this with ComputeProofHash — that one is BLAKE2b for the
// off-chain gossip envelope. See the WARNING in proof.go.
func ComputeOnChainExpectedHash(piece, nonce []byte) [32]byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(piece)
	h.Write(nonce)
	var out [32]byte
	sum := h.Sum(nil)
	copy(out[:], sum)
	return out
}
