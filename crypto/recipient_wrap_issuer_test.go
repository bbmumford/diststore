/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package crypto

import (
	"crypto/ed25519"
	"errors"
	"testing"
)

// signedWrapping returns a WrappingRecord for a fresh wrap, signed by signer.
func signedWrapping(t *testing.T, signer ed25519.PrivateKey) *WrappingRecord {
	t.Helper()
	k := fixedVectorKeys(t)
	w, err := WrapDataKeyForRecipient(k.dek, k.edPub)
	if err != nil {
		t.Fatal(err)
	}
	rec := &WrappingRecord{BlobCID: "cid", Recipient: w}
	if err := SignWrapping(signer, rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

func issuerKey(t *testing.T, label string) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	priv := ed25519.NewKeyFromSeed(labelBytes("diststore crypto issuer test: " + label))
	return priv.Public().(ed25519.PublicKey), priv
}

// A record any key holder signed with its own key is self-consistent, so
// VerifyWrapping accepts it; VerifyWrappingFrom refuses it unless the caller
// trusts that key.
func TestVerifyWrappingFromRefusesASelfSignedStranger(t *testing.T) {
	trustedPub, _ := issuerKey(t, "trusted")
	_, strangerPriv := issuerKey(t, "stranger")
	rec := signedWrapping(t, strangerPriv)
	if err := VerifyWrapping(rec); err != nil {
		t.Fatalf("a self-consistent record verifies: %v", err)
	}
	if err := VerifyWrappingFrom(rec, TrustIssuers(trustedPub)); !errors.Is(err, ErrUntrustedIssuer) {
		t.Fatalf("a record signed by an untrusted key: got %v, want ErrUntrustedIssuer", err)
	}
	if err := VerifyWrappingFrom(rec, TrustIssuers()); !errors.Is(err, ErrUntrustedIssuer) {
		t.Fatalf("an empty trust set admits no issuer: got %v", err)
	}
}

func TestVerifyWrappingFromAdmitsATrustedIssuer(t *testing.T) {
	aPub, aPriv := issuerKey(t, "a")
	bPub, bPriv := issuerKey(t, "b")
	gate := TrustIssuers(aPub, bPub)
	for _, priv := range []ed25519.PrivateKey{aPriv, bPriv} {
		if err := VerifyWrappingFrom(signedWrapping(t, priv), gate); err != nil {
			t.Fatalf("a record signed by a trusted key: %v", err)
		}
	}
}

// The trusted key embedded beside a stranger's signature does not pass: the
// signature is checked against the embedded key before the gate is asked.
func TestVerifyWrappingFromChecksTheSignatureBeforeTheGate(t *testing.T) {
	trustedPub, _ := issuerKey(t, "trusted")
	_, strangerPriv := issuerKey(t, "stranger")
	rec := signedWrapping(t, strangerPriv)
	rec.WrapperPub = append([]byte(nil), trustedPub...)
	asked := false
	err := VerifyWrappingFrom(rec, func(ed25519.PublicKey) error { asked = true; return nil })
	if err == nil {
		t.Fatal("a stranger's signature under a trusted key must be refused")
	}
	if asked {
		t.Fatal("the gate must not be asked about a record whose signature fails")
	}
}

func TestVerifyWrappingFromFailsClosedWithoutAGate(t *testing.T) {
	_, priv := issuerKey(t, "a")
	if err := VerifyWrappingFrom(signedWrapping(t, priv), nil); !errors.Is(err, ErrNoIssuerGate) {
		t.Fatalf("nil gate: got %v, want ErrNoIssuerGate", err)
	}
	if err := VerifyWrappingFrom(nil, TrustIssuers()); err == nil {
		t.Fatal("a nil record must be refused")
	}
}

// The gate's own error is returned verbatim, and the key it receives is a
// copy: a gate that writes to it leaves the record unchanged.
func TestVerifyWrappingFromReturnsTheGateErrorAndPassesACopy(t *testing.T) {
	aPub, aPriv := issuerKey(t, "a")
	rec := signedWrapping(t, aPriv)
	denied := errors.New("directory: issuer revoked")
	err := VerifyWrappingFrom(rec, func(issuer ed25519.PublicKey) error {
		if !issuer.Equal(aPub) {
			t.Fatalf("gate received %x, want the record's WrapperPub", issuer)
		}
		issuer[0] ^= 0xff
		return denied
	})
	if !errors.Is(err, denied) {
		t.Fatalf("got %v, want the gate's error", err)
	}
	if !ed25519.PublicKey(rec.WrapperPub).Equal(aPub) {
		t.Fatal("the gate's write reached the record's WrapperPub")
	}
}

// TrustIssuers copies its keys, so a caller reusing its slice afterwards
// changes nothing the gate admits.
func TestTrustIssuersCopiesItsKeys(t *testing.T) {
	aPub, aPriv := issuerKey(t, "a")
	key := append(ed25519.PublicKey(nil), aPub...)
	gate := TrustIssuers(key)
	key[0] ^= 0xff
	if err := VerifyWrappingFrom(signedWrapping(t, aPriv), gate); err != nil {
		t.Fatalf("the trusted key changed under the gate: %v", err)
	}
}
