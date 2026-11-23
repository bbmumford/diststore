/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package onchain

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"time"
)

// DefaultAcceptanceTimeout bounds the seller's wait for replicator
// acceptances.
const DefaultAcceptanceTimeout = 2 * time.Minute

// DefaultBondDeadlineSkew is how far in the future the seller will
// set the bond-deadline on the escrow contract.
const DefaultBondDeadlineSkew = 1 * time.Hour

// PinRequest is the seller's broadcast payload.
type PinRequest struct {
	RequestID             string   `json:"requestId"`
	CID                   string   `json:"cid"`
	DurationSeconds       int64    `json:"durationSeconds"`
	ReplicationFactor     int      `json:"replicationFactor"`
	FeePerReplicator      string   `json:"feePerReplicator"`
	TotalEscrowed         string   `json:"totalEscrowed"`
	AcceptedPaymentChains []string `json:"acceptedPaymentChains"`
	RecordType            string   `json:"recordType"`
	SellerPubKey          []byte   `json:"sellerPubKey"`
	SellerSig             []byte   `json:"sellerSig,omitempty"`
	IssuedAtUnixMs        int64    `json:"issuedAtMs"`
	SellerReputation      float64  `json:"sellerReputation,omitempty"`
}

// PinAcceptance is the replicator's broadcast reply.
type PinAcceptance struct {
	RequestID        string `json:"requestId"`
	CID              string `json:"cid"`
	ReplicatorID     string `json:"replicatorId"`
	ReplicatorPub    []byte `json:"replicatorPub"`
	EscrowChain      string `json:"escrowChain"`
	EscrowAddress    string `json:"escrowAddress"`
	AcceptedAtUnixMs int64  `json:"acceptedAtMs"`
	Sig              []byte `json:"sig,omitempty"`
}

// ReplicatorID is a lightweight alias.
type ReplicatorID = string

func (r PinRequest) signPayload() []byte {
	buf := bytes.NewBuffer(nil)
	buf.WriteString(r.RequestID)
	buf.WriteByte('\n')
	buf.WriteString(r.CID)
	buf.WriteByte('\n')
	fmt.Fprintf(buf, "%d\n%d\n", r.DurationSeconds, r.ReplicationFactor)
	buf.WriteString(r.FeePerReplicator)
	buf.WriteByte('\n')
	buf.WriteString(r.TotalEscrowed)
	buf.WriteByte('\n')
	for _, c := range r.AcceptedPaymentChains {
		buf.WriteString(c)
		buf.WriteByte(',')
	}
	buf.WriteByte('\n')
	buf.WriteString(r.RecordType)
	buf.WriteByte('\n')
	buf.Write(r.SellerPubKey)
	buf.WriteByte('\n')
	fmt.Fprintf(buf, "%d", r.IssuedAtUnixMs)
	return buf.Bytes()
}

// Sign fills in r.SellerSig.
func (r *PinRequest) Sign(key ed25519.PrivateKey) error {
	if len(key) != ed25519.PrivateKeySize {
		return errors.New("onchain: bad signer key")
	}
	r.SellerPubKey = key.Public().(ed25519.PublicKey)
	r.SellerSig = ed25519.Sign(key, r.signPayload())
	return nil
}

// Verify checks r.SellerSig.
func (r PinRequest) Verify() error {
	if len(r.SellerPubKey) != ed25519.PublicKeySize {
		return errors.New("onchain: bad seller pubkey")
	}
	if len(r.SellerSig) == 0 {
		return errors.New("onchain: request not signed")
	}
	if !ed25519.Verify(ed25519.PublicKey(r.SellerPubKey), r.signPayload(), r.SellerSig) {
		return errors.New("onchain: seller sig verification failed")
	}
	return nil
}

func (a PinAcceptance) signBytes() []byte {
	buf := bytes.NewBuffer(nil)
	buf.WriteString(a.RequestID)
	buf.WriteByte('\n')
	buf.WriteString(a.CID)
	buf.WriteByte('\n')
	buf.WriteString(a.ReplicatorID)
	buf.WriteByte('\n')
	buf.Write(a.ReplicatorPub)
	buf.WriteByte('\n')
	buf.WriteString(a.EscrowChain)
	buf.WriteByte('\n')
	buf.WriteString(a.EscrowAddress)
	buf.WriteByte('\n')
	fmt.Fprintf(buf, "%d", a.AcceptedAtUnixMs)
	return buf.Bytes()
}

// Sign fills in a.Sig.
func (a *PinAcceptance) Sign(key ed25519.PrivateKey) error {
	if len(key) != ed25519.PrivateKeySize {
		return errors.New("onchain: bad signer key")
	}
	a.ReplicatorPub = key.Public().(ed25519.PublicKey)
	a.Sig = ed25519.Sign(key, a.signBytes())
	return nil
}

// Verify checks a.Sig.
func (a PinAcceptance) Verify() error {
	if len(a.ReplicatorPub) != ed25519.PublicKeySize {
		return errors.New("onchain: bad replicator pubkey")
	}
	if len(a.Sig) == 0 {
		return errors.New("onchain: acceptance not signed")
	}
	if !ed25519.Verify(ed25519.PublicKey(a.ReplicatorPub), a.signBytes(), a.Sig) {
		return errors.New("onchain: acceptance sig verification failed")
	}
	return nil
}

// PinPublisher matches whisper.Engine.Publish.
type PinPublisher interface {
	Publish(topic string, payload []byte) error
}

// PinAcceptanceSubscriber is the seller-side subscription surface.
type PinAcceptanceSubscriber interface {
	SubscribeAcceptances(requestID string, handler func(PinAcceptance))
	UnsubscribeAcceptances(requestID string)
}

// PinnerConfig tunes the seller side.
type PinnerConfig struct {
	PeerID            string
	IdentityKey       ed25519.PrivateKey
	RequestTopic      string
	AcceptanceTimeout time.Duration
	Now               func() time.Time
}

// Pinner publishes pin requests + collects acceptances.
type Pinner struct {
	pub        PinPublisher
	subscriber PinAcceptanceSubscriber
	cfg        PinnerConfig
	topic      string
	timeout    time.Duration
	now        func() time.Time
}

// NewPinner wires publisher + subscriber.
func NewPinner(pub PinPublisher, sub PinAcceptanceSubscriber, cfg PinnerConfig) (*Pinner, error) {
	if pub == nil {
		return nil, errors.New("onchain: pinner needs publisher")
	}
	if len(cfg.IdentityKey) != ed25519.PrivateKeySize {
		return nil, errors.New("onchain: pinner needs valid Ed25519 private key")
	}
	topic := cfg.RequestTopic
	if topic == "" {
		topic = TopicPinRequest
	}
	timeout := cfg.AcceptanceTimeout
	if timeout <= 0 {
		timeout = DefaultAcceptanceTimeout
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Pinner{pub: pub, subscriber: sub, cfg: cfg, topic: topic, timeout: timeout, now: now}, nil
}

// PinRequestParams is the caller-facing shape for BuildRequest.
type PinRequestParams struct {
	RequestID             string
	CID                   string
	Duration              time.Duration
	ReplicationFactor     int
	FeePerReplicator      *big.Int
	AcceptedPaymentChains []string
	RecordType            string
	SellerReputation      float64
}

// BuildRequest constructs a signed PinRequest from params.
func (p *Pinner) BuildRequest(params PinRequestParams) (PinRequest, error) {
	if params.CID == "" {
		return PinRequest{}, errors.New("onchain: empty cid")
	}
	if params.Duration < 7*24*time.Hour {
		return PinRequest{}, errors.New("onchain: duration must be >= 1 week")
	}
	if params.ReplicationFactor <= 0 {
		return PinRequest{}, errors.New("onchain: replication factor must be > 0")
	}
	if params.FeePerReplicator == nil || params.FeePerReplicator.Sign() <= 0 {
		return PinRequest{}, errors.New("onchain: fee must be positive")
	}
	total := new(big.Int).Mul(params.FeePerReplicator, big.NewInt(int64(params.ReplicationFactor)))
	req := PinRequest{
		RequestID:             params.RequestID,
		CID:                   params.CID,
		DurationSeconds:       int64(params.Duration.Seconds()),
		ReplicationFactor:     params.ReplicationFactor,
		FeePerReplicator:      params.FeePerReplicator.String(),
		TotalEscrowed:         total.String(),
		AcceptedPaymentChains: append([]string(nil), params.AcceptedPaymentChains...),
		RecordType:            params.RecordType,
		IssuedAtUnixMs:        p.now().UnixMilli(),
		SellerReputation:      params.SellerReputation,
	}
	if req.RequestID == "" {
		req.RequestID = fmt.Sprintf("%x-%d", p.cfg.IdentityKey.Public().(ed25519.PublicKey)[:8], req.IssuedAtUnixMs)
	}
	if err := req.Sign(p.cfg.IdentityKey); err != nil {
		return PinRequest{}, err
	}
	return req, nil
}

// PublishPinRequest broadcasts + collects.
func (p *Pinner) PublishPinRequest(ctx context.Context, req PinRequest) ([]ReplicatorID, error) {
	if err := req.Verify(); err != nil {
		return nil, fmt.Errorf("onchain: refusing to publish invalid request: %w", err)
	}
	payload, err := json.Marshal(&req)
	if err != nil {
		return nil, fmt.Errorf("onchain: marshal pin request: %w", err)
	}
	if err := p.pub.Publish(p.topic, payload); err != nil {
		return nil, fmt.Errorf("onchain: publish: %w", err)
	}
	if p.subscriber == nil {
		return nil, nil
	}

	var (
		mu        sync.Mutex
		collected []PinAcceptance
		completed bool
		done      = make(chan struct{})
	)
	p.subscriber.SubscribeAcceptances(req.RequestID, func(a PinAcceptance) {
		if a.RequestID != req.RequestID || a.CID != req.CID {
			return
		}
		if err := a.Verify(); err != nil {
			return
		}
		mu.Lock()
		if completed {
			mu.Unlock()
			return
		}
		for _, c := range collected {
			if bytes.Equal(c.ReplicatorPub, a.ReplicatorPub) {
				mu.Unlock()
				return
			}
		}
		collected = append(collected, a)
		filled := len(collected) >= req.ReplicationFactor
		mu.Unlock()
		if filled {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	})
	defer p.subscriber.UnsubscribeAcceptances(req.RequestID)

	timer := time.NewTimer(p.timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	case <-done:
	}

	mu.Lock()
	completed = true
	snapshot := append([]PinAcceptance(nil), collected...)
	mu.Unlock()

	sort.Slice(snapshot, func(i, j int) bool {
		return snapshot[i].AcceptedAtUnixMs < snapshot[j].AcceptedAtUnixMs
	})
	n := req.ReplicationFactor
	if n > len(snapshot) {
		n = len(snapshot)
	}
	out := make([]ReplicatorID, 0, n)
	for _, a := range snapshot[:n] {
		out = append(out, a.ReplicatorID)
	}
	return out, nil
}

// PinPolicyConfig is the replicator policy.
type PinPolicyConfig struct {
	Enabled              bool
	MaxTotalPinnedBytes  int64
	MinFeeRatePerMBMonth float64
	MaxCommitmentWeeks   int
	AllowedRecordTypes   []string
	MinSellerReputation  float64
	PreferredChains      []string
	PreferredChainID     string
	MinPieceSizeBytes    int64
	MaxPieceSizeBytes    int64
}

// PinAcceptorConfig ties identity + policy + escrow client.
type PinAcceptorConfig struct {
	PeerID             string
	IdentityKey        ed25519.PrivateKey
	Policy             PinPolicyConfig
	CurrentPinnedBytes func() int64
	Now                func() time.Time
}

// PinAcceptor is the replicator's decision engine.
type PinAcceptor struct {
	cfg    PinAcceptorConfig
	escrow EscrowClient
	pub    PinPublisher
	topic  string
	now    func() time.Time
}

// NewPinAcceptor wires policy + escrow + publisher.
func NewPinAcceptor(cfg PinAcceptorConfig, escrow EscrowClient, pub PinPublisher) (*PinAcceptor, error) {
	if len(cfg.IdentityKey) != ed25519.PrivateKeySize {
		return nil, errors.New("onchain: acceptor needs valid Ed25519 private key")
	}
	if escrow == nil {
		return nil, errors.New("onchain: acceptor needs EscrowClient")
	}
	if pub == nil {
		return nil, errors.New("onchain: acceptor needs publisher")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &PinAcceptor{cfg: cfg, escrow: escrow, pub: pub, topic: TopicPinAccept, now: now}, nil
}

// PolicyRejectError carries a reason string for diagnostic surfacing.
type PolicyRejectError struct {
	Reason string
}

// Error implements error.
func (e *PolicyRejectError) Error() string { return "onchain: policy reject: " + e.Reason }

// EvaluatePolicy runs the gate logic with no side effects.
func (a *PinAcceptor) EvaluatePolicy(req PinRequest) error {
	p := a.cfg.Policy
	if !p.Enabled {
		return &PolicyRejectError{Reason: "acceptor disabled"}
	}
	if err := req.Verify(); err != nil {
		return &PolicyRejectError{Reason: "bad signature: " + err.Error()}
	}
	weeks := req.DurationSeconds / int64(7*24*3600)
	if p.MaxCommitmentWeeks > 0 && weeks > int64(p.MaxCommitmentWeeks) {
		return &PolicyRejectError{Reason: fmt.Sprintf("duration %dw > max %dw", weeks, p.MaxCommitmentWeeks)}
	}
	if p.MinSellerReputation > 0 && req.SellerReputation < p.MinSellerReputation {
		return &PolicyRejectError{Reason: fmt.Sprintf("seller reputation %.3f < min %.3f", req.SellerReputation, p.MinSellerReputation)}
	}
	if len(p.AllowedRecordTypes) > 0 {
		ok := false
		for _, rt := range p.AllowedRecordTypes {
			if rt == req.RecordType {
				ok = true
				break
			}
		}
		if !ok {
			return &PolicyRejectError{Reason: "record type not allowed: " + req.RecordType}
		}
	}
	if len(p.PreferredChains) > 0 {
		ok := false
		for _, pref := range p.PreferredChains {
			for _, acc := range req.AcceptedPaymentChains {
				if pref == acc {
					ok = true
					break
				}
			}
			if ok {
				break
			}
		}
		if !ok {
			return &PolicyRejectError{Reason: "no chain preference overlap"}
		}
	}
	if p.MaxTotalPinnedBytes > 0 && a.cfg.CurrentPinnedBytes != nil {
		if a.cfg.CurrentPinnedBytes() >= p.MaxTotalPinnedBytes {
			return &PolicyRejectError{Reason: "at pin capacity"}
		}
	}
	return nil
}

// HandlePinRequest evaluates policy, creates escrow + bond, and
// broadcasts an acceptance.
func (a *PinAcceptor) HandlePinRequest(ctx context.Context, req PinRequest) (string, error) {
	if err := a.EvaluatePolicy(req); err != nil {
		return "", err
	}
	pub := a.cfg.IdentityKey.Public().(ed25519.PublicKey)
	replicatorID := fmt.Sprintf("%x", pub)

	feePerRep, ok := new(big.Int).SetString(req.FeePerReplicator, 10)
	if !ok {
		return "", fmt.Errorf("onchain: bad FeePerReplicator: %s", req.FeePerReplicator)
	}
	chain := a.cfg.Policy.PreferredChainID
	if chain == "" && len(a.cfg.Policy.PreferredChains) > 0 {
		chain = a.cfg.Policy.PreferredChains[0]
	}
	params := CreatePinParams{
		ChainID:           chain,
		CID:               req.CID,
		Seller:            fmt.Sprintf("%x", req.SellerPubKey),
		Replicator:        replicatorID,
		Duration:          time.Duration(req.DurationSeconds) * time.Second,
		ReplicationFactor: req.ReplicationFactor,
		FeePerReplicator:  feePerRep,
		BondDeadline:      a.now().Add(DefaultBondDeadlineSkew),
	}
	addr, err := a.escrow.CreatePin(ctx, params)
	if err != nil {
		return "", fmt.Errorf("onchain: create escrow: %w", err)
	}
	if _, err := a.escrow.PostBond(ctx, addr); err != nil {
		return "", fmt.Errorf("onchain: post bond: %w", err)
	}
	ack := PinAcceptance{
		RequestID:        req.RequestID,
		CID:              req.CID,
		ReplicatorID:     replicatorID,
		EscrowChain:      chain,
		EscrowAddress:    addr,
		AcceptedAtUnixMs: a.now().UnixMilli(),
	}
	if err := ack.Sign(a.cfg.IdentityKey); err != nil {
		return addr, fmt.Errorf("onchain: sign acceptance: %w", err)
	}
	payload, err := json.Marshal(&ack)
	if err != nil {
		return addr, fmt.Errorf("onchain: marshal acceptance: %w", err)
	}
	if err := a.pub.Publish(TopicPinAccept, payload); err != nil {
		return addr, fmt.Errorf("onchain: publish acceptance: %w", err)
	}
	return addr, nil
}

// DecodePinRequest parses a PinRequest payload.
func DecodePinRequest(b []byte) (PinRequest, error) {
	var r PinRequest
	if err := json.Unmarshal(b, &r); err != nil {
		return PinRequest{}, fmt.Errorf("onchain: pin-request decode: %w", err)
	}
	return r, nil
}

// DecodePinAcceptance parses a PinAcceptance payload.
func DecodePinAcceptance(b []byte) (PinAcceptance, error) {
	var a PinAcceptance
	if err := json.Unmarshal(b, &a); err != nil {
		return PinAcceptance{}, fmt.Errorf("onchain: pin-acceptance decode: %w", err)
	}
	return a, nil
}
