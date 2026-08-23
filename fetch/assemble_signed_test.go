/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package fetch

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"testing"

	"github.com/bbmumford/diststore/chunker"
	"github.com/bbmumford/diststore/index"
)

// signedGetter serves a manifest + its signature (SignedManifestGetter) plus its pieces (PieceGetter).
type signedGetter struct {
	*mapGetter
	sig []byte
}

func (g *signedGetter) GetSignedManifest(_ context.Context, _, _ string) (*chunker.Manifest, []byte, error) {
	return g.manifest, g.sig, nil
}

func signedFixture(t *testing.T, data []byte, priv ed25519.PrivateKey) *signedGetter {
	t.Helper()
	g := splitToGetter(t, data)
	sig, err := index.SignManifest(g.manifest, priv)
	if err != nil {
		t.Fatalf("sign manifest: %v", err)
	}
	return &signedGetter{mapGetter: g, sig: sig}
}

func TestSignedVerifyingSource_RoundTrip(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	data := make([]byte, chunker.PieceSize+777)
	for i := range data {
		data[i] = byte(i * 3)
	}
	g := signedFixture(t, data, priv)
	src := SignedVerifyingSource{Manifests: g, Pieces: g, PubKey: pub}

	rc, size, err := src.Open(context.Background(), "tenant-1", g.manifest.RootCID)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if int64(len(got)) != size || len(got) != len(data) {
		t.Fatalf("size mismatch: read %d size %d want %d", len(got), size, len(data))
	}
	for i := range got {
		if got[i] != data[i] {
			t.Fatalf("byte %d differs", i)
		}
	}
}

func TestAssembleSignedBlob_RejectsBadSignatureAndKey(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	g := signedFixture(t, []byte("signed blob content spanning bytes"), priv)

	// Tampered signature → refused.
	badSig := append([]byte(nil), g.sig...)
	badSig[0] ^= 0xFF
	if _, err := AssembleSignedBlob(context.Background(), "t", g.manifest.RootCID, g.manifest, badSig, pub, g); err == nil {
		t.Fatal("a tampered signature must be rejected")
	}

	// Wrong verifying key → refused, even with the genuine signature.
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := AssembleSignedBlob(context.Background(), "t", g.manifest.RootCID, g.manifest, g.sig, otherPub, g); err == nil {
		t.Fatal("a signature from a different key must be rejected")
	}

	// The genuine signature + key assembles.
	if _, err := AssembleSignedBlob(context.Background(), "t", g.manifest.RootCID, g.manifest, g.sig, pub, g); err != nil {
		t.Fatalf("the valid signed manifest must assemble: %v", err)
	}
}

func TestAssembleSignedBlob_RejectsSubstitutedManifest(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	authentic := signedFixture(t, []byte("the authentic content here"), priv)
	// A forged manifest that keeps the authorized root but swaps its piece list. Its OLD signature
	// no longer matches the tampered body, so index.ValidateManifest rejects it before any fetch.
	other := splitToGetter(t, []byte("entirely different bytes with other piece cids"))
	forged := &chunker.Manifest{
		RootCID:    authentic.manifest.RootCID,
		Pieces:     other.manifest.Pieces,
		TotalBytes: other.manifest.TotalBytes,
		PieceSize:  other.manifest.PieceSize,
	}
	if _, err := AssembleSignedBlob(context.Background(), "t", authentic.manifest.RootCID, forged, authentic.sig, pub, authentic); err == nil {
		t.Fatal("a manifest whose body was swapped under a stale signature must be rejected")
	}
}
