/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package placement

import "sort"

// Load picks the least-loaded (highest FreeBytes) peers first.
type Load struct{}

// Select implements Policy.
func (Load) Select(hints []PeerHint, n int) ([]PeerHint, error) {
	cp := make([]PeerHint, len(hints))
	copy(cp, hints)
	sort.SliceStable(cp, func(i, j int) bool { return cp[i].FreeBytes > cp[j].FreeBytes })
	return trim(cp, n)
}
