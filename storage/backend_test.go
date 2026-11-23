/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package storage

import (
	"context"
	"errors"
	"testing"
)

// runBackendContract exercises the full Backend contract against b, so every implementation is held
// to the same behaviour: content-addressed round trips, miss semantics, caller-copy isolation,
// iteration, idempotent delete, and context cancellation.
func runBackendContract(t *testing.T, b Backend) {
	t.Helper()
	ctx := context.Background()
	const cid = "cafebabedeadbeef"
	data := []byte("encrypted chunk bytes")

	// Absent before Put.
	if ok, err := b.Has(ctx, cid); err != nil || ok {
		t.Fatalf("Has before Put: ok=%v err=%v", ok, err)
	}
	if _, err := b.Get(ctx, cid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on a miss must be ErrNotFound, got %v", err)
	}

	// Put then Get round-trips the exact bytes.
	if err := b.Put(ctx, cid, data); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := b.Get(ctx, cid)
	if err != nil || string(got) != string(data) {
		t.Fatalf("Get after Put: %q err=%v", got, err)
	}

	// The returned slice must not alias stored bytes — mutating it cannot corrupt the store.
	got[0] ^= 0xFF
	if again, _ := b.Get(ctx, cid); string(again) != string(data) {
		t.Fatal("Get must return an independent copy, not aliased storage")
	}

	// Has is true, and Iter reports the entry once with its size.
	if ok, _ := b.Has(ctx, cid); !ok {
		t.Fatal("Has after Put must be true")
	}
	seen := 0
	if err := b.Iter(ctx, func(e Entry) error {
		if e.CID == cid {
			seen++
			if e.Size != len(data) {
				t.Errorf("Iter size = %d, want %d", e.Size, len(data))
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("Iter: %v", err)
	}
	if seen != 1 {
		t.Fatalf("Iter must report the entry exactly once, saw %d", seen)
	}

	// Delete removes it; a second Delete is idempotent (no error on a miss).
	if err := b.Delete(ctx, cid); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ok, _ := b.Has(ctx, cid); ok {
		t.Fatal("Has after Delete must be false")
	}
	if err := b.Delete(ctx, cid); err != nil {
		t.Fatalf("Delete of an absent cid must be idempotent, got %v", err)
	}

	// A cancelled context is refused rather than silently mutating the store.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := b.Put(cancelled, "x", data); err == nil {
		t.Fatal("Put with a cancelled context must fail")
	}
}

func TestMemoryBackend_Contract(t *testing.T) {
	runBackendContract(t, NewMemoryBackend())
}

func TestFilesystemBackend_Contract(t *testing.T) {
	b, err := NewFilesystemBackend(t.TempDir())
	if err != nil {
		t.Fatalf("new fs backend: %v", err)
	}
	runBackendContract(t, b)
}

func TestFilesystemBackend_RequiresRoot(t *testing.T) {
	if _, err := NewFilesystemBackend(""); err == nil {
		t.Fatal("an empty root must be refused")
	}
}

// TestFilesystemBackend_PersistsAcrossReopen confirms blobs written by one backend instance are read
// by another over the same root — the durability the distributed store relies on.
func TestFilesystemBackend_PersistsAcrossReopen(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	b1, _ := NewFilesystemBackend(root)
	if err := b1.Put(ctx, "cid1", []byte("durable")); err != nil {
		t.Fatal(err)
	}
	b2, _ := NewFilesystemBackend(root)
	got, err := b2.Get(ctx, "cid1")
	if err != nil || string(got) != "durable" {
		t.Fatalf("blob must persist across reopen: %q err=%v", got, err)
	}
}
