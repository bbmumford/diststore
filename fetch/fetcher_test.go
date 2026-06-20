/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bbmumford/diststore/chunker"
	"github.com/bbmumford/diststore/resolve"
)

type mapTransport struct {
	mu        sync.Mutex
	payloads  map[string][]byte
	errs      map[string]error
	delays    map[string]time.Duration
	reqCount  map[string]int
	cancels   int32
	cancelled chan struct{}
	once      sync.Once
}

func newMapTransport() *mapTransport {
	return &mapTransport{
		payloads:  map[string][]byte{},
		errs:      map[string]error{},
		delays:    map[string]time.Duration{},
		reqCount:  map[string]int{},
		cancelled: make(chan struct{}),
	}
}

func (m *mapTransport) setPayload(p string, d []byte)        { m.mu.Lock(); m.payloads[p] = d; m.mu.Unlock() }
func (m *mapTransport) setErr(p string, e error)             { m.mu.Lock(); m.errs[p] = e; m.mu.Unlock() }
func (m *mapTransport) setDelay(p string, d time.Duration)   { m.mu.Lock(); m.delays[p] = d; m.mu.Unlock() }

func (m *mapTransport) FetchPiece(ctx context.Context, p string, req FetchRequest) (FetchResponse, error) {
	m.mu.Lock()
	m.reqCount[p]++
	err := m.errs[p]
	delay := m.delays[p]
	data := m.payloads[p]
	m.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			atomic.AddInt32(&m.cancels, 1)
			m.once.Do(func() { close(m.cancelled) })
			return FetchResponse{}, ctx.Err()
		}
	}
	if err != nil {
		return FetchResponse{}, err
	}
	return FetchResponse{CID: req.CID, Payload: data}, nil
}

// stubHas resolver-side store.
type stubHas struct{ have map[string]bool }

func (s stubHas) Has(_ context.Context, cid string) (bool, error) { return s.have[cid], nil }

type staticQuery struct{ answers []resolve.PeerAnswer }

func (s staticQuery) Query(_ context.Context, _ string, _ []byte) ([][]byte, error) {
	resp, _ := json.Marshal(&resolve.FindResponse{Answers: s.answers})
	return [][]byte{resp}, nil
}

func newStubResolver(t *testing.T, answers []resolve.PeerAnswer) *resolve.Resolver {
	t.Helper()
	r, err := resolve.New(stubHas{have: map[string]bool{}}, staticQuery{answers: answers}, resolve.Config{PeerID: "asker"})
	if err != nil {
		t.Fatalf("resolve.New: %v", err)
	}
	return r
}

func TestFetcherHappyPath(t *testing.T) {
	data := []byte("hello-distributed-piece")
	cid := chunker.ComputePieceCID(data)
	tr := newMapTransport()
	tr.setPayload("peer-A", data)
	r := newStubResolver(t, []resolve.PeerAnswer{{PeerID: "peer-A"}})
	f, err := New(r, tr, Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := f.Fetch(context.Background(), cid)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("got %q want %q", got, data)
	}
}

func TestFetcherRejectsCIDMismatch(t *testing.T) {
	data := []byte("hello")
	cid := chunker.ComputePieceCID(data)
	tr := newMapTransport()
	tr.setPayload("peer-Evil", []byte("totally-not-the-right-bytes"))
	r := newStubResolver(t, []resolve.PeerAnswer{{PeerID: "peer-Evil"}})
	f, _ := New(r, tr, Config{})
	_, err := f.Fetch(context.Background(), cid)
	if !errors.Is(err, ErrAllFetchesFailed) {
		t.Fatalf("expected ErrAllFetchesFailed, got %v", err)
	}
}

func TestFetcherNoPeers(t *testing.T) {
	tr := newMapTransport()
	r := newStubResolver(t, nil)
	f, _ := New(r, tr, Config{})
	_, err := f.Fetch(context.Background(), "some-cid")
	if !errors.Is(err, ErrNoPeers) {
		t.Fatalf("expected ErrNoPeers, got %v", err)
	}
}

func TestFetcherTimeout(t *testing.T) {
	data := []byte("eventually-arrives")
	cid := chunker.ComputePieceCID(data)
	tr := newMapTransport()
	tr.setPayload("peer-Slow", data)
	tr.setDelay("peer-Slow", 200*time.Millisecond)
	r := newStubResolver(t, []resolve.PeerAnswer{{PeerID: "peer-Slow"}})
	f, _ := New(r, tr, Config{
		PerPeerTimeout: 20 * time.Millisecond,
		OverallTimeout: 40 * time.Millisecond,
	})
	_, err := f.Fetch(context.Background(), cid)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestFetcherFirstValidWinsCancelsOthers(t *testing.T) {
	data := []byte("the-winning-bytes")
	cid := chunker.ComputePieceCID(data)
	tr := newMapTransport()
	tr.setPayload("peer-Fast", data)
	tr.setPayload("peer-Slow", data)
	tr.setDelay("peer-Slow", 500*time.Millisecond)
	tr.setErr("peer-Err", errors.New("offline"))
	r := newStubResolver(t, []resolve.PeerAnswer{
		{PeerID: "peer-Fast"}, {PeerID: "peer-Slow"}, {PeerID: "peer-Err"},
	})
	f, _ := New(r, tr, Config{TopK: 3, PerPeerTimeout: 2 * time.Second, OverallTimeout: 2 * time.Second})
	start := time.Now()
	got, err := f.Fetch(context.Background(), cid)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("payload mismatch")
	}
	if elapsed > 400*time.Millisecond {
		t.Errorf("Fetch took %v, expected <400ms", elapsed)
	}
	select {
	case <-tr.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatalf("slow peer never observed cancellation")
	}
}

type repFunc func(string) float64

func (r repFunc) Score(p string) float64 { return r(p) }

func TestFetcherRanksByLatencyAndReputation(t *testing.T) {
	answers := []resolve.PeerAnswer{
		{PeerID: "fast-lowrep", LatencyHintMs: 10},
		{PeerID: "slow-highrep", LatencyHintMs: 200},
		{PeerID: "med-medrep", LatencyHintMs: 50},
	}
	rep := repFunc(func(p string) float64 {
		switch p {
		case "fast-lowrep":
			return 0.1
		case "slow-highrep":
			return 1.0
		case "med-medrep":
			return 0.5
		}
		return 0
	})
	data := []byte("bytes")
	cid := chunker.ComputePieceCID(data)
	tr := newMapTransport()
	for _, a := range answers {
		tr.setPayload(a.PeerID, data)
	}
	r := newStubResolver(t, answers)
	f, _ := New(r, tr, Config{TopK: 1, Reputation: rep})
	if _, err := f.Fetch(context.Background(), cid); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.reqCount["slow-highrep"] != 1 {
		t.Errorf("expected slow-highrep called once, got %d", tr.reqCount["slow-highrep"])
	}
}

func TestFetcherSkipsZeroReputationPeers(t *testing.T) {
	data := []byte("x")
	cid := chunker.ComputePieceCID(data)
	tr := newMapTransport()
	tr.setPayload("bad", data)
	rep := repFunc(func(p string) float64 {
		if p == "bad" {
			return 0
		}
		return 1
	})
	r := newStubResolver(t, []resolve.PeerAnswer{{PeerID: "bad"}})
	f, _ := New(r, tr, Config{Reputation: rep})
	_, err := f.Fetch(context.Background(), cid)
	if !errors.Is(err, ErrNoPeers) {
		t.Fatalf("expected ErrNoPeers, got %v", err)
	}
}

func TestBandwidthBudgetEnforced(t *testing.T) {
	b := NewBandwidthBudget(2)
	if !b.Acquire("a") {
		t.Fatal("first acquire")
	}
	if !b.Acquire("a") {
		t.Fatal("second acquire")
	}
	if b.Acquire("a") {
		t.Fatal("third acquire should fail")
	}
	b.Release("a")
	if !b.Acquire("a") {
		t.Fatal("acquire after release")
	}
}
