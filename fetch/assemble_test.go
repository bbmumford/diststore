/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package fetch

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/bbmumford/diststore/chunker"
)

// mapGetter serves pieces (and a manifest) from in-memory maps — the untrusted transport stand-in.
type mapGetter struct {
	pieces   map[string][]byte
	manifest *chunker.Manifest
	err      error
}

func (g *mapGetter) GetPiece(_ context.Context, _, cid string) ([]byte, error) {
	if g.err != nil {
		return nil, g.err
	}
	b, ok := g.pieces[cid]
	if !ok {
		return nil, errors.New("no such piece")
	}
	return b, nil
}

func (g *mapGetter) GetManifest(_ context.Context, _, _ string) (*chunker.Manifest, error) {
	return g.manifest, nil
}

// splitToGetter runs the real chunker over data and loads a getter with the resulting manifest+pieces.
func splitToGetter(t *testing.T, data []byte) *mapGetter {
	t.Helper()
	m, pieces, err := chunker.Split(data, chunker.DefaultMaxFileSizeBytes)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	g := &mapGetter{pieces: map[string][]byte{}, manifest: m}
	for i, ref := range m.Pieces {
		g.pieces[ref.CID] = pieces[i]
	}
	return g
}

func TestVerifyingSource_RoundTrip(t *testing.T) {
	data := make([]byte, chunker.PieceSize*2+123) // spans multiple pieces + a partial
	for i := range data {
		data[i] = byte(i * 7)
	}
	g := splitToGetter(t, data)
	src := VerifyingSource{Manifests: g, Pieces: g}

	rc, size, err := src.Open(context.Background(), "tenant-1", g.manifest.RootCID)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer rc.Close()
	if size != int64(len(data)) {
		t.Fatalf("size %d, want %d", size, len(data))
	}
	got, _ := io.ReadAll(rc)
	if len(got) != len(data) {
		t.Fatalf("read %d bytes, want %d", len(got), len(data))
	}
	for i := range got {
		if got[i] != data[i] {
			t.Fatalf("byte %d differs after round trip", i)
		}
	}
}

func TestAssembleBlob_RejectsWrongRequestedBlob(t *testing.T) {
	g := splitToGetter(t, []byte("hello world, this is a blob"))
	// Ask for a blobID that is not the manifest's root → refused before any fetch.
	if _, err := AssembleBlob(context.Background(), "t", "deadbeefcafe", g.manifest, g); err == nil {
		t.Fatal("a manifest whose root != requested blobID must be rejected")
	}
}

func TestAssembleBlob_RejectsSubstitutedManifest(t *testing.T) {
	g := splitToGetter(t, []byte("the authentic content of this blob spanning some bytes"))
	// Forge a manifest that keeps the authorized RootCID but swaps in a different piece list. The
	// pieces themselves are self-consistent (valid CIDs) but do not Merkle-derive the claimed root.
	other := splitToGetter(t, []byte("completely different bytes with different piece cids entirely"))
	forged := &chunker.Manifest{
		RootCID:    g.manifest.RootCID, // claim the authorized address
		Pieces:     other.manifest.Pieces,
		TotalBytes: other.manifest.TotalBytes,
		PieceSize:  other.manifest.PieceSize,
	}
	// Serve the forged pieces so a fetch would succeed — the Merkle check must reject first.
	merged := &mapGetter{pieces: map[string][]byte{}, manifest: forged}
	for k, v := range other.pieces {
		merged.pieces[k] = v
	}
	if _, err := AssembleBlob(context.Background(), "t", g.manifest.RootCID, forged, merged); err == nil {
		t.Fatal("a manifest that keeps the root but swaps its piece list must be rejected by the Merkle check")
	}
}

func TestAssembleBlob_RejectsCorruptPiece(t *testing.T) {
	g := splitToGetter(t, []byte("content whose first piece will be tampered in transit"))
	// Corrupt the first piece's bytes under its real CID key.
	firstCID := g.manifest.Pieces[0].CID
	corrupt := append([]byte(nil), g.pieces[firstCID]...)
	corrupt[0] ^= 0xFF
	g.pieces[firstCID] = corrupt

	if _, err := AssembleBlob(context.Background(), "t", g.manifest.RootCID, g.manifest, g); err == nil {
		t.Fatal("a piece whose bytes do not hash to its CID must be rejected")
	}
}

func TestAssembleBlob_FetchErrorFailsClosed(t *testing.T) {
	g := splitToGetter(t, []byte("some blob"))
	g.err = errors.New("transport down")
	if _, err := AssembleBlob(context.Background(), "t", g.manifest.RootCID, g.manifest, g); err == nil {
		t.Fatal("a transport error must fail closed, not yield partial bytes")
	}
}
