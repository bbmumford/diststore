/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package onchain

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"

	"golang.org/x/crypto/blake2b"
)

type fakePieces struct {
	pieces map[string][]byte
}

func (f *fakePieces) Get(_ context.Context, cid string) ([]byte, error) {
	p, ok := f.pieces[cid]
	if !ok {
		return nil, errors.New("not found")
	}
	return p, nil
}

func (f *fakePieces) Has(_ context.Context, cid string) (bool, error) {
	_, ok := f.pieces[cid]
	return ok, nil
}

type routedTransport struct {
	responder *ChallengeResponder
}

func (r *routedTransport) Ask(ctx context.Context, peerID string, payload []byte) ([]byte, error) {
	return r.responder.OnMessage(ctx, peerID, r.responder.Topic(), payload)
}

func mustKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	return priv
}

func cidOf(b []byte) string {
	sum := blake2b.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestChallenge_SuccessRoundtrip(t *testing.T) {
	piece := []byte("piece bytes")
	cid := cidOf(piece)
	key := mustKey(t)
	store := &fakePieces{pieces: map[string][]byte{cid: piece}}
	responder, err := NewChallengeResponder(store, ChallengeResponderConfig{
		PeerID: "r", IdentityKey: key,
	})
	if err != nil {
		t.Fatalf("responder: %v", err)
	}
	rt := &routedTransport{responder: responder}
	issuer, _ := NewChallengeIssuer(rt, ChallengeIssuerConfig{IssuerPeerID: "i"})
	res, err := issuer.Challenge(context.Background(), "r", cid)
	if err != nil {
		t.Fatalf("Challenge: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected OK, got %+v", res)
	}
}

func TestComputeProofHash_Deterministic(t *testing.T) {
	p := []byte("abc")
	n := make([]byte, 32)
	for i := range n {
		n[i] = byte(i)
	}
	h1 := ComputeProofHash(p, n)
	h2 := ComputeProofHash(p, n)
	if !bytes.Equal(h1, h2) {
		t.Fatal("non-deterministic")
	}
}
