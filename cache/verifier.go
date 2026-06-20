/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package cache

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// VerifierConfig tunes the anti-tamper sweep.
type VerifierConfig struct {
	// Interval is the gap between sweeps. 0 = 24h.
	Interval time.Duration
	// MaxPerSweep caps how many chunks the verifier re-hashes per sweep
	// so a large cache doesn't burn cycles all at once. 0 = unlimited.
	MaxPerSweep int
	// OnCorruption is called for each cid whose backend bytes no longer
	// hash back to cid. Default action is Delete; consumers can override
	// to log + quarantine instead.
	OnCorruption func(cid string)
}

// Verifier runs periodic re-hash sweeps over the cache, evicting any
// chunk whose on-disk bytes have been tampered with.
type Verifier struct {
	store   *Store
	cfg     VerifierConfig
	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewVerifier wires a Verifier to a Store.
func NewVerifier(s *Store, cfg VerifierConfig) *Verifier {
	if cfg.Interval <= 0 {
		cfg.Interval = 24 * time.Hour
	}
	if cfg.OnCorruption == nil {
		cfg.OnCorruption = func(cid string) {
			_ = s.Delete(context.Background(), cid)
		}
	}
	return &Verifier{store: s, cfg: cfg}
}

// Start launches the sweep loop. Idempotent.
func (v *Verifier) Start(ctx context.Context) {
	v.mu.Lock()
	if v.running {
		v.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	v.cancel = cancel
	v.done = make(chan struct{})
	v.running = true
	v.mu.Unlock()
	go v.loop(loopCtx)
}

// Stop halts the sweep loop.
func (v *Verifier) Stop() {
	v.mu.Lock()
	if !v.running {
		v.mu.Unlock()
		return
	}
	c := v.cancel
	d := v.done
	v.running = false
	v.mu.Unlock()
	if c != nil {
		c()
	}
	if d != nil {
		<-d
	}
}

func (v *Verifier) loop(ctx context.Context) {
	defer close(v.done)
	t := time.NewTicker(v.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = v.Sweep(ctx)
		}
	}
}

// Sweep runs one synchronous pass over the cache. Returns the first
// fatal error (typically ctx cancellation).
func (v *Verifier) Sweep(ctx context.Context) error {
	cids, err := v.store.List(ctx)
	if err != nil {
		return fmt.Errorf("cache: verifier list: %w", err)
	}
	max := v.cfg.MaxPerSweep
	for i, cid := range cids {
		if err := ctx.Err(); err != nil {
			return err
		}
		if max > 0 && i >= max {
			return nil
		}
		if _, err := v.store.Get(ctx, cid); err != nil {
			// Get already verifies cid → plaintext hash → cid (in
			// store.getLocal). A mismatch surfaces as a non-Found error
			// which we treat as corruption.
			if v.cfg.OnCorruption != nil {
				v.cfg.OnCorruption(cid)
			}
		}
	}
	return nil
}
