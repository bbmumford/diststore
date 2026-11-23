/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package storage

import (
	"context"
	"sync"
)

// MemoryBackend is an in-memory Backend, suitable for tests and
// ephemeral nodes.
type MemoryBackend struct {
	mu  sync.RWMutex
	mem map[string][]byte
}

// NewMemoryBackend constructs an empty MemoryBackend.
func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{mem: make(map[string][]byte)}
}

// Put implements Backend.
func (m *MemoryBackend) Put(ctx context.Context, cid string, ct []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cp := make([]byte, len(ct))
	copy(cp, ct)
	m.mu.Lock()
	m.mem[cid] = cp
	m.mu.Unlock()
	return nil
}

// Get implements Backend.
func (m *MemoryBackend) Get(ctx context.Context, cid string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	b, ok := m.mem[cid]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp, nil
}

// Has implements Backend.
func (m *MemoryBackend) Has(ctx context.Context, cid string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	m.mu.RLock()
	_, ok := m.mem[cid]
	m.mu.RUnlock()
	return ok, nil
}

// Delete implements Backend.
func (m *MemoryBackend) Delete(ctx context.Context, cid string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.mem, cid)
	m.mu.Unlock()
	return nil
}

// Iter implements Backend.
func (m *MemoryBackend) Iter(ctx context.Context, fn func(Entry) error) error {
	m.mu.RLock()
	snap := make([]Entry, 0, len(m.mem))
	for cid, b := range m.mem {
		snap = append(snap, Entry{CID: cid, Size: len(b)})
	}
	m.mu.RUnlock()
	for _, e := range snap {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}
