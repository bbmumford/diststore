/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package advertise

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakePublisher struct {
	mu     sync.Mutex
	topic  string
	calls  [][]byte
	fail   error
	notify chan struct{}
}

func newFakePublisher() *fakePublisher { return &fakePublisher{notify: make(chan struct{}, 16)} }

func (f *fakePublisher) Publish(topic string, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.topic = topic
	cp := make([]byte, len(payload))
	copy(cp, payload)
	f.calls = append(f.calls, cp)
	err := f.fail
	select {
	case f.notify <- struct{}{}:
	default:
	}
	return err
}

func (f *fakePublisher) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakePublisher) Calls() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]byte, len(f.calls))
	copy(out, f.calls)
	return out
}

type staticLister struct{ cids []string }

func (s staticLister) List(_ context.Context) ([]string, error) { return s.cids, nil }

func TestEmitOncePublishes(t *testing.T) {
	pub := newFakePublisher()
	lister := staticLister{cids: []string{"aaa", "bbb"}}
	ad, err := New(lister, pub, Config{PeerID: "node-1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ad.EmitOnce(context.Background()); err != nil {
		t.Fatalf("EmitOnce: %v", err)
	}
	if pub.topic != TopicBlobHave {
		t.Errorf("topic = %q want %q", pub.topic, TopicBlobHave)
	}
	if pub.Count() != 1 {
		t.Fatalf("publish count = %d want 1", pub.Count())
	}
	got, err := Decode(pub.Calls()[0])
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.PeerID != "node-1" || len(got.CIDs) != 2 {
		t.Errorf("decoded = %+v", got)
	}
}

func TestSignedAdvertisement(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	pub := newFakePublisher()
	lister := staticLister{cids: []string{"c1"}}
	ad, _ := New(lister, pub, Config{PeerID: "p", Signer: Ed25519Signer{Priv: priv}})
	if err := ad.EmitOnce(context.Background()); err != nil {
		t.Fatalf("EmitOnce: %v", err)
	}
	got, err := Decode(pub.Calls()[0])
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if err := Verify(got); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// Tamper.
	got.CIDs = append(got.CIDs, "evil")
	if err := Verify(got); err == nil {
		t.Fatal("expected Verify to fail on tampered advertisement")
	}
}

func TestNewValidation(t *testing.T) {
	cases := []struct {
		name   string
		lister TopicLister
		pub    *fakePublisher
		cfg    Config
	}{
		{"nil store", nil, newFakePublisher(), Config{PeerID: "a"}},
		{"nil engine", staticLister{}, nil, Config{PeerID: "a"}},
		{"empty peer", staticLister{}, newFakePublisher(), Config{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p interface{ Publish(string, []byte) error }
			if tc.pub != nil {
				p = tc.pub
			}
			if _, err := New(tc.lister, p, tc.cfg); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestStartTickStop(t *testing.T) {
	pub := newFakePublisher()
	lister := staticLister{cids: []string{"x"}}
	ad, _ := New(lister, pub, Config{PeerID: "n", Interval: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ad.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	got := 0
	deadline := time.After(2 * time.Second)
	for got < 2 {
		select {
		case <-pub.notify:
			got++
		case <-deadline:
			t.Fatalf("only %d emissions", got)
		}
	}
	ad.Stop()
}

func TestPublishErrorPropagates(t *testing.T) {
	pub := newFakePublisher()
	pub.fail = errors.New("boom")
	ad, _ := New(staticLister{cids: []string{"x"}}, pub, Config{PeerID: "n"})
	if err := ad.EmitOnce(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}
