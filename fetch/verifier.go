/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package fetch

import (
	"errors"

	"github.com/bbmumford/diststore/chunker"
)

// ErrIntegrityMismatch is returned when a fetched chunk's hash does
// not match the expected CID.
var ErrIntegrityMismatch = errors.New("fetch: integrity mismatch")

// VerifyChunk re-hashes the bytes and returns an error if they do not
// match the expected CID. Used internally by Fetcher.Fetch and
// callable separately by consumers who do their own multi-source
// reconciliation.
func VerifyChunk(expectedCID string, bytes []byte) error {
	if chunker.ComputePieceCID(bytes) != expectedCID {
		return ErrIntegrityMismatch
	}
	return nil
}
