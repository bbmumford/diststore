/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

// Package health publishes per-blob health snapshots derived from
// replica counts + last-verified timestamps. Consumers render this for
// operators or feed it back into the repair queue.
package health

import "time"

// State buckets a blob's current health.
type State string

const (
	StateHealthy  State = "healthy"
	StateDegraded State = "degraded"
	StateAtRisk   State = "at_risk"
	StateLost     State = "lost"
)

// Report is the per-blob health snapshot.
type Report struct {
	RootCID        string
	ReplicaCount   int
	Age            time.Duration
	LastVerifiedAt time.Time
	State          State
}

// ClassifyReplicas returns the bucket for a given replica count vs the
// target. Below half-target → at-risk; below target → degraded; equal
// or above → healthy. Zero replicas → lost.
func ClassifyReplicas(have, target int) State {
	if have <= 0 {
		return StateLost
	}
	if target <= 0 {
		return StateHealthy
	}
	if have*2 < target {
		return StateAtRisk
	}
	if have < target {
		return StateDegraded
	}
	return StateHealthy
}
