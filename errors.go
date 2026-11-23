/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package diststore

import "errors"

// Sentinel errors used across sub-packages.
//
// Sub-packages re-export the errors they're responsible for raising so
// callers can errors.Is against either the root or sub-package symbol.
var (
	// ErrBlobNotFound is returned when a CID is not present in the store.
	ErrBlobNotFound = errors.New("diststore: blob not found")
	// ErrChunkNotFound is returned when a specific chunk (not a whole
	// manifest) is missing.
	ErrChunkNotFound = errors.New("diststore: chunk not found")
	// ErrQuotaExceeded is returned when a Put would exceed the current
	// disk budget (quota.Manager).
	ErrQuotaExceeded = errors.New("diststore: quota exceeded")
	// ErrIntegrityMismatch is returned when a stored or fetched chunk's
	// CID does not match the expected content hash.
	ErrIntegrityMismatch = errors.New("diststore: integrity mismatch")
	// ErrInsufficientReplicas is returned when a placement / replication
	// loop cannot meet the requested replication factor.
	ErrInsufficientReplicas = errors.New("diststore: insufficient replicas")
	// ErrNoEligiblePeers is returned by the placement policy when no peer
	// matches the requested constraints.
	ErrNoEligiblePeers = errors.New("diststore: no eligible peers")
	// ErrAdvertisementSignatureInvalid is returned when a signed
	// advertisement envelope fails Ed25519 verification.
	ErrAdvertisementSignatureInvalid = errors.New("diststore: advertisement signature invalid")
	// ErrFileTooLarge is returned when Split receives a file above the
	// per-call cap.
	ErrFileTooLarge = errors.New("diststore: file exceeds max size")
	// ErrAllFetchesFailed is returned when every candidate peer failed to
	// deliver a valid piece.
	ErrAllFetchesFailed = errors.New("diststore: all peer fetches failed")
	// ErrNoPeers is returned when resolver.Find returns no candidates.
	ErrNoPeers = errors.New("diststore: no peers know this cid")
)
