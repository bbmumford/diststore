/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

// Package placement picks which N-of-M peers should hold a given blob.
// The default Hybrid policy composes locality + load + reliability;
// callers can swap in Random / Locality / Load for tests or specialised
// deployments.
package placement

import (
	"errors"
	"sort"
)

// ErrNoEligiblePeers is returned by Policy.Select when no peer matches.
var ErrNoEligiblePeers = errors.New("placement: no eligible peers")

// PeerHint is the per-peer signal the placement policy ranks against.
// Region / AZ / FreeBytes / Reliability are all populated by the
// consumer's heartbeat layer; zero values are treated as "unknown".
type PeerHint struct {
	NodeID      string
	Region      string
	AZ          string
	FreeBytes   int64
	Reliability float64 // 0.0..1.0
	RTTSampleMs int64
	HopCount    int
}

// Policy ranks PeerHints into a sorted-best-first slice and trims to n.
type Policy interface {
	Select(hints []PeerHint, n int) ([]PeerHint, error)
}

// stable trims + returns sorted-by-score slice.
func trim(sorted []PeerHint, n int) ([]PeerHint, error) {
	if n <= 0 || len(sorted) <= n {
		if len(sorted) == 0 {
			return nil, ErrNoEligiblePeers
		}
		return sorted, nil
	}
	return sorted[:n], nil
}

// Hybrid composes locality + load + reliability into a single score.
// Use as the production default: it falls back gracefully when any one
// signal is missing.
type Hybrid struct {
	LocalRegion string
	LocalAZ     string
}

// Select implements Policy.
func (h Hybrid) Select(hints []PeerHint, n int) ([]PeerHint, error) {
	type scored struct {
		h PeerHint
		s float64
	}
	arr := make([]scored, 0, len(hints))
	for _, hint := range hints {
		score := hint.Reliability
		// Locality dominates: a same-region peer beats a remote peer
		// even if the remote one has perfect reliability.
		if h.LocalRegion != "" && hint.Region == h.LocalRegion {
			score += 2.0
		}
		if h.LocalAZ != "" && hint.AZ == h.LocalAZ {
			score += 1.0
		}
		// Free-capacity weight (normalize 0..1 over up to 1 TB).
		if hint.FreeBytes > 0 {
			cap := float64(hint.FreeBytes) / float64(1<<40)
			if cap > 1 {
				cap = 1
			}
			score += cap * 0.25
		}
		// RTT penalty (1ms→tiny, 1s→full point off).
		if hint.RTTSampleMs > 0 {
			rtt := float64(hint.RTTSampleMs) / 1000.0
			score -= rtt
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
