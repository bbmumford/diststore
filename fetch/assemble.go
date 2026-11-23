/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package fetch

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"

	"github.com/bbmumford/diststore/chunker"
	"github.com/bbmumford/diststore/index"
)

// PieceGetter fetches one piece's ciphertext-or-plaintext bytes by its CID for a partition. It is the
// transport seam — a mesh Fetcher in production, a fake in tests — and is NOT trusted: AssembleBlob
// re-hashes every piece it returns.
type PieceGetter interface {
	GetPiece(ctx context.Context, partition, pieceCID string) ([]byte, error)
}

// ManifestGetter resolves a blob's manifest (its ordered piece list + root) by the blob's content
// address for a partition. Like PieceGetter it is untrusted; AssembleBlob re-derives the root from the
// pieces it lists.
type ManifestGetter interface {
	GetManifest(ctx context.Context, partition, blobID string) (*chunker.Manifest, error)
}

// AssembleBlob fetches and CID-verifies a whole blob from its manifest and returns the ordered
// plaintext bytes. It enforces the full content-address chain a storage capability authorizes, so a
// corrupt, truncated, or substituted blob can never reach the holder as if authentic:
//
//  1. the manifest's RootCID must equal the requested blobID — the capability bound this exact
//     address, so a manifest for any other blob is refused before a byte is fetched;
//  2. the Merkle root recomputed over the manifest's piece CIDs must equal that RootCID — a manifest
//     that lists a different piece set cannot reproduce the claimed root, so a swapped piece list is
//     caught here rather than serving other bytes under the authorized name;
//  3. every fetched piece must hash to its listed CID and match its declared size (chunker.Reassemble
//     re-checks both), with a defense-in-depth VerifyChunk before reassembly so a getter bug surfaces
//     as an integrity error rather than a size error.
//
// Any failure is a hard error and yields no bytes. The whole blob is held in memory during assembly
// (bounded by the chunker's file-size cap); streaming piece-by-piece with offset reads is a later
// optimization the get-side serve path leaves gated.
func AssembleBlob(ctx context.Context, partition, blobID string, m *chunker.Manifest, g PieceGetter) ([]byte, error) {
	if m == nil {
		return nil, errors.New("fetch: nil manifest")
	}
	if g == nil {
		return nil, errors.New("fetch: nil piece getter")
	}
	if m.RootCID != blobID {
		return nil, fmt.Errorf("fetch: manifest root %q does not match requested blob %q", m.RootCID, blobID)
	}
	cids := make([]string, len(m.Pieces))
	for i := range m.Pieces {
		cids[i] = m.Pieces[i].CID
	}
	if chunker.ComputeMerkleRoot(cids) != m.RootCID {
		return nil, errors.New("fetch: manifest merkle root does not derive from its piece CIDs")
	}
	pieces := make([][]byte, len(m.Pieces))
	for i, ref := range m.Pieces {
		b, err := g.GetPiece(ctx, partition, ref.CID)
		if err != nil {
			return nil, fmt.Errorf("fetch: piece %d (%s): %w", i, ref.CID, err)
		}
		if err := VerifyChunk(ref.CID, b); err != nil {
			return nil, fmt.Errorf("fetch: piece %d (%s): %w", i, ref.CID, err)
		}
		pieces[i] = b
	}
	// Reassemble re-verifies each piece CID + size + the total length against the manifest.
	return chunker.Reassemble(m, pieces)
}

// SignedManifestGetter resolves a blob's manifest and its detached Ed25519 signature. Like the
// unsigned getter it is untrusted: AssembleSignedBlob verifies the signature before the manifest is
// used at all.
type SignedManifestGetter interface {
	GetSignedManifest(ctx context.Context, partition, blobID string) (*chunker.Manifest, []byte, error)
}

// AssembleSignedBlob is AssembleBlob with the strongest manifest check first: the manifest's Ed25519
// signature is verified against the partition's storage key via index.ValidateManifest (which also
// recomputes the Merkle root and sanity-checks the piece shape). Only then is the root bound to the
// requested blobID and the pieces fetched + CID-verified. A forged or tampered manifest is rejected
// on the signature, so a substituted piece list cannot even be considered — this is the validation a
// content-served blob must pass, stronger than the structural-only AssembleBlob.
func AssembleSignedBlob(ctx context.Context, partition, blobID string, m *chunker.Manifest, signature []byte, pubkey ed25519.PublicKey, g PieceGetter) ([]byte, error) {
	if g == nil {
		return nil, errors.New("fetch: nil piece getter")
	}
	if err := index.ValidateManifest(m, signature, pubkey); err != nil {
		return nil, fmt.Errorf("fetch: manifest failed signature/root validation: %w", err)
	}
	if m.RootCID != blobID {
		return nil, fmt.Errorf("fetch: manifest root %q does not match requested blob %q", m.RootCID, blobID)
	}
	pieces := make([][]byte, len(m.Pieces))
	for i, ref := range m.Pieces {
		b, err := g.GetPiece(ctx, partition, ref.CID)
		if err != nil {
			return nil, fmt.Errorf("fetch: piece %d (%s): %w", i, ref.CID, err)
		}
		if err := VerifyChunk(ref.CID, b); err != nil {
			return nil, fmt.Errorf("fetch: piece %d (%s): %w", i, ref.CID, err)
		}
		pieces[i] = b
	}
	return chunker.Reassemble(m, pieces)
}

// SignedVerifyingSource assembles blobs with full validation — the manifest's Ed25519 signature
// (against PubKey), its Merkle root, the requested-address binding, and every piece CID — and exposes
// them as an (io.ReadCloser, size) pair. It is the byte-source seam the get-side serve path injects
// once the partition's storage signing key is available; it is strictly stronger than VerifyingSource.
type SignedVerifyingSource struct {
	Manifests SignedManifestGetter
	Pieces    PieceGetter
	PubKey    ed25519.PublicKey
}

// Open resolves the signed manifest, validates + assembles the blob, and returns a reader over the
// bytes plus their length. The partition is the one the caller's chain established, never a token claim.
func (s SignedVerifyingSource) Open(ctx context.Context, partition, blobID string) (io.ReadCloser, int64, error) {
	if s.Manifests == nil {
		return nil, 0, errors.New("fetch: nil signed manifest getter")
	}
	m, sig, err := s.Manifests.GetSignedManifest(ctx, partition, blobID)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch: signed manifest for %s: %w", blobID, err)
	}
	data, err := AssembleSignedBlob(ctx, partition, blobID, m, sig, s.PubKey, s.Pieces)
	if err != nil {
		return nil, 0, err
	}
	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
}

// VerifyingSource assembles blobs from a ManifestGetter + PieceGetter with full CID verification and
// exposes them as an (io.ReadCloser, size) pair. Its Open method matches the byte-source seam the
// get-side serve path injects, so once diststore is wired into that surface this type is the data
// plane behind it. All verification happens before Open returns, so a failure becomes a clean error
// the caller can turn into a fail-closed status before any response header is committed.
type VerifyingSource struct {
	Manifests ManifestGetter
	Pieces    PieceGetter
}

// Open resolves the manifest, assembles + verifies the blob, and returns a reader over the bytes plus
// their length. The partition is the one the caller's chain established, never a token claim.
func (s VerifyingSource) Open(ctx context.Context, partition, blobID string) (io.ReadCloser, int64, error) {
	if s.Manifests == nil {
		return nil, 0, errors.New("fetch: nil manifest getter")
	}
	m, err := s.Manifests.GetManifest(ctx, partition, blobID)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch: manifest for %s: %w", blobID, err)
	}
	data, err := AssembleBlob(ctx, partition, blobID, m, s.Pieces)
	if err != nil {
		return nil, 0, err
	}
	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
}
