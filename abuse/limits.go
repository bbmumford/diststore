/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

// Package abuse enforces per-uploader, per-source-peer and per-record-
// type byte caps against the blob cache.
//
// It manages two distinct ledgers — uploader (signed manifests) and
// replication (opportunistic fetches) — so the two paths cannot
// cross-contaminate quotas.
package abuse

import "time"

// RecordType mirrors the diststore root type but is duplicated here so
// the abuse package has no upward dependency on root.
type RecordType string

const (
	RecordTypeListing     RecordType = "listing"
	RecordTypeMessage     RecordType = "message"
	RecordTypeEvidence    RecordType = "evidence"
	RecordTypeAvatar      RecordType = "avatar"
	RecordTypeUnknown     RecordType = "unknown"
	RecordTypeReplication RecordType = "replication"
)

// PerTypeLimits captures per-record-type byte caps.
type PerTypeLimits struct {
	Listing  int64
	Message  int64
	Evidence int64
	Avatar   int64
	Unknown  int64
}

// DefaultPerTypeLimits returns the recommended per-record-type byte
// caps (50MB listings, 100MB messages, 20MB evidence, 2MB avatars).
func DefaultPerTypeLimits() PerTypeLimits {
	return PerTypeLimits{
		Listing:  50 * 1024 * 1024,
		Message:  100 * 1024 * 1024,
		Evidence: 20 * 1024 * 1024,
		Avatar:   2 * 1024 * 1024,
	}
}

// Cap returns the byte cap for a given record type.
func (l PerTypeLimits) Cap(t RecordType) int64 {
	switch t {
	case RecordTypeListing:
		return l.Listing
	case RecordTypeMessage:
		return l.Message
	case RecordTypeEvidence:
		return l.Evidence
	case RecordTypeAvatar:
		return l.Avatar
	default:
		return l.Unknown
	}
}

// DefaultReplicationAllowedRecordTypes is the initial accept-list for
// the Check() admission path.
var DefaultReplicationAllowedRecordTypes = []RecordType{
	RecordTypeReplication,
	RecordTypeListing,
	"attachment",
	"dispute",
}

// DefaultOrphanGrace is the grace period before unreferenced manifests
// become eligible for GC.
const DefaultOrphanGrace = 24 * time.Hour
