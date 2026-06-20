/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package crypto

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
)

func mustDEK(t *testing.T) []byte {
	t.Helper()
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		t.Fatalf("rand dek: %v", err)
	}
	return dek
}

func TestRecipientWrapRoundtrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dek := mustDEK(t)
	w, err := WrapDataKeyForRecipient(dek, pub)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if w.Alg != WrapAlgNaClBox {
		t.Fatalf("alg = %q, want %q", w.Alg, WrapAlgNaClBox)
	}
	got, err := UnwrapToRecipient(w, pub, priv, nil)
	if err != nil {
		t.Fatalf("unwrap: %v", err)
	}
	if !bytes.Equal(got, dek) {
		t.Fatal("roundtrip DEK mismatch")
	}
}

func TestRecipientWrapWrongKeyFails(t *testing.T) {
	pub1, _, _ := ed25519.GenerateKey(rand.Reader)
	pub2, priv2, _ := ed25519.GenerateKey(rand.Reader)
	w, err := WrapDataKeyForRecipient(mustDEK(t), pub1)
	if err != nil {
		t.Fatal(err)
	}
	// A different recipient keypair must not open a box sealed to pub1.
	if _, err := UnwrapToRecipient(w, pub2, priv2, nil); !errors.Is(err, ErrRecipientUnwrap) {
		t.Fatalf("wrong-key unwrap err = %v, want ErrRecipientUnwrap", err)
	}
}

func TestRecipientUnwrapEntitlementGate(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	denied := errors.New("not entitled")

	// A valid wrap + failing gate must return the gate error verbatim.
	w, _ := WrapDataKeyForRecipient(mustDEK(t), pub)
	if _, err := UnwrapToRecipient(w, pub, priv, func() error { return denied }); !errors.Is(err, denied) {
		t.Fatalf("gated unwrap err = %v, want denied", err)
	}
	// A GARBAGE wrap + failing gate must still return the gate error, proving
	// the gate short-circuits before any crypto is attempted (fail-closed).
	bad := RecipientWrappedDataKey{Alg: WrapAlgNaClBox, Wrapped: []byte("garbage")}
	if _, err := UnwrapToRecipient(bad, pub, priv, func() error { return denied }); !errors.Is(err, denied) {
		t.Fatalf("garbage+gate err = %v, want denied (no crypto before gate)", err)
	}
}

func TestRecipientWrapP256Roundtrip(t *testing.T) {
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dek := mustDEK(t)
	w, err := WrapDataKeyForRecipientP256(dek, priv.PublicKey())
	if err != nil {
		t.Fatalf("p256 wrap: %v", err)
	}
	if w.Alg != WrapAlgP256AESKW {
		t.Fatalf("alg = %q, want %q", w.Alg, WrapAlgP256AESKW)
	}
	got, err := UnwrapToRecipientP256(w, priv, nil)
	if err != nil {
		t.Fatalf("p256 unwrap: %v", err)
	}
	if !bytes.Equal(got, dek) {
		t.Fatal("p256 roundtrip DEK mismatch")
	}
}

func TestP256WrongKeyFails(t *testing.T) {
	priv1, _ := ecdh.P256().GenerateKey(rand.Reader)
	priv2, _ := ecdh.P256().GenerateKey(rand.Reader)
	w, err := WrapDataKeyForRecipientP256(mustDEK(t), priv1.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnwrapToRecipientP256(w, priv2, nil); !errors.Is(err, ErrRecipientUnwrap) {
		t.Fatalf("p256 wrong-key err = %v, want ErrRecipientUnwrap", err)
	}
}

func TestWrappingRecordSignVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	w, _ := WrapDataKeyForRecipient(mustDEK(t), pub)
	rec := &WrappingRecord{BlobCID: "bafy-cid-123", Recipient: w}
	if err := SignWrapping(priv, rec); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if len(rec.WrapperPub) != ed25519.PublicKeySize || len(rec.Signature) == 0 {
		t.Fatal("sign must embed wrapper pub + signature")
	}
	if err := VerifyWrapping(rec); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestWrappingRecordTamperRejected(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	w, _ := WrapDataKeyForRecipient(mustDEK(t), pub)

	// Tamper the anchor CID.
	rec := &WrappingRecord{BlobCID: "cid-A", Recipient: w}
	_ = SignWrapping(priv, rec)
	rec.BlobCID = "cid-B"
	if err := VerifyWrapping(rec); err == nil {
		t.Fatal("verify must reject a tampered BlobCID")
	}

	// Tamper the sealed bytes.
	rec2 := &WrappingRecord{BlobCID: "cid-A", Recipient: w}
	_ = SignWrapping(priv, rec2)
	if len(rec2.Recipient.Wrapped) > 0 {
		rec2.Recipient.Wrapped[0] ^= 0xFF
	}
	if err := VerifyWrapping(rec2); err == nil {
		t.Fatal("verify must reject tampered sealed bytes")
	}
}
