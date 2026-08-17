/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

// Package storage hosts the pluggable Backend interface that decouples
// the cache.Store from the on-disk / in-memory layout of individual
// chunk files.
//
// The default Filesystem backend writes one .blob file per chunk under
// the configured Root. The Memory backend keeps the same map in process
// memory — useful for tests + ephemeral nodes.
package storage

import (
	"context"
	"errors"
)

// ErrNotFound is returned by Get when the cid is absent. Delete treats an absent cid as a no-op and
// does not return it.
var ErrNotFound = errors.New("storage: not found")

// Entry is a single (cid, size) record returned by Iter.
type Entry struct {
	CID  string
	Size int
}

// Backend abstracts the on-disk / in-memory layout of chunk storage.
// All methods are safe for concurrent use unless documented otherwise.
type Backend interface {
	// Put writes ciphertext under cid. Overwrite is allowed for content-
	// addressed stores (the bytes-for-CID mapping is stable).
	Put(ctx context.Context, cid string, ciphertext []byte) error
	// Get reads ciphertext by cid. Returns ErrNotFound on miss.
	Get(ctx context.Context, cid string) ([]byte, error)
	// Has reports whether cid is present without reading the bytes.
	Has(ctx context.Context, cid string) (bool, error)
	// Delete removes cid. Missing cid is a no-op (no error).
	Delete(ctx context.Context, cid string) error
	// Iter enumerates all (cid, ciphertext-size) entries. Order is
	// backend-defined.
	Iter(ctx context.Context, fn func(Entry) error) error
}
