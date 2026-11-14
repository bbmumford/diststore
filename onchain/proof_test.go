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
	"encoding/json"
	"errors"
	"testing"
	"time"

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

// routedTransport pipes an issuer's Ask straight to a ChallengeResponder's
// OnMessage in-process. intercept, when set, answers instead, so a test can
// tamper with or withhold the responder's reply.
type routedTransport struct {
	responder *ChallengeResponder
	intercept func(raw []byte) ([]byte, error)
}

func (r *routedTransport) Ask(ctx context.Context, peerID string, payload []byte) ([]byte, error) {
	if r.intercept != nil {
		return r.intercept(payload)
	}
	return r.responder.OnMessage(ctx, peerID, r.responder.Topic(), payload)
}

func mustKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
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
	issuer, err := NewChallengeIssuer(rt, ChallengeIssuerConfig{IssuerPeerID: "i"})
	if err != nil {
		t.Fatalf("issuer: %v", err)
	}
	res, err := issuer.Challenge(context.Background(), "r", cid)
	if err != nil {
		t.Fatalf("Challenge: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected OK, got %+v", res)
	}
	if res.Response == nil {
		t.Fatalf("expected response")
	}
	if res.Response.CID != cid {
		t.Errorf("cid mismatch")
	}
	if err := issuer.VerifyResponse(res.Response, piece); err != nil {
		t.Errorf("VerifyResponse: %v", err)
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
	if len(h1) != 32 {
		t.Fatalf("expected 32-byte hash, got %d", len(h1))
	}
}

func TestChallenge_WrongHashFailsSig(t *testing.T) {
	piece := []byte("the real piece")
	cid := cidOf(piece)
	key := mustKey(t)

	store := &fakePieces{pieces: map[string][]byte{cid: piece}}
	responder, _ := NewChallengeResponder(store, ChallengeResponderConfig{
		PeerID: "r", IdentityKey: key,
	})

	// The signature covers the hash the responder computed, so a reply whose
	// hash is replaced in flight fails verification at the issuer.
	rt := &routedTransport{
		responder: responder,
		intercept: func(raw []byte) ([]byte, error) {
			respRaw, err := responder.OnMessage(context.Background(), "r", responder.Topic(), raw)
			if err != nil || respRaw == nil {
				return respRaw, err
			}
			var resp ProofResponse
			_ = json.Unmarshal(respRaw, &resp)
			resp.Hash = []byte("deliberately-wrong-hash-deliberately-wrong-hash!")[:32]
			return json.Marshal(&resp)
		},
	}
	issuer, _ := NewChallengeIssuer(rt, ChallengeIssuerConfig{IssuerPeerID: "i"})
	res, err := issuer.Challenge(context.Background(), "r", cid)
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	if res.OK {
		t.Fatalf("expected NOT ok; reason=%s", res.Reason)
	}
	if res.Reason != "sig_fail" {
		t.Errorf("expected sig_fail, got %s", res.Reason)
	}
}

func TestChallenge_TimeoutRecordsMiss(t *testing.T) {
	cid := cidOf([]byte("no-such-piece"))
	key := mustKey(t)

	store := &fakePieces{pieces: map[string][]byte{}}
	responder, _ := NewChallengeResponder(store, ChallengeResponderConfig{
		PeerID: "r", IdentityKey: key,
	})

	rt := &routedTransport{
		responder: responder,
		intercept: func(raw []byte) ([]byte, error) {
			// A peer that never replies; the issuer's short ResponseTimeout
			// keeps the test fast.
			return nil, context.DeadlineExceeded
		},
	}
	issuer, _ := NewChallengeIssuer(rt, ChallengeIssuerConfig{
		IssuerPeerID:    "i",
		ResponseTimeout: 50 * time.Millisecond,
	})

	res, err := issuer.Challenge(context.Background(), "r", cid)
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	if res.OK {
		t.Errorf("expected miss")
	}
	if res.Reason != "timeout" {
		t.Errorf("expected timeout, got %s", res.Reason)
	}
}

func TestChallenge_CIDNotHeldReturnsMiss(t *testing.T) {
	// A responder without the CID answers OnMessage with (nil, nil), which
	// the issuer records as empty_response.
	key := mustKey(t)
	store := &fakePieces{pieces: map[string][]byte{}}
	responder, _ := NewChallengeResponder(store, ChallengeResponderConfig{
		PeerID: "r", IdentityKey: key,
	})
	rt := &routedTransport{responder: responder}

	issuer, _ := NewChallengeIssuer(rt, ChallengeIssuerConfig{IssuerPeerID: "i"})
	res, err := issuer.Challenge(context.Background(), "r", cidOf([]byte("nope")))
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	if res.OK {
		t.Errorf("expected miss")
	}
	if res.Reason != "empty_response" {
		t.Errorf("expected empty_response, got %s", res.Reason)
	}
}

func TestChallenge_WrongKeyRejected(t *testing.T) {
	piece := []byte("piece bytes")
	cid := cidOf(piece)
	responderKey := mustKey(t)

	store := &fakePieces{pieces: map[string][]byte{cid: piece}}
	responder, _ := NewChallengeResponder(store, ChallengeResponderConfig{
		PeerID: "r", IdentityKey: responderKey,
	})

	// The reply is signed under responderKey but claims another public key,
	// so verification fails.
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	rt := &routedTransport{
		responder: responder,
		intercept: func(raw []byte) ([]byte, error) {
			respRaw, err := responder.OnMessage(context.Background(), "r", responder.Topic(), raw)
			if err != nil || respRaw == nil {
				return respRaw, err
			}
			var resp ProofResponse
			_ = json.Unmarshal(respRaw, &resp)
			resp.PubKey = otherPub
			return json.Marshal(&resp)
		},
	}
	issuer, _ := NewChallengeIssuer(rt, ChallengeIssuerConfig{IssuerPeerID: "i"})
	res, _ := issuer.Challenge(context.Background(), "r", cid)
	if res.OK {
		t.Errorf("expected rejection on pubkey swap")
	}
	if res.Reason != "sig_fail" {
		t.Errorf("expected sig_fail, got %s", res.Reason)
	}
}

func TestVerifyResponse_LocalPiece(t *testing.T) {
	piece := []byte("verify-against-local-piece")
	key := mustKey(t)
	nonce, _ := NewNonce()
	cid := cidOf(piece)
	resp := BuildResponseFor(cid, piece, nonce, key)

	issuer, _ := NewChallengeIssuer(nil, ChallengeIssuerConfig{IssuerPeerID: "i"})
	if err := issuer.VerifyResponse(&resp, piece); err != nil {
		t.Errorf("VerifyResponse: %v", err)
	}

	if err := issuer.VerifyResponse(&resp, []byte("wrong")); err == nil {
		t.Errorf("expected hash mismatch")
	}
}

func TestComputeProofHash_DeterministicForAnASCIINonce(t *testing.T) {
	p := []byte("abc")
	n := []byte("nonce12345nonce12345nonce12345xx") // 32 bytes
	h1 := ComputeProofHash(p, n)
	h2 := ComputeProofHash(p, n)
	if !bytes.Equal(h1, h2) {
		t.Fatalf("expected deterministic hash")
	}
	if len(h1) != 32 {
		t.Fatalf("expected 32-byte hash, got %d", len(h1))
	}
}

func TestChallenge_RecentResultsRing(t *testing.T) {
	piece := []byte("ring-test")
	cid := cidOf(piece)
	key := mustKey(t)
	store := &fakePieces{pieces: map[string][]byte{cid: piece}}
	responder, _ := NewChallengeResponder(store, ChallengeResponderConfig{
		PeerID: "r", IdentityKey: key,
	})
	rt := &routedTransport{responder: responder}
	issuer, _ := NewChallengeIssuer(rt, ChallengeIssuerConfig{IssuerPeerID: "i"})

	for i := 0; i < 5; i++ {
		if _, err := issuer.Challenge(context.Background(), "r", cid); err != nil {
			t.Fatalf("challenge %d: %v", i, err)
		}
	}
	got := issuer.RecentResults()
	if len(got) != 5 {
		t.Fatalf("expected 5 results, got %d", len(got))
	}
	for i, r := range got {
		if !r.OK {
			t.Errorf("result %d expected OK", i)
		}
	}
}

// A peer with a valid Ed25519 identity can sign a hash over bytes it never
// held: the signature verifies and the CID and nonce bind. An issuer built
// with NewChallengeIssuerWithPieceSource hashes its own copy of the piece and
// refuses the reply as wrong_hash.
func TestChallenge_IssuerRejectsSignedButFabricatedHash(t *testing.T) {
	realPiece := []byte("the real piece that the issuer actually holds")
	cid := cidOf(realPiece)
	responderKey := mustKey(t)

	// The responder holds different bytes under the CID and signs a hash
	// over them.
	fakePiece := []byte("totally different content the attacker made up!")

	responderStore := &fakePieces{pieces: map[string][]byte{cid: fakePiece}}
	responder, _ := NewChallengeResponder(responderStore, ChallengeResponderConfig{
		PeerID: "r", IdentityKey: responderKey,
	})
	rt := &routedTransport{responder: responder}

	issuerStore := &fakePieces{pieces: map[string][]byte{cid: realPiece}}
	issuer, err := NewChallengeIssuerWithPieceSource(rt, issuerStore, ChallengeIssuerConfig{IssuerPeerID: "i"})
	if err != nil {
		t.Fatalf("issuer: %v", err)
	}

	res, err := issuer.Challenge(context.Background(), "r", cid)
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	if res.OK {
		t.Fatalf("issuer accepted a fabricated-hash response; wanted rejection")
	}
	if res.Reason != "wrong_hash" {
		t.Fatalf("expected reason=wrong_hash, got %s", res.Reason)
	}
}

// An issuer that does not hold the piece skips the cross-check and accepts a
// correctly signed reply: it cannot penalise a responder for bytes it cannot
// verify itself.
func TestChallenge_IssuerSkipsCrossCheckWhenPieceMissing(t *testing.T) {
	piece := []byte("a piece the responder holds and the issuer does not")
	cid := cidOf(piece)
	responderKey := mustKey(t)

	responderStore := &fakePieces{pieces: map[string][]byte{cid: piece}}
	responder, _ := NewChallengeResponder(responderStore, ChallengeResponderConfig{
		PeerID: "r", IdentityKey: responderKey,
	})
	rt := &routedTransport{responder: responder}

	issuerStore := &fakePieces{pieces: map[string][]byte{}}
	issuer, _ := NewChallengeIssuerWithPieceSource(rt, issuerStore, ChallengeIssuerConfig{IssuerPeerID: "i"})

	res, err := issuer.Challenge(context.Background(), "r", cid)
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected OK (fallback to signature-only), got reason=%s", res.Reason)
	}
}

func TestResponder_WrongTopicRejected(t *testing.T) {
	key := mustKey(t)
	responder, _ := NewChallengeResponder(&fakePieces{pieces: map[string][]byte{}}, ChallengeResponderConfig{
		PeerID: "r", IdentityKey: key,
	})
	_, err := responder.OnMessage(context.Background(), "x", "plutus.blob.find", []byte("{}"))
	if err == nil {
		t.Fatalf("expected error on wrong topic")
	}
}
