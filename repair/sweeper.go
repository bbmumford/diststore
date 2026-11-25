/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package repair

import (
	"context"
	"sync"
	"time"
)

// DefaultBandwidthFraction is the share of free bandwidth the sweeper
// is allowed to consume by default.
const DefaultBandwidthFraction = 0.10

// SweeperConfig tunes the background loop.
type SweeperConfig struct {
	// Interval between scans of the queue. 0 = 5 minutes.
	Interval time.Duration
	// BandwidthFraction is the cap on free-bandwidth usage. 0 = default.
	BandwidthFraction float64
	// MaxConcurrent caps in-flight repair jobs. 0 = 1.
	MaxConcurrent int
}

// RepairFunc is called for each item the sweeper pops. The consumer
// hooks this up to its actual re-replication path (typically a call
// into replicate.Replicator with a per-blob target factor).
type RepairFunc func(ctx context.Context, item *Item) error

// Sweeper runs the repair loop in the background.
type Sweeper struct {
	queue   *Queue
	cfg     SweeperConfig
	repair  RepairFunc
	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewSweeper constructs a Sweeper.
func NewSweeper(q *Queue, cfg SweeperConfig, fn RepairFunc) *Sweeper {
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Minute
	}
	if cfg.BandwidthFraction <= 0 {
		cfg.BandwidthFraction = DefaultBandwidthFraction
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 1
	}
	return &Sweeper{queue: q, cfg: cfg, repair: fn}
}

// Start launches the background loop. Idempotent.
func (s *Sweeper) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	loopCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.mu.Unlock()
	go s.loop(loopCtx)
}

// Stop halts the loop.
func (s *Sweeper) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	cancel := s.cancel
	done := s.done
	s.running = false
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (s *Sweeper) loop(ctx context.Context) {
	defer close(s.done)
	t := time.NewTicker(s.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.drain(ctx)
		}
	}
}

func (s *Sweeper) drain(ctx context.Context) {
	sem := make(chan struct{}, s.cfg.MaxConcurrent)
	var wg sync.WaitGroup
	for ctx.Err() == nil {
		item := s.queue.Pop()
		if item == nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(it *Item) {
			defer wg.Done()
			defer func() { <-sem }()
			if s.repair != nil {
				_ = s.repair(ctx, it)
			}
		}(item)
	}
	wg.Wait()
}
