/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package fetch

import "sync"

// BandwidthBudget is a per-peer in-flight cap: each peer may only have
// MaxConcurrent active fetches at a time. The implementation is a
// counter per peerID guarded by a mutex.
//
// Caveat: in-flight counting treats all fetches as equal weight, so a
// single slow huge fetch holds a slot for as long as it runs and can
// crowd out smaller fetches from the same peer. Consumers that need
// byte-level fairness should wrap Acquire / Release with a token
// bucket keyed on response size.
type BandwidthBudget struct {
	maxConcurrent int
	mu            sync.Mutex
	active        map[string]int
}

// NewBandwidthBudget caps each peer to maxConcurrent in-flight fetches.
// 0 = effectively unlimited.
func NewBandwidthBudget(maxConcurrent int) *BandwidthBudget {
	return &BandwidthBudget{
		maxConcurrent: maxConcurrent,
		active:        make(map[string]int),
	}
}

// Acquire reserves a slot for peerID. Returns false when the per-peer
// cap is full.
func (b *BandwidthBudget) Acquire(peerID string) bool {
	if b.maxConcurrent <= 0 {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.active[peerID] >= b.maxConcurrent {
		return false
	}
	b.active[peerID]++
	return true
}

// Release frees a slot for peerID. Safe to call after Acquire returns
// true even if the caller's fetch errored.
func (b *BandwidthBudget) Release(peerID string) {
	if b.maxConcurrent <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.active[peerID] > 0 {
		b.active[peerID]--
		if b.active[peerID] == 0 {
			delete(b.active, peerID)
		}
	}
}

// Active returns the current in-flight count for peerID.
func (b *BandwidthBudget) Active(peerID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.active[peerID]
}
