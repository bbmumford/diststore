/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

// Package chunker turns plaintext bytes into ordered CID-addressed
// chunks + a Merkle-rooted manifest, and reassembles them back.
//
// Default piece size is 256 KB (PieceSize); the optional adaptive
// policy (see adaptive.go) varies size based on total bytes.
package chunker

import (
	"encoding/hex"
	"errors"
	"fmt"

	"golang.org/x/crypto/blake2b"
)

// PieceSize is the default fixed chunk size.
const PieceSize = 256 * 1024 // 256 KB

// DefaultMaxFileSizeBytes is the file-size cap (50 MB = 200 × 256 KB).
const DefaultMaxFileSizeBytes int64 = 50 * 1024 * 1024

// ErrFileTooLarge is returned when Split receives bytes above the cap.
var ErrFileTooLarge = errors.New("chunker: file exceeds max size")

// PieceRef describes one chunk in a manifest.
type PieceRef struct {
	Index     int    `json:"index"`
	CID       string `json:"cid"`
	SizeBytes int    `json:"sizeBytes"`
}

// Manifest is the piece list that describes a logical file.
type Manifest struct {
	RootCID    string     `json:"rootCid"`
	Pieces     []PieceRef `json:"pieces"`
	TotalBytes int64      `json:"totalBytes"`
	PieceSize  int        `json:"pieceSize"`
}

// ComputePieceCID hashes a plaintext chunk with BLAKE2b-256 and returns
// the hex digest.
func ComputePieceCID(chunk []byte) string {
	sum := blake2b.Sum256(chunk)
	return hex.EncodeToString(sum[:])
}

// ComputeMerkleRoot builds a binary Merkle root over ordered piece CIDs.
// Odd-length levels duplicate the last leaf (BitTorrent-style).
func ComputeMerkleRoot(pieceCIDs []string) string {
	if len(pieceCIDs) == 0 {
		return ""
	}
	layer := make([][]byte, len(pieceCIDs))
	for i, c := range pieceCIDs {
		b, err := hex.DecodeString(c)
		if err != nil {
			sum := blake2b.Sum256([]byte(c))
			b = sum[:]
		}
		layer[i] = b
	}
	for len(layer) > 1 {
		if len(layer)%2 == 1 {
			layer = append(layer, layer[len(layer)-1])
		}
		next := make([][]byte, len(layer)/2)
		for i := 0; i < len(next); i++ {
			concat := make([]byte, 0, len(layer[2*i])+len(layer[2*i+1]))
			concat = append(concat, layer[2*i]...)
			concat = append(concat, layer[2*i+1]...)
			sum := blake2b.Sum256(concat)
			next[i] = sum[:]
		}
		layer = next
	}
	return hex.EncodeToString(layer[0])
}

// Split chunks plaintext at the fixed PieceSize and returns the manifest
// + per-piece bytes. Pass zero or negative for maxFileSizeBytes to use
// the default.
//
// For adaptive sizing call SplitAdaptive instead — Split keeps the
// legacy fixed-size behaviour so callers that depend on exact 256 KB
// chunks (e.g., wire-protocol compatibility) are not surprised.
func Split(data []byte, maxFileSizeBytes int64) (*Manifest, [][]byte, error) {
	return splitWith(data, PieceSize, maxFileSizeBytes)
}

// SplitAdaptive picks the piece size from PiecePolicy.PieceSizeFor(len)
// and then runs the same chunking algorithm. Useful for small blobs
// (single chunk) and very large ones (4 MB chunks reduce metadata
// overhead).
func SplitAdaptive(data []byte, policy PiecePolicy, maxFileSizeBytes int64) (*Manifest, [][]byte, error) {
	size := policy.PieceSizeFor(len(data))
	if size <= 0 {
		size = PieceSize
	}
	return splitWith(data, size, maxFileSizeBytes)
}

func splitWith(data []byte, pieceSize int, maxFileSizeBytes int64) (*Manifest, [][]byte, error) {
	if maxFileSizeBytes <= 0 {
		maxFileSizeBytes = DefaultMaxFileSizeBytes
	}
	if int64(len(data)) > maxFileSizeBytes {
		return nil, nil, fmt.Errorf("%w: %d > %d", ErrFileTooLarge, len(data), maxFileSizeBytes)
	}
	if len(data) == 0 {
		return &Manifest{
			RootCID:    "",
			Pieces:     []PieceRef{},
			TotalBytes: 0,
			PieceSize:  pieceSize,
		}, [][]byte{}, nil
	}

	numPieces := (len(data) + pieceSize - 1) / pieceSize
	pieces := make([][]byte, 0, numPieces)
	refs := make([]PieceRef, 0, numPieces)
	cids := make([]string, 0, numPieces)

	for i := 0; i < numPieces; i++ {
		start := i * pieceSize
		end := start + pieceSize
		if end > len(data) {
			end = len(data)
		}
		chunk := make([]byte, end-start)
		copy(chunk, data[start:end])
		cid := ComputePieceCID(chunk)
		pieces = append(pieces, chunk)
		cids = append(cids, cid)
		refs = append(refs, PieceRef{
			Index:     i,
			CID:       cid,
			SizeBytes: len(chunk),
		})
	}

	return &Manifest{
		RootCID:    ComputeMerkleRoot(cids),
		Pieces:     refs,
		TotalBytes: int64(len(data)),
		PieceSize:  pieceSize,
	}, pieces, nil
}

// Reassemble concatenates chunks in order after verifying each chunk's
// CID matches the manifest. Returns an error if any piece fails.
func Reassemble(m *Manifest, pieces [][]byte) ([]byte, error) {
	if m == nil {
		return nil, errors.New("chunker: nil manifest")
	}
	if len(pieces) != len(m.Pieces) {
		return nil, fmt.Errorf("chunker: piece count mismatch: got %d want %d",
			len(pieces), len(m.Pieces))
	}
	out := make([]byte, 0, m.TotalBytes)
	for i, ref := range m.Pieces {
		if len(pieces[i]) != ref.SizeBytes {
			return nil, fmt.Errorf("chunker: piece %d size %d want %d",
				i, len(pieces[i]), ref.SizeBytes)
		}
		if ComputePieceCID(pieces[i]) != ref.CID {
			return nil, fmt.Errorf("chunker: piece %d cid mismatch", i)
		}
		out = append(out, pieces[i]...)
	}
	if int64(len(out)) != m.TotalBytes {
		return nil, fmt.Errorf("chunker: reassembled size %d want %d",
			len(out), m.TotalBytes)
	}
	return out, nil
}
