/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

// Package onchain implements proof-of-storage + paid-pinning workflows
// and declares the EscrowClient interface that plutus provides.
package onchain

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/blake2b"
)

// DefaultChallengeResponseTimeout bounds how long a challenger waits.
const DefaultChallengeResponseTimeout = 30 * time.Second

// ProofChallenge is the outbound query payload.
type ProofChallenge struct {
	CID       string    `json:"cid"`
	Nonce     []byte    `json:"nonce"`
	Timestamp time.Time `json:"ts"`
	Issuer    string    `json:"issuer,omitempty"`
}

// ProofResponse is the signed reply.
type ProofResponse struct {
	CID    string `json:"cid"`
	Nonce  []byte `json:"nonce"`
	Hash   []byte `json:"hash"`
	PubKey []byte `json:"pubkey"`
	Sig    []byte `json:"sig"`
}

// signMessage builds the canonical signed-over bytes.
func signMessage(cid string, nonce, hash, pub []byte) []byte {
	buf := make([]byte, 0, len(cid)+len(nonce)+len(hash)+len(pub)+16)
	buf = append(buf, []byte(cid)...)
	buf = append(buf, '\n')
	buf = append(buf, nonce...)
	buf = append(buf, '\n')
	buf = append(buf, hash...)
	buf = append(buf, '\n')
	buf = append(buf, pub...)
	return buf
}

// ComputeProofHash computes BLAKE2b-256(piece || nonce). OFF-CHAIN ONLY.
// For on-chain use call ComputeOnChainExpectedHash.
func ComputeProofHash(piece, nonce []byte) []byte {
	h, _ := blake2b.New256(nil)
	h.Write(piece)
	h.Write(nonce)
	return h.Sum(nil)
}

// ChallengeTransport is the per-peer ask/reply interface used by the
// issuer.
type ChallengeTransport interface {
	Ask(ctx context.Context, peerID string, payload []byte) ([]byte, error)
}

// ChallengeResult records the outcome of one Challenge call.
type ChallengeResult struct {
	CID      string
	PeerID   string
	Issued   time.Time
	Received time.Time
	OK       bool
	Reason   string
	Response *ProofResponse
}

// ChallengeIssuerConfig tunes the issuer side.
type ChallengeIssuerConfig struct {
	IssuerPeerID    string
	ResponseTimeout time.Duration
	Now             func() time.Time
}

// PieceSource is the minimum surface the issuer + responder need.
type PieceSource interface {
	Get(ctx context.Context, cid string) ([]byte, error)
	Has(ctx context.Context, cid string) (bool, error)
}

// ChallengeIssuer issues + verifies proof-of-storage challenges.
type ChallengeIssuer struct {
	transport ChallengeTransport
	cfg       ChallengeIssuerConfig
	now       func() time.Time
	timeout   time.Duration
	pieces    PieceSource

	mu      sync.Mutex
	results []ChallengeResult
}

// NewChallengeIssuer wires a transport. Without a PieceSource the
// issuer cannot cross-check responder hashes — see SECURITY NOTE.
func NewChallengeIssuer(t ChallengeTransport, cfg ChallengeIssuerConfig) (*ChallengeIssuer, error) {
	return NewChallengeIssuerWithPieceSource(t, nil, cfg)
}

// NewChallengeIssuerWithPieceSource is the production-grade constructor.
func NewChallengeIssuerWithPieceSource(t ChallengeTransport, pieces PieceSource, cfg ChallengeIssuerConfig) (*ChallengeIssuer, error) {
	timeout := cfg.ResponseTimeout
	if timeout <= 0 {
		timeout = DefaultChallengeResponseTimeout
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &ChallengeIssuer{
		transport: t,
		cfg:       cfg,
		now:       now,
		timeout:   timeout,
		pieces:    pieces,
	}, nil
}

// Challenge issues one proof-of-storage challenge.
func (ci *ChallengeIssuer) Challenge(ctx context.Context, peerID, cid string) (ChallengeResult, error) {
	if ci.transport == nil {
		return ChallengeResult{}, errors.New("onchain: challenge issuer has no transport")
	}
	if peerID == "" {
		return ChallengeResult{}, errors.New("onchain: empty peerID")
	}
	if cid == "" {
		return ChallengeResult{}, errors.New("onchain: empty cid")
	}

	nonce, err := NewNonce()
	if err != nil {
		return ChallengeResult{}, err
	}
	chal := ProofChallenge{
		CID:       cid,
		Nonce:     nonce,
		Timestamp: ci.now(),
		Issuer:    ci.cfg.IssuerPeerID,
	}
	payload, err := json.Marshal(&chal)
	if err != nil {
		return ChallengeResult{}, fmt.Errorf("onchain: challenge marshal: %w", err)
	}

	askCtx, cancel := context.WithTimeout(ctx, ci.timeout)
	defer cancel()

	raw, askErr := ci.transport.Ask(askCtx, peerID, payload)
	res := ChallengeResult{CID: cid, PeerID: peerID, Issued: chal.Timestamp}
	if askErr != nil {
		if errors.Is(askErr, context.DeadlineExceeded) || errors.Is(askErr, context.Canceled) {
			res.Reason = "timeout"
		} else {
			res.Reason = "transport:" + askErr.Error()
		}
		ci.recordResult(res)
		return res, nil
	}
	if len(raw) == 0 {
		res.Reason = "empty_response"
		res.Received = ci.now()
		ci.recordResult(res)
		return res, nil
	}

	var resp ProofResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		res.Reason = "decode_error"
		res.Received = ci.now()
		ci.recordResult(res)
		return res, nil
	}
	res.Response = &resp
	res.Received = ci.now()

	if resp.CID != cid {
		res.Reason = "cid_mismatch"
		ci.recordResult(res)
		return res, nil
	}
	if !bytes.Equal(resp.Nonce, nonce) {
		res.Reason = "nonce_mismatch"
		ci.recordResult(res)
		return res, nil
	}
	if len(resp.PubKey) != ed25519.PublicKeySize {
		res.Reason = "bad_pubkey"
		ci.recordResult(res)
		return res, nil
	}
	if !ed25519.Verify(ed25519.PublicKey(resp.PubKey),
		signMessage(resp.CID, resp.Nonce, resp.Hash, resp.PubKey), resp.Sig) {
		res.Reason = "sig_fail"
		ci.recordResult(res)
		return res, nil
	}

	if ci.pieces != nil {
		has, hErr := ci.pieces.Has(ctx, cid)
		if hErr == nil && has {
			piece, gErr := ci.pieces.Get(ctx, cid)
			if gErr == nil {
				expected := ComputeProofHash(piece, resp.Nonce)
				if !bytes.Equal(expected, resp.Hash) {
					res.Reason = "wrong_hash"
					ci.recordResult(res)
					return res, nil
				}
			}
		}
	}

	res.OK = true
	ci.recordResult(res)
	return res, nil
}

// VerifyResponse checks a ProofResponse against locally-held piece bytes.
func (ci *ChallengeIssuer) VerifyResponse(resp *ProofResponse, pieceBytes []byte) error {
	if resp == nil {
		return errors.New("onchain: nil response")
	}
	if len(resp.PubKey) != ed25519.PublicKeySize {
		return errors.New("onchain: bad pubkey length")
	}
	expected := ComputeProofHash(pieceBytes, resp.Nonce)
	if !bytes.Equal(expected, resp.Hash) {
		return fmt.Errorf("onchain: hash mismatch: expected %s got %s",
			hex.EncodeToString(expected), hex.EncodeToString(resp.Hash))
	}
	if !ed25519.Verify(ed25519.PublicKey(resp.PubKey),
		signMessage(resp.CID, resp.Nonce, resp.Hash, resp.PubKey), resp.Sig) {
		return errors.New("onchain: signature verification failed")
	}
	return nil
}

// RecentResults returns a snapshot of the last N challenge results.
func (ci *ChallengeIssuer) RecentResults() []ChallengeResult {
	ci.mu.Lock()
	defer ci.mu.Unlock()
	out := make([]ChallengeResult, len(ci.results))
	copy(out, ci.results)
	return out
}

const challengeResultCap = 256

func (ci *ChallengeIssuer) recordResult(r ChallengeResult) {
	ci.mu.Lock()
	defer ci.mu.Unlock()
	if len(ci.results) >= challengeResultCap {
		ci.results = ci.results[1:]
	}
	ci.results = append(ci.results, r)
}

// ChallengeResponderConfig tunes the replicator side.
type ChallengeResponderConfig struct {
	PeerID      string
	IdentityKey ed25519.PrivateKey
	Topic       string
	Now         func() time.Time
}

// ChallengeResponder handles inbound ProofChallenge messages.
type ChallengeResponder struct {
	store PieceSource
	cfg   ChallengeResponderConfig
	topic string
	now   func() time.Time
}

// NewChallengeResponder wires a store to a responder.
func NewChallengeResponder(store PieceSource, cfg ChallengeResponderConfig) (*ChallengeResponder, error) {
	if store == nil {
		return nil, errors.New("onchain: responder needs non-nil store")
	}
	if len(cfg.IdentityKey) != ed25519.PrivateKeySize {
		return nil, errors.New("onchain: responder needs valid Ed25519 private key")
	}
	topic := cfg.Topic
	if topic == "" {
		topic = TopicBlobChallenge
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &ChallengeResponder{store: store, cfg: cfg, topic: topic, now: now}, nil
}

// Topic returns the effective topic name.
func (r *ChallengeResponder) Topic() string { return r.topic }

// OnMessage implements the responder TopicHandler contract.
func (r *ChallengeResponder) OnMessage(ctx context.Context, from string, topic string, payload []byte) ([]byte, error) {
	if topic != r.topic {
		return nil, fmt.Errorf("onchain: responder topic mismatch: %q", topic)
	}
	if len(payload) == 0 {
		return nil, errors.New("onchain: empty challenge payload")
	}
	var chal ProofChallenge
	if err := json.Unmarshal(payload, &chal); err != nil {
		return nil, fmt.Errorf("onchain: challenge decode: %w", err)
	}
	if chal.CID == "" || len(chal.Nonce) == 0 {
		return nil, errors.New("onchain: challenge missing fields")
	}
	has, err := r.store.Has(ctx, chal.CID)
	if err != nil || !has {
		return nil, nil
	}
	piece, err := r.store.Get(ctx, chal.CID)
	if err != nil {
		return nil, nil
	}
	hash := ComputeProofHash(piece, chal.Nonce)
	pub := r.cfg.IdentityKey.Public().(ed25519.PublicKey)
	sig := ed25519.Sign(r.cfg.IdentityKey, signMessage(chal.CID, chal.Nonce, hash, pub))
	resp := ProofResponse{
		CID: chal.CID, Nonce: chal.Nonce, Hash: hash, PubKey: pub, Sig: sig,
	}
	return json.Marshal(&resp)
}

// BuildResponseFor produces a signed ProofResponse directly. Helper
// for tests + direct callers.
func BuildResponseFor(cid string, piece, nonce []byte, identityKey ed25519.PrivateKey) ProofResponse {
	hash := ComputeProofHash(piece, nonce)
	pub := identityKey.Public().(ed25519.PublicKey)
	sig := ed25519.Sign(identityKey, signMessage(cid, nonce, hash, pub))
	return ProofResponse{CID: cid, Nonce: nonce, Hash: hash, PubKey: pub, Sig: sig}
}
