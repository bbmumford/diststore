/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

// Package rarest tracks per-CID replication counts so the fetcher /
// replicator can prefer rarer pieces (BitTorrent-style rarest-first).
package rarest

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/bbmumford/diststore/advertise"
)

// DefaultTTL is the window an observed (peer, cid) pair stays fresh.
const DefaultTTL = 1 * time.Hour

type entry struct {
	peers map[string]time.Time
}

// State tracks per-CID replication counts derived from inbound
// HaveAdvertisement broadcasts.
//
// The zero value is NOT valid; construct via New. Safe for concurrent
// use.
type State struct {
	mu      sync.Mutex
	entries map[string]*entry
	updated map[string]time.Time
	ttl     time.Duration
	now     func() time.Time

	pruneCancel context.CancelFunc
	pruneDone   chan struct{}
}

// New constructs an empty tracker. Pass zero for ttl to use DefaultTTL.
func New(ttl time.Duration) *State {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &State{
		entries: make(map[string]*entry),
		updated: make(map[string]time.Time),
		ttl:     ttl,
		now:     time.Now,
	}
}

// SetNow overrides the clock for tests.
func (s *State) SetNow(f func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = f
}

// Observe records that peerID holds cid as of emittedAt.
func (s *State) Observe(peerID, cid string, emittedAt time.Time) {
	if peerID == "" || cid == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := emittedAt
	if ts.IsZero() {
		ts = s.now()
	}
	ent, ok := s.entries[cid]
	if !ok {
		ent = &entry{peers: make(map[string]time.Time)}
		s.entries[cid] = ent
	}
	if existing, had := ent.peers[peerID]; !had || ts.After(existing) {
		ent.peers[peerID] = ts
	}
	s.updated[cid] = s.now()
}

// Forget removes a single (peer, cid) observation.
func (s *State) Forget(peerID, cid string) {
	if peerID == "" || cid == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ent, ok := s.entries[cid]
	if !ok {
		return
	}
	delete(ent.peers, peerID)
	if len(ent.peers) == 0 {
		delete(s.entries, cid)
		delete(s.updated, cid)
		return
	}
	s.updated[cid] = s.now()
}

// ObserveHaveAdvertisement records every CID in a decoded
// HaveAdvertisement against the advertising peer.
func (s *State) ObserveHaveAdvertisement(ad *advertise.HaveAdvertisement) {
	if ad == nil || ad.PeerID == "" {
		return
	}
	ts := time.UnixMilli(ad.EmittedAtUnixMs)
	if ad.EmittedAtUnixMs == 0 {
		ts = time.Time{}
	}
	for _, cid := range ad.CIDs {
		s.Observe(ad.PeerID, cid, ts)
	}
}

// Count returns the current peer replication count for cid (TTL-filtered).
func (s *State) Count(cid string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.liveCountLocked(cid)
}

func (s *State) liveCountLocked(cid string) int {
	ent, ok := s.entries[cid]
	if !ok {
		return 0
	}
	cutoff := s.now().Add(-s.ttl)
	count := 0
	for _, ts := range ent.peers {
		if ts.After(cutoff) {
			count++
		}
	}
	return count
}

// RankRarest returns cids sorted ascending by live replication count.
func (s *State) RankRarest(cids []string) []string {
	if len(cids) == 0 {
		return nil
	}
	s.mu.Lock()
	counts := make(map[string]int, len(cids))
	for _, cid := range cids {
		counts[cid] = s.liveCountLocked(cid)
	}
	s.mu.Unlock()

	out := make([]string, len(cids))
	copy(out, cids)
	sort.SliceStable(out, func(i, j int) bool {
		return counts[out[i]] < counts[out[j]]
	})
	return out
}

// Snapshot returns a copy of (cid → live count).
func (s *State) Snapshot() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int, len(s.entries))
	for cid := range s.entries {
		out[cid] = s.liveCountLocked(cid)
	}
	return out
}

// KnownCIDs returns the set of observed CIDs regardless of TTL.
func (s *State) KnownCIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.entries))
	for cid := range s.entries {
		out = append(out, cid)
	}
	return out
}

// ActiveCIDs returns CIDs with at least one peer observation inside TTL.
func (s *State) ActiveCIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := s.now().Add(-s.ttl)
	out := make([]string, 0, len(s.entries))
	for cid, ent := range s.entries {
		for _, ts := range ent.peers {
			if ts.After(cutoff) {
				out = append(out, cid)
				break
			}
		}
	}
	return out
}

// PeersForCID returns peerIDs holding cid (TTL-filtered).
func (s *State) PeersForCID(cid string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ent, ok := s.entries[cid]
	if !ok {
		return nil
	}
	cutoff := s.now().Add(-s.ttl)
	out := make([]string, 0, len(ent.peers))
	for peerID, ts := range ent.peers {
		if ts.After(cutoff) {
			out = append(out, peerID)
		}
	}
	return out
}

// KnownPeerCount returns distinct peers across all CIDs.
func (s *State) KnownPeerCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[string]struct{})
	for _, ent := range s.entries {
		for peerID := range ent.peers {
			seen[peerID] = struct{}{}
		}
	}
	return len(seen)
}

// StartPruner launches a background goroutine that expires stale entries.
func (s *State) StartPruner(ctx context.Context, period time.Duration) {
	if period <= 0 {
		period = s.ttl
	}
	s.mu.Lock()
	if s.pruneCancel != nil {
		s.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	s.pruneCancel = cancel
	s.pruneDone = make(chan struct{})
	done := s.pruneDone
	s.mu.Unlock()

	go func() {
		defer close(done)
		t := time.NewTicker(period)
		defer t.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-t.C:
				s.Prune()
			}
		}
	}()
}

// StopPruner halts the background pruner.
func (s *State) StopPruner() {
	s.mu.Lock()
	cancel := s.pruneCancel
	done := s.pruneDone
	s.pruneCancel = nil
	s.pruneDone = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// Prune removes entries older than TTL.
func (s *State) Prune() {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := s.now().Add(-s.ttl)
	for cid, ent := range s.entries {
		for peer, ts := range ent.peers {
			if ts.Before(cutoff) || ts.Equal(cutoff) {
				delete(ent.peers, peer)
			}
		}
		if len(ent.peers) == 0 {
			delete(s.entries, cid)
			delete(s.updated, cid)
		}
	}
}
