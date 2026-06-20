/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package placement

import "sort"

// Locality prefers peers that match LocalRegion + LocalAZ first
// (region match scores 2, AZ match scores 1, ties stable-sorted).
//
// Caveat: LocalRegion / LocalAZ are caller-supplied snapshots. The
// policy does not subscribe to topology changes — rebind by
// constructing a fresh Locality (or mutating the fields under the
// caller's own lock) whenever the consumer's heartbeat layer
// observes a region/AZ move.
type Locality struct {
	LocalRegion string
	LocalAZ     string
}

// Select implements Policy.
func (l Locality) Select(hints []PeerHint, n int) ([]PeerHint, error) {
	type scored struct {
		h PeerHint
		s int
	}
	arr := make([]scored, 0, len(hints))
	for _, hint := range hints {
		score := 0
		if l.LocalRegion != "" && hint.Region == l.LocalRegion {
			score += 2
		}
		if l.LocalAZ != "" && hint.AZ == l.LocalAZ {
			score += 1
		}
		arr = append(arr, scored{h: hint, s: score})
	}
	sort.SliceStable(arr, func(i, j int) bool { return arr[i].s > arr[j].s })
	out := make([]PeerHint, len(arr))
	for i, s := range arr {
		out[i] = s.h
	}
	return trim(out, n)
}
