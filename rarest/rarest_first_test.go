/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package rarest

import (
	"sync"
	"testing"
	"time"

	"github.com/bbmumford/diststore/advertise"
)

func TestObserveAndCount(t *testing.T) {
	s := New(time.Minute)
	s.Observe("peer-a", "cid-1", time.Now())
	s.Observe("peer-b", "cid-1", time.Now())
	s.Observe("peer-a", "cid-2", time.Now())
	if got := s.Count("cid-1"); got != 2 {
		t.Errorf("cid-1 count = %d, want 2", got)
	}
	if got := s.Count("cid-2"); got != 1 {
		t.Errorf("cid-2 count = %d, want 1", got)
	}
}

func TestForget(t *testing.T) {
	s := New(time.Minute)
	s.Observe("peer-a", "cid-1", time.Now())
	s.Observe("peer-b", "cid-1", time.Now())
	s.Forget("peer-a", "cid-1")
	if got := s.Count("cid-1"); got != 1 {
		t.Errorf("after forget count = %d, want 1", got)
	}
}

func TestTTLExpiry(t *testing.T) {
	now := time.Now()
	fakeClock := now
	s := New(100 * time.Millisecond)
	s.SetNow(func() time.Time { return fakeClock })
	s.Observe("peer-a", "cid-1", fakeClock)
	fakeClock = now.Add(200 * time.Millisecond)
	if got := s.Count("cid-1"); got != 0 {
		t.Errorf("after TTL count = %d, want 0", got)
	}
}

func TestRankRarest(t *testing.T) {
	s := New(time.Minute)
	for _, p := range []string{"p1", "p2", "p3"} {
		s.Observe(p, "cid-a", time.Now())
	}
	s.Observe("p1", "cid-b", time.Now())
	ranked := s.RankRarest([]string{"cid-a", "cid-b", "cid-c"})
	if ranked[0] != "cid-c" || ranked[1] != "cid-b" || ranked[2] != "cid-a" {
		t.Errorf("ranked = %v want [cid-c cid-b cid-a]", ranked)
	}
}

func TestObserveHaveAdvertisement(t *testing.T) {
	s := New(time.Minute)
	ad := &advertise.HaveAdvertisement{
		PeerID:          "peer-x",
		EmittedAtUnixMs: time.Now().UnixMilli(),
		CIDs:            []string{"c1", "c2", "c3"},
	}
	s.ObserveHaveAdvertisement(ad)
	for _, cid := range []string{"c1", "c2", "c3"} {
		if got := s.Count(cid); got != 1 {
			t.Errorf("cid %s count = %d", cid, got)
		}
	}
}

func TestConcurrentSafe(t *testing.T) {
	s := New(time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s.Observe("peer", "cid", time.Now())
				_ = s.Count("cid")
			}
		}()
	}
	wg.Wait()
}
