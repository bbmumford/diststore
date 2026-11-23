/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package cache

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bbmumford/diststore/crypto"
	"github.com/bbmumford/diststore/quota"
	"github.com/bbmumford/diststore/storage"
)

const gb = int64(1024) * 1024 * 1024

type fakeStatter struct{ total, free int64 }

func (f *fakeStatter) DiskStats(string) (int64, int64, error) { return f.total, f.free, nil }

func newTestStore(t *testing.T, inMemory bool) (*Store, string) {
	t.Helper()
	seed := make([]byte, 32)
	rand.Read(seed)
	key, err := crypto.DeriveStorageKey(seed)
	if err != nil {
		t.Fatalf("DeriveStorageKey: %v", err)
	}
	var backend storage.Backend
	var dir string
	if inMemory {
		backend = storage.NewMemoryBackend()
	} else {
		dir = t.TempDir()
		fs, ferr := storage.NewFilesystemBackend(dir)
		if ferr != nil {
			t.Fatalf("NewFilesystemBackend: %v", ferr)
		}
		backend = fs
	}
	s, err := New(Config{
		Backend:    backend,
		Cipher:     crypto.Default(),
		StorageKey: key,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, dir
}

func TestBlobStoreRoundtripInMemory(t *testing.T) {
	s, _ := newTestStore(t, true)
	testBlobStoreConformance(t, s)
}

func TestBlobStoreRoundtripOnDisk(t *testing.T) {
	s, _ := newTestStore(t, false)
	testBlobStoreConformance(t, s)
}

func testBlobStoreConformance(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	data := []byte("hello distributed world")
	cid, err := s.Put(ctx, data)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if cid == "" {
		t.Fatal("empty cid")
	}
	ok, err := s.Has(ctx, cid)
	if err != nil || !ok {
		t.Fatalf("Has: ok=%v err=%v", ok, err)
	}
	got, err := s.Get(ctx, cid)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("get mismatch")
	}
	count, bytesTotal, err := s.Size(ctx)
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if count != 1 || bytesTotal != int64(len(data)) {
		t.Errorf("Size: got count=%d bytes=%d", count, bytesTotal)
	}
	if err := s.Delete(ctx, cid); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, cid); !errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("Get after delete: got %v want ErrBlobNotFound", err)
	}
	if err := s.Delete(ctx, cid); err != nil {
		t.Errorf("Delete missing: %v", err)
	}
	// Idempotent put.
	cid2, _ := s.Put(ctx, data)
	cid3, _ := s.Put(ctx, data)
	if cid2 != cid || cid3 != cid {
		t.Errorf("cid drift")
	}
	count, _, _ = s.Size(ctx)
	if count != 1 {
		t.Errorf("idempotent put: got count=%d want 1", count)
	}
}

func TestStoreEncryptsOnDisk(t *testing.T) {
	s, dir := newTestStore(t, false)
	ctx := context.Background()
	plaintext := []byte("super-secret-bytes-that-should-not-appear-on-disk")
	cid, err := s.Put(ctx, plaintext)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, cid+".blob"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if bytes.Contains(raw, plaintext) {
		t.Fatal("plaintext appeared on disk")
	}
}

func TestStoreQuotaRejection(t *testing.T) {
	seed := make([]byte, 32)
	rand.Read(seed)
	key, _ := crypto.DeriveStorageKey(seed)
	fs := &fakeStatter{total: 100 * gb, free: 30 * gb}
	qm := quota.NewManager(quota.DefaultManagerConfig(), "", fs)
	s, err := New(Config{
		Backend:    storage.NewMemoryBackend(),
		Cipher:     crypto.Default(),
		StorageKey: key,
		Quota:      qm,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = s.Put(context.Background(), []byte("hello"))
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded, got %v", err)
	}
}

func TestStoreRehydrate(t *testing.T) {
	seed := make([]byte, 32)
	rand.Read(seed)
	key, _ := crypto.DeriveStorageKey(seed)
	dir := t.TempDir()
	fs1, _ := storage.NewFilesystemBackend(dir)
	s1, err := New(Config{Backend: fs1, StorageKey: key})
	if err != nil {
		t.Fatalf("New 1: %v", err)
	}
	cid, err := s1.Put(context.Background(), []byte("persistent bytes"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	fs2, _ := storage.NewFilesystemBackend(dir)
	s2, err := New(Config{Backend: fs2, StorageKey: key})
	if err != nil {
		t.Fatalf("New 2: %v", err)
	}
	ok, err := s2.Has(context.Background(), cid)
	if err != nil || !ok {
		t.Fatalf("rehydrated Has: ok=%v err=%v", ok, err)
	}
	got, err := s2.Get(context.Background(), cid)
	if err != nil {
		t.Fatalf("rehydrated Get: %v", err)
	}
	if !bytes.Equal(got, []byte("persistent bytes")) {
		t.Fatalf("rehydrated plaintext mismatch: %q", got)
	}
}
