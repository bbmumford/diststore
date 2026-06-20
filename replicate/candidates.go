/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package replicate

import "context"

// CandidateSource supplies the set of CIDs the replicator should
// consider fetching on each tick.
type CandidateSource interface {
	Candidates(ctx context.Context) ([]string, error)
}

// CandidateSourceFunc adapts a plain function to CandidateSource.
type CandidateSourceFunc func(ctx context.Context) ([]string, error)

// Candidates implements CandidateSource.
func (f CandidateSourceFunc) Candidates(ctx context.Context) ([]string, error) { return f(ctx) }
