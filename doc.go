/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

// Package diststore implements a content-addressed, encrypted-at-rest,
// quota-aware distributed blob store.
//
// Sub-package layers:
//
//   - Local-only path:
//     chunker / index / crypto / storage / cache / quota / abuse.
//     Sufficient for a single-node encrypted blob cache.
//
//   - Mesh path:
//     transport / advertise / resolve / fetch / rarest / replicate.
//     Layered on the local path to participate in a peer mesh.
//
//   - On-chain path (optional):
//     onchain — proof-of-storage challenges, paid pinning,
//     EscrowClient interface (concrete impl lives in plutus).
//
// The root package itself exposes only the cross-cutting types that
// every sub-package shares: strong-typed IDs (BlobID, ChunkID,
// NodeID), the canonical Manifest + PieceRef, sentinel errors, and
// the functional-option Config struct.
//
// See README.md for the sub-package map + composition recipes.
package diststore
