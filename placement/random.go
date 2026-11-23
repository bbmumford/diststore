/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package placement

import (
	"math/rand"
)

// Random picks n peers uniformly at random. Baseline.
type Random struct{}

// Select implements Policy.
func (Random) Select(hints []PeerHint, n int) ([]PeerHint, error) {
	if len(hints) == 0 {
		return nil, ErrNoEligiblePeers
	}
	cp := make([]PeerHint, len(hints))
	copy(cp, hints)
	rand.Shuffle(len(cp), func(i, j int) { cp[i], cp[j] = cp[j], cp[i] })
	return trim(cp, n)
}
