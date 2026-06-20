/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

// Package advertise periodically gossips the CIDs a local cache holds
// over the transport's BroadcastOnly topic.
//
// Two envelope forms are accepted on read: the Ed25519-signed
// envelope produced by signed.go (the form to prefer for new
// deployments) and an unsigned plaintext envelope. Producers select
// the form via Config.SignAdvertisements.
package advertise

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bbmumford/diststore/transport"
)

// TopicBlobHave is the BroadcastOnly topic on which nodes advertise
// the CIDs they hold.
const TopicBlobHave = "plutus.blob.have"

// DefaultInterval is the cadence used when Config.Interval is zero.
const DefaultInterval = 60 * time.Second

// HaveAdvertisement is the gossip payload.
//
// When SignedAdvertisements is on at the diststore.Config level, the
// extra Signature + PubKey fields (see signed.go) are populated.
type HaveAdvertisement struct {
	PeerID          string   `json:"peerId"`
	EmittedAtUnixMs int64    `json:"emittedAtMs"`
	CIDs            []string `json:"cids"`
	// Optional signature fields (see signed.go).
	PubKey    []byte `json:"pubKey,omitempty"`
	Signature []byte `json:"sig,omitempty"`
}

// TopicLister enumerates the CIDs this node currently holds.
type TopicLister interface {
	List(ctx context.Context) ([]string, error)
}

// Config tunes the advertiser cadence + identity.
type Config struct {
	PeerID   string
	Interval time.Duration
	Topic    string
	Now      func() time.Time

	// Signer, if non-nil, signs each emitted envelope. See signed.go.
	Signer Signer
}

// Advertiser periodically gossips the CIDs held by a local store.
type Advertiser struct {
	store    TopicLister
	pub      transport.PublishTopic
	cfg      Config
	topic    string
	interval time.Duration
	now      func() time.Time

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// New wires a Store-like topic lister + publisher into an Advertiser.
// Start is NOT called; do that explicitly.
func New(store TopicLister, pub transport.PublishTopic, cfg Config) (*Advertiser, error) {
	if store == nil {
		return nil, errors.New("advertise: needs non-nil store")
	}
	if pub == nil {
		return nil, errors.New("advertise: needs non-nil publisher")
	}
	if cfg.PeerID == "" {
		return nil, errors.New("advertise: needs non-empty PeerID")
	}
	topic := cfg.Topic
	if topic == "" {
		topic = TopicBlobHave
	}
	iv := cfg.Interval
	if iv <= 0 {
		iv = DefaultInterval
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Advertiser{
		store:    store,
		pub:      pub,
		cfg:      cfg,
		topic:    topic,
		interval: iv,
		now:      now,
	}, nil
}

// Start begins the advertisement loop.
func (a *Advertiser) Start(ctx context.Context) error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return errors.New("advertise: already running")
	}
	a.running = true
	a.done = make(chan struct{})
	loopCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	a.mu.Unlock()
	go a.loop(loopCtx)
	return nil
}

// Stop halts the loop and waits for the goroutine to exit.
func (a *Advertiser) Stop() {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		return
	}
	a.running = false
	cancel := a.cancel
	done := a.done
	a.cancel = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (a *Advertiser) loop(ctx context.Context) {
	defer func() {
		a.mu.Lock()
		done := a.done
		a.done = nil
		a.mu.Unlock()
		if done != nil {
			close(done)
		}
	}()
	_ = a.EmitOnce(ctx)
	t := time.NewTicker(a.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = a.EmitOnce(ctx)
		}
	}
}

// EmitOnce builds + publishes a single HaveAdvertisement synchronously.
func (a *Advertiser) EmitOnce(ctx context.Context) error {
	cids, err := a.store.List(ctx)
	if err != nil {
		return fmt.Errorf("advertise: list: %w", err)
	}
	ad := HaveAdvertisement{
		PeerID:          a.cfg.PeerID,
		EmittedAtUnixMs: a.now().UnixMilli(),
		CIDs:            cids,
	}
	if a.cfg.Signer != nil {
		if err := a.cfg.Signer.Sign(&ad); err != nil {
			return fmt.Errorf("advertise: sign: %w", err)
		}
	}
	payload, err := json.Marshal(&ad)
	if err != nil {
		return fmt.Errorf("advertise: marshal: %w", err)
	}
	if err := a.pub.Publish(a.topic, payload); err != nil {
		return fmt.Errorf("advertise: publish: %w", err)
	}
	return nil
}

// Topic returns the effective topic name.
func (a *Advertiser) Topic() string { return a.topic }

// Decode parses bytes emitted by EmitOnce.
func Decode(b []byte) (*HaveAdvertisement, error) {
	if len(b) == 0 {
		return nil, errors.New("advertise: empty payload")
	}
	var ad HaveAdvertisement
	if err := json.Unmarshal(b, &ad); err != nil {
		return nil, fmt.Errorf("advertise: decode: %w", err)
	}
	return &ad, nil
}
