/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

// Package metrics holds small, lock-free numeric helpers for mesh hot paths.
package metrics

import (
	"math"
	"sync/atomic"
)

// EWMA is a lock-free exponentially-weighted moving average. The current value
// is held as the IEEE-754 bits of a float64 inside an atomic.Uint64, and Update
// uses a compare-and-swap retry loop, so concurrent updaters never block and
// never corrupt the value — a drop-in replacement for an `α*x + (1-α)*old`
// field guarded by a mutex on a per-sample hot path.
//
// Under contention the CAS retries against the latest value: the losing
// goroutine recomputes its blend against the winner's value rather than
// overwriting it, so no sample's contribution is silently lost. This matches
// the accepted semantics of the mesh EMA fields it replaces; do not use EWMA
// where exact additive accumulation is required.
type EWMA struct {
	bits  atomic.Uint64
	alpha float64
}

// NewEWMA returns an EWMA seeded with initial. alpha is the weight applied to
// each new sample and is clamped to [0, 1]. alpha is fixed for the lifetime of
// the EWMA (set once here, read without synchronisation in Update).
func NewEWMA(alpha, initial float64) *EWMA {
	if alpha < 0 {
		alpha = 0
	}
	if alpha > 1 {
		alpha = 1
	}
	e := &EWMA{alpha: alpha}
	e.bits.Store(math.Float64bits(initial))
	return e
}

// Update folds sample into the average: new = alpha*sample + (1-alpha)*old.
func (e *EWMA) Update(sample float64) {
	for {
		oldBits := e.bits.Load()
		next := e.alpha*sample + (1-e.alpha)*math.Float64frombits(oldBits)
		if e.bits.CompareAndSwap(oldBits, math.Float64bits(next)) {
			return
		}
	}
}

// Reset hard-sets the average to value, discarding history.
func (e *EWMA) Reset(value float64) {
	e.bits.Store(math.Float64bits(value))
}

// Value returns the current smoothed average.
func (e *EWMA) Value() float64 {
	return math.Float64frombits(e.bits.Load())
}
