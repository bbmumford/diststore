/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

// Package replicate runs the opt-in seed loop: periodically scan for
// rarer CIDs the local cache is missing, fetch them, and (optionally)
// re-advertise.
package replicate

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/bbmumford/diststore/abuse"
	"github.com/bbmumford/diststore/advertise"
	"github.com/bbmumford/diststore/cache"
	"github.com/bbmumford/diststore/fetch"
	"github.com/bbmumford/diststore/metrics"
	"github.com/bbmumford/diststore/rarest"
)

// DefaultFetchInterval is the replicator tick cadence.
const DefaultFetchInterval = 5 * time.Minute

// DefaultMaxParallelJobs caps concurrent piece fetches per tick.
const DefaultMaxParallelJobs = 4

// Config tunes the seed loop.
type Config struct {
	PeerID                string
	FetchInterval         time.Duration
	MaxParallelJobs       int
	RarestFirst           *rarest.State
	Candidates            CandidateSource
	AbuseGuard            *abuse.Guard
	ReplicationRecordType abuse.RecordType
	Now                   func() time.Time
}

// Replicator is the opt-in seed loop.
type Replicator struct {
	store      *cache.Store
	advertiser *advertise.Advertiser
	resolver   *fetch.Fetcher // accessed only for FetchWithPeer
	fetcher    *fetch.Fetcher
	cfg        Config
	interval   time.Duration
	maxJobs    int
	now        func() time.Time
	candidates CandidateSource
	abuseGuard *abuse.Guard
	replType   abuse.RecordType
	log        metricsLogger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// metricsLogger is a tiny indirection so we can swap loggers in tests.
type metricsLogger interface {
	Warn(msg string, args ...any)
}

// New wires up the seed loop.
func New(cfg Config, store *cache.Store, ad *advertise.Advertiser, f *fetch.Fetcher) (*Replicator, error) {
	if store == nil {
		return nil, errors.New("replicate: needs non-nil store")
	}
	if f == nil {
		return nil, errors.New("replicate: needs non-nil fetcher")
	}
	if cfg.PeerID == "" {
		return nil, errors.New("replicate: needs non-empty PeerID")
	}
	if cfg.RarestFirst == nil {
		return nil, errors.New("replicate: needs non-nil RarestFirst state")
	}
	interval := cfg.FetchInterval
	if interval <= 0 {
		interval = DefaultFetchInterval
	}
	maxJobs := cfg.MaxParallelJobs
	if maxJobs <= 0 {
		maxJobs = DefaultMaxParallelJobs
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	candidates := cfg.Candidates
	if candidates == nil {
		rf := cfg.RarestFirst
		candidates = CandidateSourceFunc(func(ctx context.Context) ([]string, error) {
			return rf.KnownCIDs(), nil
		})
	}
	replType := cfg.ReplicationRecordType
	if replType == "" {
		replType = abuse.RecordTypeReplication
	}
	return &Replicator{
		store:      store,
		advertiser: ad,
		fetcher:    f,
		cfg:        cfg,
		interval:   interval,
		maxJobs:    maxJobs,
		now:        now,
		candidates: candidates,
		abuseGuard: cfg.AbuseGuard,
		replType:   replType,
		log:        metrics.Log("replicate"),
	}, nil
}

// Start launches the seed loop.
func (r *Replicator) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return errors.New("replicate: already running")
	}
	r.running = true
	r.done = make(chan struct{})
	loopCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.mu.Unlock()
	go r.loop(loopCtx)
	return nil
}

// Stop halts the seed loop and waits for any in-flight tick.
func (r *Replicator) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	r.running = false
	cancel := r.cancel
	done := r.done
	r.cancel = nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (r *Replicator) loop(ctx context.Context) {
	defer func() {
		r.mu.Lock()
		done := r.done
		r.done = nil
		r.mu.Unlock()
		if done != nil {
			close(done)
		}
	}()
	r.scan(ctx)
	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.scan(ctx)
		}
	}
}

// scan runs one tick of the seed loop. The loop has no caller to hand an error
// to, so a failed scan is logged and the next tick tries again; a scan that
// Stop cut short is not a failure.
func (r *Replicator) scan(ctx context.Context) {
	if err := r.tickOnce(ctx); err != nil && ctx.Err() == nil {
		r.log.Warn("replicate: scan failed", "err", err)
	}
}

// Tick runs one synchronous replication scan.
func (r *Replicator) Tick(ctx context.Context) error { return r.tickOnce(ctx) }

func (r *Replicator) tickOnce(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cids, err := r.candidates.Candidates(ctx)
	if err != nil {
		return err
	}
	missing := r.filterMissing(ctx, cids)
	if len(missing) == 0 {
		return nil
	}
	ordered := r.cfg.RarestFirst.RankRarest(missing)
	return r.fetchBatch(ctx, ordered)
}

func (r *Replicator) filterMissing(ctx context.Context, cids []string) []string {
	out := make([]string, 0, len(cids))
	for _, cid := range cids {
		if cid == "" {
			continue
		}
		has, err := r.store.Has(ctx, cid)
		if err == nil && has {
			continue
		}
		out = append(out, cid)
	}
	return out
}

func (r *Replicator) fetchBatch(ctx context.Context, ordered []string) error {
	sem := make(chan struct{}, r.maxJobs)
	var wg sync.WaitGroup
	for _, cid := range ordered {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(cid string) {
			defer wg.Done()
			defer func() { <-sem }()
			data, fromPeer, err := r.fetcher.FetchWithPeer(ctx, cid)
			if err != nil {
				return
			}
			if r.abuseGuard != nil {
				if err := r.abuseGuard.Check(ctx, cid, fromPeer, int64(len(data)), r.replType); err != nil {
					if errors.Is(err, abuse.ErrAlreadyHeld) {
						return
					}
					r.log.Warn("replicate: abuse-check denied",
						"cid", cid, "from", fromPeer, "err", err)
					return
				}
			}
			if _, err := r.store.Put(ctx, data); err != nil {
				if r.abuseGuard != nil {
					r.abuseGuard.ReleaseReplication(cid, fromPeer)
				}
				return
			}
			if r.advertiser != nil {
				_ = r.advertiser.EmitOnce(ctx)
			}
		}(cid)
	}
	wg.Wait()
	return nil
}
