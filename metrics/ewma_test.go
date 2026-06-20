/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package metrics

import (
	"math"
	"sync"
	"testing"
)

func TestEWMA_Converges(t *testing.T) {
	e := NewEWMA(0.3, 0)
	for i := 0; i < 200; i++ {
		e.Update(100)
	}
	if got := e.Value(); math.Abs(got-100) > 0.5 {
		t.Fatalf("EWMA should converge to 100, got %v", got)
	}
}

func TestEWMA_SeedAndReset(t *testing.T) {
	e := NewEWMA(0.5, 42)
	if e.Value() != 42 {
		t.Fatalf("seed = %v, want 42", e.Value())
	}
	e.Reset(7)
	if e.Value() != 7 {
		t.Fatalf("after reset = %v, want 7", e.Value())
	}
}

func TestEWMA_AlphaClamped(t *testing.T) {
	// alpha>1 clamps to 1 → Update tracks the latest sample exactly.
	e := NewEWMA(5, 0)
	e.Update(13)
	if e.Value() != 13 {
		t.Fatalf("alpha clamped-to-1 should track sample, got %v", e.Value())
	}
	// alpha<0 clamps to 0 → Update never moves off the seed.
	z := NewEWMA(-1, 9)
	z.Update(1000)
	if z.Value() != 9 {
		t.Fatalf("alpha clamped-to-0 should stay at seed, got %v", z.Value())
	}
}

// Concurrent Update from many goroutines must never panic or corrupt the value
// (the CAS invariant). Run under -race. The value stays bounded within the
// sample range regardless of interleaving.
func TestEWMA_ConcurrentUpdate(t *testing.T) {
	e := NewEWMA(0.2, 50)
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				e.Update(50)
			}
		}()
	}
	wg.Wait()
	if got := e.Value(); got < 0 || got > 100 || math.IsNaN(got) {
		t.Fatalf("value out of range after concurrent updates: %v", got)
	}
}
