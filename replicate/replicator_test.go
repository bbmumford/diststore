/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package replicate

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/bbmumford/diststore/abuse"
	"github.com/bbmumford/diststore/cache"
	"github.com/bbmumford/diststore/chunker"
	"github.com/bbmumford/diststore/crypto"
	"github.com/bbmumford/diststore/fetch"
	"github.com/bbmumford/diststore/rarest"
	"github.com/bbmumford/diststore/resolve"
	"github.com/bbmumford/diststore/storage"
)

func newTestStore(t *testing.T) *cache.Store {
	t.Helper()
	key := make([]byte, 32)
	s, err := cache.New(cache.Config{
		Backend:    storage.NewMemoryBackend(),
		Cipher:     crypto.Default(),
		StorageKey: key,
	})
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	return s
}

type mapTransport struct {
	mu       sync.Mutex
	payloads map[string][]byte
	errs     map[string]error
}

func newMapTransport() *mapTransport {
	return &mapTransport{payloads: map[string][]byte{}, errs: map[string]error{}}
}

func (m *mapTransport) setPayload(p string, d []byte) { m.mu.Lock(); m.payloads[p] = d; m.mu.Unlock() }
func (m *mapTransport) setErr(p string, e error)      { m.mu.Lock(); m.errs[p] = e; m.mu.Unlock() }

func (m *mapTransport) FetchPiece(ctx context.Context, p string, req fetch.FetchRequest) (fetch.FetchResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.errs[p]; err != nil {
		return fetch.FetchResponse{}, err
	}
	return fetch.FetchResponse{CID: req.CID, Payload: m.payloads[p]}, nil
}

type stubHas struct{}

func (stubHas) Has(_ context.Context, _ string) (bool, error) { return false, nil }

type staticQuery struct{ answers []resolve.PeerAnswer }

func (s staticQuery) Query(_ context.Context, _ string, _ []byte) ([][]byte, error) {
	resp, _ := json.Marshal(&resolve.FindResponse{Answers: s.answers})
	return [][]byte{resp}, nil
}

func newStubResolver(t *testing.T, answers []resolve.PeerAnswer) *resolve.Resolver {
	t.Helper()
	r, err := resolve.New(stubHas{}, staticQuery{answers: answers}, resolve.Config{PeerID: "asker"})
	if err != nil {
		t.Fatalf("resolve.New: %v", err)
	}
	return r
}

func TestNewReplicator_Validation(t *testing.T) {
	if _, err := New(Config{PeerID: "p", RarestFirst: rarest.New(0)}, nil, nil, &fetch.Fetcher{}); err == nil {
		t.Error("expected nil-store error")
	}
	if _, err := New(Config{PeerID: "p", RarestFirst: rarest.New(0)}, newTestStore(t), nil, nil); err == nil {
		t.Error("expected nil-fetcher error")
	}
	if _, err := New(Config{RarestFirst: rarest.New(0)}, newTestStore(t), nil, &fetch.Fetcher{}); err == nil {
		t.Error("expected empty-peerID error")
	}
	if _, err := New(Config{PeerID: "p"}, newTestStore(t), nil, &fetch.Fetcher{}); err == nil {
		t.Error("expected nil-RarestFirst error")
	}
}

func TestTickFetchesMissing(t *testing.T) {
	data := []byte("piece-bytes-for-replicator-test")
	cid := chunker.ComputePieceCID(data)
	tr := newMapTransport()
	tr.setPayload("peer-A", data)
	store := newTestStore(t)
	rf := rarest.New(time.Minute)
	rf.Observe("peer-A", cid, time.Now())
	resolver := newStubResolver(t, []resolve.PeerAnswer{{PeerID: "peer-A"}})
	fetcher, err := fetch.New(resolver, tr, fetch.Config{})
	if err != nil {
		t.Fatalf("fetch.New: %v", err)
	}
	rep, err := New(Config{
		PeerID:          "self",
		FetchInterval:   time.Hour,
		MaxParallelJobs: 2,
		RarestFirst:     rf,
	}, store, nil, fetcher)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rep.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	has, err := store.Has(ctx, cid)
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if !has {
		t.Errorf("expected store to hold cid after tick")
	}
}

func TestAbuseGuardDeniesOpportunisticFetch(t *testing.T) {
	data := []byte("bytes-that-must-not-be-stored")
	cid := chunker.ComputePieceCID(data)
	tr := newMapTransport()
	tr.setPayload("peer-A", data)
	store := newTestStore(t)
	rf := rarest.New(time.Minute)
	rf.Observe("peer-A", cid, time.Now())
	resolver := newStubResolver(t, []resolve.PeerAnswer{{PeerID: "peer-A"}})
	fetcher, _ := fetch.New(resolver, tr, fetch.Config{})
	guard := abuse.New(abuse.Config{
		Limits:                     abuse.DefaultPerTypeLimits(),
		MaxReplicationBytesPerPeer: 4,
		MaxReplicationBytesTotal:   4,
	})
	rep, err := New(Config{
		PeerID:          "self",
		FetchInterval:   time.Hour,
		MaxParallelJobs: 1,
		RarestFirst:     rf,
		AbuseGuard:      guard,
	}, store, nil, fetcher)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rep.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	has, _ := store.Has(ctx, cid)
	if has {
		t.Fatal("store should NOT hold cid after guard denial")
	}
}
