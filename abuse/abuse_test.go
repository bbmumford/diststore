/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package abuse

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bbmumford/diststore/chunker"
	"github.com/bbmumford/diststore/index"
)

type fakeRefs struct {
	mu    sync.Mutex
	alive map[string]bool
}

func (f *fakeRefs) IsReferenced(_ context.Context, rootCID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alive[rootCID], nil
}

func (f *fakeRefs) set(cid string, alive bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.alive == nil {
		f.alive = map[string]bool{}
	}
	f.alive[cid] = alive
}

func makeSignedManifest(t *testing.T, size int) (*chunker.Manifest, []byte, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	data := make([]byte, size)
	rand.Read(data)
	m, _, err := chunker.Split(data, 0)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	sig, err := index.SignManifest(m, priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return m, sig, pub, priv
}

func TestValidateStorablePerTypeLimits(t *testing.T) {
	m, sig, pub, _ := makeSignedManifest(t, 3*1024*1024)
	refs := &fakeRefs{}
	refs.set(m.RootCID, true)
	ag := New(Config{References: refs, Limits: DefaultPerTypeLimits()})
	err := ag.ValidateStorable(context.Background(), m, sig, pub, RecordTypeAvatar)
	if !errors.Is(err, ErrPerTypeExceeded) {
		t.Fatalf("expected ErrPerTypeExceeded, got %v", err)
	}
	m2, sig2, pub2, _ := makeSignedManifest(t, 1*1024*1024)
	refs.set(m2.RootCID, true)
	if err := ag.ValidateStorable(context.Background(), m2, sig2, pub2, RecordTypeAvatar); err != nil {
		t.Fatalf("unexpected reject: %v", err)
	}
}

func TestValidateStorableReferenceRequired(t *testing.T) {
	m, sig, pub, _ := makeSignedManifest(t, 200*1024)
	refs := &fakeRefs{}
	ag := New(Config{References: refs, Limits: DefaultPerTypeLimits()})
	err := ag.ValidateStorable(context.Background(), m, sig, pub, RecordTypeListing)
	if !errors.Is(err, ErrNoReference) {
		t.Fatalf("expected ErrNoReference, got %v", err)
	}
}

func TestValidateStorableBadSignature(t *testing.T) {
	m, sig, pub, _ := makeSignedManifest(t, 128)
	sig[0] ^= 0xFF
	refs := &fakeRefs{}
	refs.set(m.RootCID, true)
	ag := New(Config{References: refs, Limits: DefaultPerTypeLimits()})
	err := ag.ValidateStorable(context.Background(), m, sig, pub, RecordTypeListing)
	if !errors.Is(err, ErrBadUploaderSig) {
		t.Fatalf("expected ErrBadUploaderSig, got %v", err)
	}
}

func TestGarbageCollectOrphanAfterGrace(t *testing.T) {
	refs := &fakeRefs{}
	ag := New(Config{
		References:  refs,
		Limits:      DefaultPerTypeLimits(),
		OrphanGrace: 50 * time.Millisecond,
	})
	cid := "abc123"
	refs.set(cid, false)

	got, err := ag.GarbageCollect(context.Background(), []string{cid})
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("first sweep: got %v, want empty", got)
	}
	time.Sleep(60 * time.Millisecond)
	got, _ = ag.GarbageCollect(context.Background(), []string{cid})
	if len(got) != 1 || got[0] != cid {
		t.Fatalf("post-grace: got %v, want [%s]", got, cid)
	}
}

func TestAbuseCheck_FirstFetchAllowed(t *testing.T) {
	ag := New(Config{
		Limits:                     DefaultPerTypeLimits(),
		MaxReplicationBytesPerPeer: 10 * 1024 * 1024,
		MaxReplicationBytesTotal:   100 * 1024 * 1024,
	})
	if err := ag.Check(context.Background(), "cid-1", "peer-A", 4096, RecordTypeReplication); err != nil {
		t.Fatalf("first fetch unexpectedly rejected: %v", err)
	}
	if got := ag.ReplicationUsage("peer-A"); got != 4096 {
		t.Errorf("peer-A usage: got %d want 4096", got)
	}
}

func TestAbuseCheck_PerPeerQuotaEnforced(t *testing.T) {
	ag := New(Config{
		Limits:                     DefaultPerTypeLimits(),
		MaxReplicationBytesPerPeer: 10 * 1024,
		MaxReplicationBytesTotal:   1024 * 1024,
	})
	if err := ag.Check(context.Background(), "cid-A1", "peer-A", 8*1024, RecordTypeReplication); err != nil {
		t.Fatalf("peer-A first: %v", err)
	}
	err := ag.Check(context.Background(), "cid-A2", "peer-A", 4*1024, RecordTypeReplication)
	if !errors.Is(err, ErrPeerQuotaExceeded) {
		t.Fatalf("expected ErrPeerQuotaExceeded, got %v", err)
	}
}

func TestAbuseCheck_AlreadyHeldReturnsSentinel(t *testing.T) {
	ag := New(Config{
		Limits:                     DefaultPerTypeLimits(),
		MaxReplicationBytesPerPeer: 10 * 1024 * 1024,
		MaxReplicationBytesTotal:   100 * 1024 * 1024,
	})
	if err := ag.Check(context.Background(), "cid-dup", "peer-A", 2048, RecordTypeReplication); err != nil {
		t.Fatalf("first: %v", err)
	}
	err := ag.Check(context.Background(), "cid-dup", "peer-B", 2048, RecordTypeReplication)
	if !errors.Is(err, ErrAlreadyHeld) {
		t.Fatalf("expected ErrAlreadyHeld, got %v", err)
	}
}

func TestAbuseCheck_ReleaseRefundsPeer(t *testing.T) {
	ag := New(Config{
		Limits:                     DefaultPerTypeLimits(),
		MaxReplicationBytesPerPeer: 8 * 1024,
		MaxReplicationBytesTotal:   1024 * 1024,
	})
	if err := ag.Check(context.Background(), "cid-1", "peer-A", 6*1024, RecordTypeReplication); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := ag.Check(context.Background(), "cid-2", "peer-A", 4*1024, RecordTypeReplication); !errors.Is(err, ErrPeerQuotaExceeded) {
		t.Fatalf("expected ErrPeerQuotaExceeded pre-release, got %v", err)
	}
	ag.ReleaseReplication("cid-1", "peer-A")
	if got := ag.ReplicationUsage("peer-A"); got != 0 {
		t.Errorf("after release: got %d want 0", got)
	}
	if err := ag.Check(context.Background(), "cid-2", "peer-A", 4*1024, RecordTypeReplication); err != nil {
		t.Fatalf("post-release: %v", err)
	}
}

func TestChargeAndRefundUploader(t *testing.T) {
	ag := New(Config{Limits: DefaultPerTypeLimits()})
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	ag.ChargeUploader(pub, 1024)
	ag.ChargeUploader(pub, 512)
	if got := ag.UploaderUsage(pub); got != 1536 {
		t.Errorf("usage: got %d want 1536", got)
	}
	ag.RefundUploader(pub, 1024)
	if got := ag.UploaderUsage(pub); got != 512 {
		t.Errorf("after refund: got %d want 512", got)
	}
	ag.RefundUploader(pub, 10_000)
	if got := ag.UploaderUsage(pub); got != 0 {
		t.Errorf("clamp: got %d want 0", got)
	}
}
