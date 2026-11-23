/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package chunker

// PiecePolicy returns the chunk size to use for a blob of n bytes.
//
// The default DefaultPiecePolicy implements the three-band heuristic:
//   - n < 1 MB         → single chunk (n bytes)
//   - 1 MB ≤ n ≤ 10 MB → 256 KB chunks (legacy default)
//   - n > 10 MB        → 4 MB chunks (cuts metadata for large files)
//
// Callers that want a fixed size pass FixedPiecePolicy{Size: N}.
type PiecePolicy interface {
	PieceSizeFor(totalBytes int) int
}

const (
	// SmallBlobUpperBound is the size below which adaptive sizing
	// collapses to a single chunk.
	SmallBlobUpperBound = 1 << 20 // 1 MB
	// MediumBlobUpperBound is the size at-or-below-which adaptive sizing
	// keeps the legacy 256 KB chunks.
	MediumBlobUpperBound = 10 << 20 // 10 MB
	// LargePieceSize is the chunk size used above MediumBlobUpperBound.
	LargePieceSize = 4 << 20 // 4 MB
)

// DefaultPiecePolicy is the recommended adaptive policy.
type DefaultPiecePolicy struct{}

// PieceSizeFor implements PiecePolicy.
func (DefaultPiecePolicy) PieceSizeFor(totalBytes int) int {
	switch {
	case totalBytes <= 0:
		return PieceSize
	case totalBytes < SmallBlobUpperBound:
		// Single chunk — avoid manifest+merkle overhead.
		return totalBytes
	case totalBytes <= MediumBlobUpperBound:
		return PieceSize
	default:
		return LargePieceSize
	}
}

// FixedPiecePolicy always returns the same size. Useful for tests and
// for callers that want to lock to a specific chunk size.
type FixedPiecePolicy struct{ Size int }

// PieceSizeFor implements PiecePolicy.
func (f FixedPiecePolicy) PieceSizeFor(totalBytes int) int {
	if f.Size <= 0 {
		return PieceSize
	}
	return f.Size
}
