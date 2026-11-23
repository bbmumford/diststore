/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

// Package cache is the encrypted, quota-aware blob cache.
//
// Store composes:
//   - a storage.Backend (filesystem default; memory for tests),
//   - a crypto.Cipher (XChaCha20-Poly1305 default; AES-GCM available),
//   - an optional quota.Manager (gate writes),
//   - an optional Fetcher fallback (mesh-side miss).
package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/bbmumford/diststore/chunker"
	"github.com/bbmumford/diststore/crypto"
	"github.com/bbmumford/diststore/quota"
	"github.com/bbmumford/diststore/storage"
)

// ErrBlobNotFound is returned when a CID is not present in the store.
var ErrBlobNotFound = errors.New("cache: blob not found")

// ErrQuotaExceeded is returned when a Put would exceed the current budget.
var ErrQuotaExceeded = errors.New("cache: quota exceeded")

// BlobStore is the context-aware blob interface served by the cache.
type BlobStore interface {
	Put(ctx context.Context, data []byte) (cid string, err error)
	Get(ctx context.Context, cid string) ([]byte, error)
	Has(ctx context.Context, cid string) (bool, error)
	Delete(ctx context.Context, cid string) error
	Size(ctx context.Context) (count int, bytes int64, err error)
}

// Fetcher is the optional mesh-side fallback consulted on Get miss.
// It is declared here (rather than in the fetch package) to break a
// circular dependency — fetch depends on cache.Store for the
// caching-back integration.
type Fetcher interface {
	Fetch(ctx context.Context, cid string) ([]byte, error)
}

// Config wires the Store's collaborators together.
type Config struct {
	// Backend is the on-disk / in-memory backend. Required.
	Backend storage.Backend
	// Cipher is the AEAD primitive used to encrypt chunks. nil = use
	// crypto.Default() (XChaCha20-Poly1305).
	Cipher crypto.Cipher
	// StorageKey is the 32-byte key used by Cipher. Required.
	StorageKey []byte
	// Quota, if non-nil, gates writes. Pass nil to disable enforcement.
	Quota *quota.Manager
	// Fetcher, if non-nil, is consulted by Get on a local miss.
	Fetcher Fetcher
}

// Store is the encrypted-at-rest, quota-aware blob cache.
type Store struct {
	cfg Config

	mu      sync.RWMutex
	sizes   map[string]int // cid -> plaintext size
	byteSum int64
}

// New constructs a Store from cfg.
func New(cfg Config) (*Store, error) {
	if cfg.Backend == nil {
		return nil, errors.New("cache: Config.Backend required")
	}
	if cfg.Cipher == nil {
		cfg.Cipher = crypto.Default()
	}
	if len(cfg.StorageKey) != cfg.Cipher.KeyLen() {
		return nil, fmt.Errorf("cache: StorageKey must be %d bytes, got %d",
			cfg.Cipher.KeyLen(), len(cfg.StorageKey))
	}
	s := &Store{
		cfg:   cfg,
		sizes: make(map[string]int),
	}
	if err := s.rehydrate(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

// SetFetcher attaches (or replaces) the mesh-side fallback. Safe to
// call concurrently with Get.
func (s *Store) SetFetcher(f Fetcher) {
	s.mu.Lock()
	s.cfg.Fetcher = f
	s.mu.Unlock()
}

// rehydrate walks the backend and rebuilds the in-memory index.
func (s *Store) rehydrate(ctx context.Context) error {
	return s.cfg.Backend.Iter(ctx, func(e storage.Entry) error {
		// Approximate plaintext as ciphertext-size minus AEAD overhead.
		plain := e.Size - 16
		if plain < 0 {
			plain = 0
		}
		s.sizes[e.CID] = plain
		s.byteSum += int64(plain)
		return nil
	})
}

// Put stores a single chunk. The returned CID is BLAKE2b-256 of the
// plaintext.
func (s *Store) Put(ctx context.Context, data []byte) (string, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return "", err
		}
	}
	cid := chunker.ComputePieceCID(data)

	s.mu.RLock()
	_, exists := s.sizes[cid]
	s.mu.RUnlock()
	if !exists && s.cfg.Quota != nil {
		if !s.cfg.Quota.HasRoomFor(int64(len(data))) {
			return "", fmt.Errorf("%w: budget=%d need=%d",
				ErrQuotaExceeded, s.cfg.Quota.CurrentBudget(), len(data))
		}
	}

	ct, err := s.cfg.Cipher.EncryptForStorage(s.cfg.StorageKey, cid, data)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, already := s.sizes[cid]; already {
		return cid, nil
	}
	if err := s.cfg.Backend.Put(ctx, cid, ct); err != nil {
		return "", err
	}
	s.sizes[cid] = len(data)
	s.byteSum += int64(len(data))
	if s.cfg.Quota != nil {
		s.cfg.Quota.SetOurStorage(s.byteSum)
	}
	return cid, nil
}

// Get returns the plaintext chunk identified by cid. Falls back to the
// Fetcher (if wired) on local miss.
func (s *Store) Get(ctx context.Context, cid string) ([]byte, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	pt, err := s.getLocal(ctx, cid)
	if err == nil {
		return pt, nil
	}
	if !errors.Is(err, ErrBlobNotFound) {
		return nil, err
	}
	s.mu.RLock()
	f := s.cfg.Fetcher
	s.mu.RUnlock()
	if f == nil {
		return nil, ErrBlobNotFound
	}
	data, ferr := f.Fetch(ctx, cid)
	if ferr != nil {
		return nil, ferr
	}
	// Cache back; ignore quota errors so caller still gets the bytes.
	if _, perr := s.Put(ctx, data); perr != nil && !errors.Is(perr, ErrQuotaExceeded) {
		_ = perr
	}
	return data, nil
}

func (s *Store) getLocal(ctx context.Context, cid string) ([]byte, error) {
	ct, err := s.cfg.Backend.Get(ctx, cid)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, ErrBlobNotFound
		}
		return nil, fmt.Errorf("cache: read %q: %w", cid, err)
	}
	if len(ct) == 0 {
		return nil, ErrBlobNotFound
	}
	pt, err := s.cfg.Cipher.DecryptFromStorage(s.cfg.StorageKey, cid, ct)
	if err != nil {
		return nil, err
	}
	if chunker.ComputePieceCID(pt) != cid {
		return nil, fmt.Errorf("cache: cid mismatch on read")
	}
	return pt, nil
}

// Has reports whether cid is in the store.
func (s *Store) Has(ctx context.Context, cid string) (bool, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return false, err
		}
	}
	s.mu.RLock()
	_, ok := s.sizes[cid]
	s.mu.RUnlock()
	return ok, nil
}

// Delete removes cid. Missing cid is a no-op.
func (s *Store) Delete(ctx context.Context, cid string) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	size, ok := s.sizes[cid]
	if !ok {
		return nil
	}
	delete(s.sizes, cid)
	s.byteSum -= int64(size)
	if s.byteSum < 0 {
		s.byteSum = 0
	}
	if err := s.cfg.Backend.Delete(ctx, cid); err != nil {
		return fmt.Errorf("cache: backend delete %q: %w", cid, err)
	}
	if s.cfg.Quota != nil {
		s.cfg.Quota.SetOurStorage(s.byteSum)
	}
	return nil
}

// Size reports the current count + byte totals.
func (s *Store) Size(ctx context.Context) (int, int64, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sizes), s.byteSum, nil
}

// List enumerates the root CIDs currently stored.
func (s *Store) List(ctx context.Context) ([]string, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.sizes))
	for cid := range s.sizes {
		out = append(out, cid)
	}
	return out, nil
}

// Bytes returns the total plaintext bytes currently cached.
func (s *Store) Bytes() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byteSum
}
