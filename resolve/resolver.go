/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

// Package resolve implements the "who has piece X?" request/response
// protocol over a transport.RequestTopic.
package resolve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bbmumford/diststore/transport"
)

// TopicBlobFind is the RequestResponse topic for "who has CID X?"
// queries.
const TopicBlobFind = "plutus.blob.find"

// FindRequest is the payload sent on a TopicBlobFind query.
type FindRequest struct {
	CID             string `json:"cid"`
	RequesterPeerID string `json:"requesterPeerId,omitempty"`
	Nonce           string `json:"nonce,omitempty"`
}

// FindResponse is the payload returned by a peer that holds the CID.
type FindResponse struct {
	Answers []PeerAnswer `json:"answers"`
	Nonce   string       `json:"nonce,omitempty"`
}

// PeerAnswer describes one peer that claims to hold a CID.
type PeerAnswer struct {
	PeerID         string `json:"peerId"`
	LastSeenUnixMs int64  `json:"lastSeenMs"`
	LatencyHintMs  int64  `json:"latencyMs,omitempty"`
}

// HasChecker is the subset of cache.Store used by the responder.
type HasChecker interface {
	Has(ctx context.Context, cid string) (bool, error)
}

// Config tunes responder behaviour + self-identity.
type Config struct {
	PeerID        string
	Topic         string
	Now           func() time.Time
	LatencyHintMs int64
}

// Resolver owns both halves of the blob-find protocol.
type Resolver struct {
	store HasChecker
	q     transport.RequestTopic
	cfg   Config
	topic string
	now   func() time.Time
}

// New constructs a Resolver. The transport may be nil on a
// responder-only node; Find will return an error in that case.
func New(store HasChecker, q transport.RequestTopic, cfg Config) (*Resolver, error) {
	if store == nil {
		return nil, errors.New("resolve: needs non-nil store")
	}
	if cfg.PeerID == "" {
		return nil, errors.New("resolve: needs non-empty PeerID")
	}
	topic := cfg.Topic
	if topic == "" {
		topic = TopicBlobFind
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Resolver{
		store: store,
		q:     q,
		cfg:   cfg,
		topic: topic,
		now:   now,
	}, nil
}

// Topic returns the effective topic name.
func (r *Resolver) Topic() string { return r.topic }

// OnMessage implements the responder-side TopicHandler contract.
func (r *Resolver) OnMessage(ctx context.Context, from string, topic string, payload []byte) ([]byte, error) {
	if topic != r.topic {
		return nil, fmt.Errorf("resolve: topic mismatch: %q", topic)
	}
	req, err := decodeFindRequest(payload)
	if err != nil {
		return nil, err
	}
	if req.CID == "" {
		return nil, nil
	}
	has, err := r.store.Has(ctx, req.CID)
	if err != nil {
		return nil, fmt.Errorf("resolve: has-check: %w", err)
	}
	if !has {
		return nil, nil
	}
	resp := FindResponse{
		Answers: []PeerAnswer{{
			PeerID:         r.cfg.PeerID,
			LastSeenUnixMs: r.now().UnixMilli(),
			LatencyHintMs:  r.cfg.LatencyHintMs,
		}},
		Nonce: req.Nonce,
	}
	return json.Marshal(&resp)
}

// Find broadcasts a FindRequest and aggregates de-duplicated responses.
func (r *Resolver) Find(ctx context.Context, cid string) ([]PeerAnswer, error) {
	if r.q == nil {
		return nil, errors.New("resolve: no transport configured for Find")
	}
	if cid == "" {
		return nil, errors.New("resolve: Find needs non-empty cid")
	}
	req := FindRequest{
		CID:             cid,
		RequesterPeerID: r.cfg.PeerID,
	}
	payload, err := json.Marshal(&req)
	if err != nil {
		return nil, fmt.Errorf("resolve: Find marshal: %w", err)
	}
	raw, err := r.q.Query(ctx, r.topic, payload)
	if err != nil {
		return nil, fmt.Errorf("resolve: Find query: %w", err)
	}
	return dedup(decodeAnswers(raw)), nil
}

func decodeFindRequest(b []byte) (*FindRequest, error) {
	if len(b) == 0 {
		return nil, errors.New("resolve: empty find-request")
	}
	var req FindRequest
	if err := json.Unmarshal(b, &req); err != nil {
		return nil, fmt.Errorf("resolve: find-request decode: %w", err)
	}
	return &req, nil
}

func decodeAnswers(responses [][]byte) []PeerAnswer {
	var out []PeerAnswer
	for _, raw := range responses {
		if len(raw) == 0 {
			continue
		}
		var resp FindResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			continue
		}
		for _, a := range resp.Answers {
			if a.PeerID == "" {
				continue
			}
			out = append(out, a)
		}
	}
	return out
}

func dedup(in []PeerAnswer) []PeerAnswer {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]int, len(in))
	out := make([]PeerAnswer, 0, len(in))
	for _, a := range in {
		if idx, ok := seen[a.PeerID]; ok {
			if a.LastSeenUnixMs > out[idx].LastSeenUnixMs {
				out[idx] = a
			}
			continue
		}
		seen[a.PeerID] = len(out)
		out = append(out, a)
	}
	return out
}
