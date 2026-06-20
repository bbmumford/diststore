/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package diststore

// BlobID is the content-address of a logical file (= Merkle root over its
// ordered piece CIDs). Strong type prevents accidental confusion with
// per-chunk ChunkID.
type BlobID string

// ChunkID is the BLAKE2b-256 hash of a single plaintext chunk.
type ChunkID string

// NodeID is the stable mesh identifier of a peer. Where it crosses into
// the route/aether/swarm packages, it is the same string they use.
type NodeID string

// String returns the BlobID as a string.
func (b BlobID) String() string { return string(b) }

// String returns the ChunkID as a string.
func (c ChunkID) String() string { return string(c) }

// String returns the NodeID as a string.
func (n NodeID) String() string { return string(n) }

// PieceRef describes one chunk in a manifest.
type PieceRef struct {
	Index     int    `json:"index"`
	CID       string `json:"cid"`
	SizeBytes int    `json:"sizeBytes"`
}

// Manifest is the piece list that describes a logical file.
//
// RootCID is the Merkle root over ordered piece CIDs and is the file's
// content address on the mesh.
type Manifest struct {
	RootCID    string     `json:"rootCid"`
	Pieces     []PieceRef `json:"pieces"`
	TotalBytes int64      `json:"totalBytes"`
	PieceSize  int        `json:"pieceSize"`
}

// RecordType classifies the kind of signed mesh record a blob is attached
// to. Used for per-type byte caps in the abuse guard.
type RecordType string

const (
	RecordTypeListing     RecordType = "listing"
	RecordTypeMessage     RecordType = "message"
	RecordTypeEvidence    RecordType = "evidence"
	RecordTypeAvatar      RecordType = "avatar"
	RecordTypeReplication RecordType = "replication"
	RecordTypeUnknown     RecordType = "unknown"
)
