/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

// Package ec implements Reed-Solomon K-of-N erasure coding as a more
// storage-efficient alternative to plain N-replica replication.
//
// A blob is split into K data shards + (N-K) parity shards. Any K
// shards are sufficient to reconstruct the blob, so the cluster can
// tolerate up to N-K shard losses for a storage multiplier of N/K
// instead of N (e.g. K=8, N=12 → 1.5x vs 3x for 3-replica).
package ec

import (
	"errors"
	"fmt"

	"github.com/klauspost/reedsolomon"
)

// Encoder is the EC encode/decode interface. Wraps klauspost's
// reedsolomon library with a friendlier API + parameter validation.
type Encoder interface {
	// K returns the number of data shards.
	K() int
	// N returns the total number of shards (data + parity).
	N() int
	// Encode splits data into K data shards + (N-K) parity shards.
	Encode(data []byte) ([][]byte, error)
	// Reconstruct fills in any nil entries in shards using the
	// available shards. Returns an error if fewer than K shards are
	// available.
	Reconstruct(shards [][]byte) error
	// Join concatenates the first K data shards back into the original
	// payload (truncated to outSize).
	Join(shards [][]byte, outSize int) ([]byte, error)
}

// RSEncoder is a klauspost-backed Reed-Solomon encoder.
type RSEncoder struct {
	k, n int
	rs   reedsolomon.Encoder
}

// NewRSEncoder constructs a K-of-N Reed-Solomon encoder.
func NewRSEncoder(k, n int) (*RSEncoder, error) {
	if k <= 0 || n <= k {
		return nil, fmt.Errorf("ec: invalid K=%d N=%d (need 0<K<N)", k, n)
	}
	rs, err := reedsolomon.New(k, n-k)
	if err != nil {
		return nil, fmt.Errorf("ec: reedsolomon: %w", err)
	}
	return &RSEncoder{k: k, n: n, rs: rs}, nil
}

// K implements Encoder.
func (e *RSEncoder) K() int { return e.k }

// N implements Encoder.
func (e *RSEncoder) N() int { return e.n }

// Encode implements Encoder.
func (e *RSEncoder) Encode(data []byte) ([][]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("ec: cannot encode empty data")
	}
	shards, err := e.rs.Split(data)
	if err != nil {
		return nil, fmt.Errorf("ec: split: %w", err)
	}
	if err := e.rs.Encode(shards); err != nil {
		return nil, fmt.Errorf("ec: encode: %w", err)
	}
	return shards, nil
}

// Reconstruct implements Encoder.
func (e *RSEncoder) Reconstruct(shards [][]byte) error {
	if err := e.rs.Reconstruct(shards); err != nil {
		return fmt.Errorf("ec: reconstruct: %w", err)
	}
	return nil
}

// Join implements Encoder.
func (e *RSEncoder) Join(shards [][]byte, outSize int) ([]byte, error) {
	if outSize <= 0 {
		return nil, errors.New("ec: outSize must be positive")
	}
	// klauspost's Join writes to an io.Writer; small wrapper here.
	buf := newWriterBuf(outSize)
	if err := e.rs.Join(buf, shards, outSize); err != nil {
		return nil, fmt.Errorf("ec: join: %w", err)
	}
	return buf.Bytes(), nil
}

// writerBuf is a minimal in-memory writer used by Join.
type writerBuf struct {
	b []byte
}

func newWriterBuf(cap int) *writerBuf { return &writerBuf{b: make([]byte, 0, cap)} }
func (w *writerBuf) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	return len(p), nil
}
func (w *writerBuf) Bytes() []byte { return w.b }
