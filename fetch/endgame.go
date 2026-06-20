/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package fetch

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bbmumford/diststore/chunker"
	"github.com/bbmumford/diststore/resolve"
)

// DefaultEndgameThreshold is the completion fraction past which the
// endgame fetcher escalates to duplicate-fetch mode.
const DefaultEndgameThreshold = 0.99

// DefaultEndgameMinRemaining keeps endgame off for tiny files.
const DefaultEndgameMinRemaining = 2

// EndgameConfig tunes when + how aggressively duplicate-fetch kicks in.
type EndgameConfig struct {
	Threshold    float64
	MinRemaining int
	Now          func() time.Time
}

// EndgameFetcher wraps Fetcher with BitTorrent-style endgame mode.
type EndgameFetcher struct {
	fetcher  *Fetcher
	resolver *resolve.Resolver
	cfg      EndgameConfig
	now      func() time.Time
}

// NewEndgame wraps a Fetcher + Resolver into an EndgameFetcher.
func NewEndgame(f *Fetcher, r *resolve.Resolver, cfg EndgameConfig) (*EndgameFetcher, error) {
	if f == nil {
		return nil, errors.New("fetch: endgame needs non-nil fetcher")
	}
	if r == nil {
		return nil, errors.New("fetch: endgame needs non-nil resolver")
	}
	if cfg.Threshold <= 0 {
		cfg.Threshold = DefaultEndgameThreshold
	}
	if cfg.Threshold >= 1 {
		return nil, fmt.Errorf("fetch: endgame threshold %.3f >= 1", cfg.Threshold)
	}
	if cfg.MinRemaining <= 0 {
		cfg.MinRemaining = DefaultEndgameMinRemaining
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &EndgameFetcher{fetcher: f, resolver: r, cfg: cfg, now: now}, nil
}

// FetchFile downloads every piece in manifest.Pieces in order.
func (e *EndgameFetcher) FetchFile(ctx context.Context, manifest *chunker.Manifest) ([][]byte, error) {
	if manifest == nil {
		return nil, errors.New("fetch: endgame FetchFile nil manifest")
	}
	n := len(manifest.Pieces)
	if n == 0 {
		return nil, errors.New("fetch: endgame FetchFile empty manifest")
	}

	pieces := make([][]byte, n)
	held := make([]bool, n)
	var mu sync.Mutex
	heldCount := 0

	for i := 0; i < n; i++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		mu.Lock()
		done := heldCount
		mu.Unlock()
		if e.shouldEnterEndgame(done, n) {
			break
		}
		p := manifest.Pieces[i]
		data, err := e.fetcher.Fetch(ctx, p.CID)
		if err != nil {
			return nil, fmt.Errorf("fetch: endgame stage-1 piece %d (%s): %w", i, p.CID, err)
		}
		mu.Lock()
		pieces[i] = data
		held[i] = true
		heldCount++
		mu.Unlock()
	}

	remaining := e.remainingIndexes(held)
	if len(remaining) == 0 {
		return pieces, nil
	}
	if err := e.fetchEndgameBatch(ctx, manifest, remaining, pieces, held, &mu); err != nil {
		return nil, err
	}
	return pieces, nil
}

func (e *EndgameFetcher) shouldEnterEndgame(done, total int) bool {
	if total == 0 {
		return false
	}
	remaining := total - done
	if remaining < e.cfg.MinRemaining {
		return false
	}
	ratio := float64(done) / float64(total)
	return ratio >= e.cfg.Threshold
}

func (e *EndgameFetcher) remainingIndexes(held []bool) []int {
	out := make([]int, 0, len(held))
	for i, h := range held {
		if !h {
			out = append(out, i)
		}
	}
	return out
}

func (e *EndgameFetcher) fetchEndgameBatch(
	ctx context.Context,
	manifest *chunker.Manifest,
	indexes []int,
	pieces [][]byte,
	held []bool,
	mu *sync.Mutex,
) error {
	var wg sync.WaitGroup
	errs := make(chan error, len(indexes))

	for _, idx := range indexes {
		idx := idx
		p := manifest.Pieces[idx]
		wg.Add(1)
		go func() {
			defer wg.Done()
			answers, err := e.resolver.Find(ctx, p.CID)
			if err != nil {
				errs <- fmt.Errorf("fetch: endgame resolve piece %d (%s): %w", idx, p.CID, err)
				return
			}
			if len(answers) == 0 {
				data, ferr := e.fetcher.Fetch(ctx, p.CID)
				if ferr != nil {
					errs <- fmt.Errorf("fetch: endgame fallback piece %d (%s): %w", idx, p.CID, ferr)
					return
				}
				mu.Lock()
				pieces[idx] = data
				held[idx] = true
				mu.Unlock()
				return
			}
			data, err := e.raceAll(ctx, p.CID, answers)
			if err != nil {
				errs <- fmt.Errorf("fetch: endgame race piece %d (%s): %w", idx, p.CID, err)
				return
			}
			mu.Lock()
			pieces[idx] = data
			held[idx] = true
			mu.Unlock()
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func (e *EndgameFetcher) raceAll(ctx context.Context, cid string, answers []resolve.PeerAnswer) ([]byte, error) {
	raceCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		data []byte
		err  error
	}
	winner := make(chan result, 1)
	var wg sync.WaitGroup

	for _, a := range answers {
		if a.PeerID == "" {
			continue
		}
		a := a
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := e.fetcher.Transport().FetchPiece(raceCtx, a.PeerID, FetchRequest{CID: cid})
			if err != nil {
				return
			}
			if resp.CID != "" && resp.CID != cid {
				return
			}
			if chunker.ComputePieceCID(resp.Payload) != cid {
				return
			}
			select {
			case winner <- result{data: resp.Payload}:
				cancel()
			default:
			}
		}()
	}

	go func() {
		wg.Wait()
		select {
		case winner <- result{err: errors.New("fetch: endgame race exhausted all peers")}:
		default:
		}
	}()

	select {
	case r := <-winner:
		return r.data, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
