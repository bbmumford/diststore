/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package ec

// Decision describes whether a particular blob should use erasure
// coding or plain replication.
type Decision struct {
	UseEC bool
	K     int
	N     int
}

// PolicyFunc decides per-blob whether to use EC. Default: plain
// replication for blobs ≤ 1 MB; 8-of-12 EC for larger ones (1.5x vs
// 3x for plain N=3, tolerates 4 shard losses).
type PolicyFunc func(blobBytes int64) Decision

// DefaultPolicy returns the recommended threshold policy.
func DefaultPolicy() PolicyFunc {
	return func(blobBytes int64) Decision {
		if blobBytes <= 1<<20 {
			return Decision{UseEC: false}
		}
		return Decision{UseEC: true, K: 8, N: 12}
	}
}
