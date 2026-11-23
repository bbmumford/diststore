/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

// Package fetch resolves candidate peers + races parallel piece fetches
// over a PeerTransport.
//
// Bonus enhancements (budget.go + verifier.go) add per-peer bandwidth
// budgeting and per-chunk BLAKE3/BLAKE2b pin verification.
package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/bbmumford/diststore/chunker"
	"github.com/bbmumford/diststore/resolve"
)

// BlobFetchStreamID is the Aether application-level stream ID reserved
// for the blob-fetch subprotocol.
const BlobFetchStreamID uint64 = 300

// Sentinel errors.
var (
	ErrNoPeers          = errors.New("fetch: no peers know this cid")
	ErrAllFetchesFailed = errors.New("fetch: all peer fetches failed")
)

// FetchRequest is the message sent over the fetch stream.
type FetchRequest struct {
	CID    string `json:"cid"`
	Offset int64  `json:"offset,omitempty"`
	Length int64  `json:"length,omitempty"`
}

// FetchResponse is the server-side reply.
type FetchResponse struct {
	CID     string `json:"cid"`
	Payload []byte `json:"payload,omitempty"`
	Error   string `json:"error,omitempty"`
}

// PeerTransport is the per-peer fetch RPC surface the fetcher needs.
// A single FetchPiece call must return the bytes for one piece or an
// error; the fetcher races requests across peers. Concrete impls in
// production wiring include aether.Session-backed pools and the
// in-memory fakes under internal/mocktransport.
type PeerTransport interface {
	FetchPiece(ctx context.Context, peerID string, req FetchRequest) (FetchResponse, error)
}

// Reputation scores a peer on a [0.0, 1.0) band where higher is better.
type Reputation interface {
	Score(peerID string) float64
}

// DefaultReputation scores every peer at 1.0.
type DefaultReputation struct{}

// Score implements Reputation.
func (DefaultReputation) Score(_ string) float64 { return 1.0 }

// Config tunes parallelism, peer selection and timeouts.
type Config struct {
	TopK           int
	PerPeerTimeout time.Duration
	OverallTimeout time.Duration
	Reputation     Reputation
	// Budget, if non-nil, enforces a per-peer bandwidth budget.
	Budget *BandwidthBudget
}

// Fetcher resolves candidate peers, races parallel piece fetches,
// verifies CIDs, and hands plaintext back.
type Fetcher struct {
	resolver  *resolve.Resolver
	transport PeerTransport
	cfg       Config
}

// New wires a Resolver to a PeerTransport.
func New(r *resolve.Resolver, t PeerTransport, cfg Config) (*Fetcher, error) {
	if r == nil {
		return nil, errors.New("fetch: needs non-nil resolver")
	}
	if t == nil {
		return nil, errors.New("fetch: needs non-nil transport")
	}
	if cfg.TopK <= 0 {
		cfg.TopK = 3
	}
	if cfg.PerPeerTimeout <= 0 {
		cfg.PerPeerTimeout = 30 * time.Second
	}
	if cfg.OverallTimeout <= 0 {
		cfg.OverallTimeout = 2 * cfg.PerPeerTimeout
	}
	if cfg.Reputation == nil {
		cfg.Reputation = DefaultReputation{}
	}
	return &Fetcher{resolver: r, transport: t, cfg: cfg}, nil
}

// Transport exposes the underlying transport so endgame can race all
// known holders directly.
func (f *Fetcher) Transport() PeerTransport { return f.transport }

// Fetch retrieves a single piece identified by cid.
func (f *Fetcher) Fetch(ctx context.Context, cid string) ([]byte, error) {
	data, _, err := f.FetchWithPeer(ctx, cid)
	return data, err
}

// FetchWithPeer behaves like Fetch but additionally returns the peerID
// of the winning candidate.
func (f *Fetcher) FetchWithPeer(ctx context.Context, cid string) ([]byte, string, error) {
	if cid == "" {
		return nil, "", errors.New("fetch: needs non-empty cid")
	}
	if f.cfg.OverallTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.cfg.OverallTimeout)
		defer cancel()
	}

	answers, err := f.resolver.Find(ctx, cid)
	if err != nil {
		return nil, "", fmt.Errorf("fetch: resolve: %w", err)
	}
	if len(answers) == 0 {
		return nil, "", ErrNoPeers
	}
	candidates := rankCandidates(answers, f.cfg.Reputation, f.cfg.TopK)
	if len(candidates) == 0 {
		return nil, "", ErrNoPeers
	}

	raceCtx, cancelRace := context.WithCancel(ctx)
	defer cancelRace()

	type result struct {
		data []byte
		err  error
		peer string
	}
	results := make(chan result, len(candidates))
	var wg sync.WaitGroup
	for _, peer := range candidates {
		wg.Add(1)
		go func(peerID string) {
			defer wg.Done()
			if f.cfg.Budget != nil {
				if !f.cfg.Budget.Acquire(peerID) {
					results <- result{err: fmt.Errorf("peer %s: bandwidth budget exhausted", peerID), peer: peerID}
					return
				}
				defer f.cfg.Budget.Release(peerID)
			}
			attemptCtx, cancel := context.WithTimeout(raceCtx, f.cfg.PerPeerTimeout)
			defer cancel()

			resp, err := f.transport.FetchPiece(attemptCtx, peerID, FetchRequest{CID: cid})
			if err != nil {
				results <- result{err: err, peer: peerID}
				return
			}
			if resp.Error != "" {
				results <- result{err: fmt.Errorf("peer %s: %s", peerID, resp.Error), peer: peerID}
				return
			}
			if resp.CID != "" && resp.CID != cid {
				results <- result{err: fmt.Errorf("peer %s: cid echo mismatch %s", peerID, resp.CID), peer: peerID}
				return
			}
			if chunker.ComputePieceCID(resp.Payload) != cid {
				results <- result{err: fmt.Errorf("peer %s: cid verification failed", peerID), peer: peerID}
				return
			}
			results <- result{data: resp.Payload, peer: peerID}
		}(peer)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var errs []error
	for r := range results {
		if r.err != nil {
			errs = append(errs, r.err)
			continue
		}
		cancelRace()
		return r.data, r.peer, nil
	}

	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}
	return nil, "", fmt.Errorf("%w: %d attempts: %v", ErrAllFetchesFailed, len(errs), errs)
}

// rankCandidates sorts answers by score = reputation / (1 + latencyHintMs/1000).
func rankCandidates(answers []resolve.PeerAnswer, rep Reputation, topK int) []string {
	type scored struct {
		peerID string
		score  float64
	}
	ranked := make([]scored, 0, len(answers))
	for _, a := range answers {
		reputation := rep.Score(a.PeerID)
		if reputation <= 0 {
			continue
		}
		latencyFactor := 1.0 + float64(a.LatencyHintMs)/1000.0
		if latencyFactor < 1.0 {
			latencyFactor = 1.0
		}
		ranked = append(ranked, scored{peerID: a.PeerID, score: reputation / latencyFactor})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return ranked[i].peerID < ranked[j].peerID
		}
		return ranked[i].score > ranked[j].score
	})
	if topK > 0 && len(ranked) > topK {
		ranked = ranked[:topK]
	}
	out := make([]string, 0, len(ranked))
	for _, s := range ranked {
		out = append(out, s.peerID)
	}
	return out
}

// EncodeFetchRequest is a helper for transport impls.
func EncodeFetchRequest(r FetchRequest) ([]byte, error) { return json.Marshal(&r) }

// DecodeFetchRequest reverses EncodeFetchRequest.
func DecodeFetchRequest(b []byte) (FetchRequest, error) {
	var r FetchRequest
	if err := json.Unmarshal(b, &r); err != nil {
		return FetchRequest{}, err
	}
	return r, nil
}

// EncodeFetchResponse is a helper for transport impls.
func EncodeFetchResponse(r FetchResponse) ([]byte, error) { return json.Marshal(&r) }

// DecodeFetchResponse reverses EncodeFetchResponse.
func DecodeFetchResponse(b []byte) (FetchResponse, error) {
	var r FetchResponse
	if err := json.Unmarshal(b, &r); err != nil {
		return FetchResponse{}, err
	}
	return r, nil
}
