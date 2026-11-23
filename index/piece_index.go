/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

// Package index handles canonical encoding, Ed25519 signing, and
// Merkle-root validation of chunker Manifests.
package index

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bbmumford/diststore/chunker"
)

// ErrInvalidManifest is returned when validation fails.
var ErrInvalidManifest = errors.New("index: invalid manifest")

// EncodeManifest produces canonical JSON bytes used for signing. Struct
// field order drives JSON field order, which Go guarantees is stable.
func EncodeManifest(m *chunker.Manifest) ([]byte, error) {
	if m == nil {
		return nil, errors.New("index: nil manifest")
	}
	return json.Marshal(m)
}

// DecodeManifest parses bytes produced by EncodeManifest.
func DecodeManifest(data []byte) (*chunker.Manifest, error) {
	if len(data) == 0 {
		return nil, ErrInvalidManifest
	}
	var m chunker.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	return &m, nil
}

// SignManifest returns the detached Ed25519 signature over the canonical
// JSON encoding of the manifest.
func SignManifest(m *chunker.Manifest, priv ed25519.PrivateKey) ([]byte, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, errors.New("index: bad private key size")
	}
	enc, err := EncodeManifest(m)
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(priv, enc), nil
}

// ValidateManifest verifies the signature, recomputes the Merkle root
// from ordered piece CIDs, and sanity-checks the piece shape.
//
// It does NOT re-hash chunk payloads (it has no access to them). Call
// VerifyPieces when the bytes are available alongside the manifest.
func ValidateManifest(m *chunker.Manifest, signature []byte, pubkey ed25519.PublicKey) error {
	if m == nil {
		return fmt.Errorf("%w: nil manifest", ErrInvalidManifest)
	}
	if len(pubkey) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: bad pubkey size", ErrInvalidManifest)
	}
	enc, err := EncodeManifest(m)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pubkey, enc, signature) {
		return fmt.Errorf("%w: bad signature", ErrInvalidManifest)
	}

	if m.PieceSize <= 0 {
		return fmt.Errorf("%w: zero piece size", ErrInvalidManifest)
	}
	// PieceSize used to be capped at the legacy 256KB constant.
	// Adaptive chunker may emit 4 MB pieces for large files; allow up
	// to chunker.LargePieceSize so big-band manifests still validate.
	maxPiece := chunker.PieceSize
	if maxPiece < chunker.LargePieceSize {
		maxPiece = chunker.LargePieceSize
	}
	if m.PieceSize > maxPiece {
		return fmt.Errorf("%w: piece size %d exceeds max %d",
			ErrInvalidManifest, m.PieceSize, maxPiece)
	}
	expectedCount := 0
	if m.TotalBytes > 0 {
		expectedCount = int((m.TotalBytes + int64(m.PieceSize) - 1) / int64(m.PieceSize))
	}
	if len(m.Pieces) != expectedCount {
		return fmt.Errorf("%w: piece count %d want %d",
			ErrInvalidManifest, len(m.Pieces), expectedCount)
	}

	cids := make([]string, len(m.Pieces))
	var coverage int64
	for i, p := range m.Pieces {
		if p.Index != i {
			return fmt.Errorf("%w: piece %d has wrong index %d",
				ErrInvalidManifest, i, p.Index)
		}
		if p.SizeBytes <= 0 || p.SizeBytes > m.PieceSize {
			return fmt.Errorf("%w: piece %d size %d out of range",
				ErrInvalidManifest, i, p.SizeBytes)
		}
		cids[i] = p.CID
		coverage += int64(p.SizeBytes)
	}
	if coverage != m.TotalBytes {
		return fmt.Errorf("%w: coverage %d != totalBytes %d",
			ErrInvalidManifest, coverage, m.TotalBytes)
	}
	if got := chunker.ComputeMerkleRoot(cids); got != m.RootCID {
		return fmt.Errorf("%w: root cid mismatch", ErrInvalidManifest)
	}
	return nil
}

// VerifyPieces rehashes each chunk and compares against the manifest's
// piece CIDs.
func VerifyPieces(m *chunker.Manifest, pieces [][]byte) error {
	if m == nil {
		return fmt.Errorf("%w: nil manifest", ErrInvalidManifest)
	}
	if len(pieces) != len(m.Pieces) {
		return fmt.Errorf("%w: piece count mismatch", ErrInvalidManifest)
	}
	for i, chunk := range pieces {
		if chunker.ComputePieceCID(chunk) != m.Pieces[i].CID {
			return fmt.Errorf("%w: piece %d cid mismatch", ErrInvalidManifest, i)
		}
	}
	return nil
}
