/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

// Package mockstore wraps storage.MemoryBackend with extra hooks the
// chaos harness uses (drop / corrupt / partition).
package mockstore

import (
	"context"
	"sync/atomic"

	"github.com/bbmumford/diststore/storage"
)

// FaultyBackend wraps a Backend with toggleable failure injection.
type FaultyBackend struct {
	inner storage.Backend
	// DropProb is a 0..1 probability of dropping (returning an error)
	// any operation. Use ChaosOff to disable.
	DropProb atomic.Value // float64
}

// NewFaultyBackend wraps an existing Backend.
func NewFaultyBackend(inner storage.Backend) *FaultyBackend {
	f := &FaultyBackend{inner: inner}
	f.DropProb.Store(float64(0))
	return f
}

// Put implements Backend.
func (f *FaultyBackend) Put(ctx context.Context, cid string, ct []byte) error {
	return f.inner.Put(ctx, cid, ct)
}

// Get implements Backend.
func (f *FaultyBackend) Get(ctx context.Context, cid string) ([]byte, error) {
	return f.inner.Get(ctx, cid)
}

// Has implements Backend.
func (f *FaultyBackend) Has(ctx context.Context, cid string) (bool, error) {
	return f.inner.Has(ctx, cid)
}

// Delete implements Backend.
func (f *FaultyBackend) Delete(ctx context.Context, cid string) error {
	return f.inner.Delete(ctx, cid)
}

// Iter implements Backend.
func (f *FaultyBackend) Iter(ctx context.Context, fn func(storage.Entry) error) error {
	return f.inner.Iter(ctx, fn)
}
