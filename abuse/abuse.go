/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package abuse

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bbmumford/diststore/chunker"
	"github.com/bbmumford/diststore/index"
)

// ReferenceSource is the consumer-provided hook that tells the blob
// guard which manifests are still referenced by live mesh records.
type ReferenceSource interface {
	IsReferenced(ctx context.Context, rootCID string) (bool, error)
}

// Errors surfaced by ValidateStorable / GarbageCollect / Check.
var (
	ErrNoReference              = errors.New("abuse: manifest references no live record")
	ErrPerPeerExceeded          = errors.New("abuse: per-uploader quota exceeded")
	ErrPerTypeExceeded          = errors.New("abuse: per-record-type size exceeded")
	ErrBadUploaderSig           = errors.New("abuse: bad uploader signature")
	ErrAlreadyHeld              = errors.New("abuse: cid already held")
	ErrReplicationQuotaExceeded = errors.New("abuse: replication quota exceeded")
	ErrPeerQuotaExceeded        = errors.New("abuse: per-peer replication quota exceeded")
	ErrRecordTypeNotAllowed     = errors.New("abuse: record type not allowed")
)

// Guard enforces anti-bloat rules and orchestrates GC of orphaned blobs.
type Guard struct {
	refs        ReferenceSource
	limits      PerTypeLimits
	perPeerCap  int64
	orphanGrace time.Duration

	replMaxBytesPerPeer int64
	replMaxBytesTotal   int64
	replTypeLimits      map[RecordType]int64
	replAllowedTypes    map[RecordType]struct{}

	mu          sync.Mutex
	perUploader map[string]int64
	firstSeen   map[string]time.Time

	replBytesPerPeer map[string]int64
	replBytesTotal   int64
	replCIDs         map[string]int64
}

// Config is the constructor input for Guard.
type Config struct {
	References  ReferenceSource
	Limits      PerTypeLimits
	PerPeerCap  int64
	OrphanGrace time.Duration

	MaxReplicationBytesPerPeer    int64
	MaxReplicationBytesTotal      int64
	ReplicationRecordTypeLimits   map[RecordType]int64
	ReplicationAllowedRecordTypes []RecordType
}

// New constructs a Guard.
func New(cfg Config) *Guard {
	if (cfg.Limits == PerTypeLimits{}) {
		cfg.Limits = DefaultPerTypeLimits()
	}
	grace := cfg.OrphanGrace
	if grace <= 0 {
		grace = DefaultOrphanGrace
	}
	allowed := cfg.ReplicationAllowedRecordTypes
	if len(allowed) == 0 {
		allowed = DefaultReplicationAllowedRecordTypes
	}
	allowSet := make(map[RecordType]struct{}, len(allowed))
	for _, t := range allowed {
		allowSet[t] = struct{}{}
	}
	typeLimits := make(map[RecordType]int64, len(cfg.ReplicationRecordTypeLimits))
	for k, v := range cfg.ReplicationRecordTypeLimits {
		typeLimits[k] = v
	}
	return &Guard{
		refs:                cfg.References,
		limits:              cfg.Limits,
		perPeerCap:          cfg.PerPeerCap,
		orphanGrace:         grace,
		replMaxBytesPerPeer: cfg.MaxReplicationBytesPerPeer,
		replMaxBytesTotal:   cfg.MaxReplicationBytesTotal,
		replTypeLimits:      typeLimits,
		replAllowedTypes:    allowSet,
		perUploader:         make(map[string]int64),
		firstSeen:           make(map[string]time.Time),
		replBytesPerPeer:    make(map[string]int64),
		replCIDs:            make(map[string]int64),
	}
}

// ValidateStorable enforces uploader-signature + reference + per-type +
// per-uploader gates on a signed manifest.
func (a *Guard) ValidateStorable(
	ctx context.Context,
	m *chunker.Manifest,
	uploaderSig []byte,
	uploaderPub ed25519.PublicKey,
	recordType RecordType,
) error {
	if m == nil {
		return errors.New("abuse: nil manifest")
	}
	if err := index.ValidateManifest(m, uploaderSig, uploaderPub); err != nil {
		if errors.Is(err, index.ErrInvalidManifest) {
			return fmt.Errorf("%w: %v", ErrBadUploaderSig, err)
		}
		return err
	}
	cap := a.limits.Cap(recordType)
	if cap > 0 && m.TotalBytes > cap {
		return fmt.Errorf("%w: type=%s size=%d cap=%d",
			ErrPerTypeExceeded, recordType, m.TotalBytes, cap)
	} else if cap <= 0 && recordType == RecordTypeUnknown {
		return fmt.Errorf("%w: type=%s has no cap", ErrPerTypeExceeded, recordType)
	}
	if a.refs != nil {
		ok, err := a.refs.IsReferenced(ctx, m.RootCID)
		if err != nil {
			return fmt.Errorf("abuse: reference check: %w", err)
		}
		if !ok {
			return fmt.Errorf("%w: rootCID=%s", ErrNoReference, m.RootCID)
		}
	}
	if a.perPeerCap > 0 {
		key := uploaderKey(uploaderPub)
		a.mu.Lock()
		cur := a.perUploader[key]
		a.mu.Unlock()
		if cur+m.TotalBytes > a.perPeerCap {
			return fmt.Errorf("%w: uploader=%s current=%d new=%d cap=%d",
				ErrPerPeerExceeded, key, cur, m.TotalBytes, a.perPeerCap)
		}
	}
	return nil
}

// ChargeUploader records bytes against an uploader's quota after a
// successful store.
func (a *Guard) ChargeUploader(uploader ed25519.PublicKey, bytes int64) {
	a.mu.Lock()
	a.perUploader[uploaderKey(uploader)] += bytes
	a.mu.Unlock()
}

// RefundUploader reverses ChargeUploader on delete / GC.
func (a *Guard) RefundUploader(uploader ed25519.PublicKey, bytes int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := uploaderKey(uploader)
	a.perUploader[key] -= bytes
	if a.perUploader[key] < 0 {
		a.perUploader[key] = 0
	}
}

// UploaderUsage returns the bytes currently charged to an uploader.
func (a *Guard) UploaderUsage(uploader ed25519.PublicKey) int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.perUploader[uploaderKey(uploader)]
}

// GCCandidate is a candidate returned by ListOrphans.
type GCCandidate struct {
	RootCID   string
	FirstSeen time.Time
}

// GarbageCollect scans manifests and returns root CIDs that are
// orphaned and have been orphaned for at least OrphanGrace.
func (a *Guard) GarbageCollect(ctx context.Context, rootCIDs []string) ([]string, error) {
	if a.refs == nil {
		return nil, nil
	}
	now := time.Now()
	var out []string
	a.mu.Lock()
	keep := make(map[string]time.Time, len(a.firstSeen))
	for _, cid := range rootCIDs {
		if t, ok := a.firstSeen[cid]; ok {
			keep[cid] = t
		}
	}
	a.firstSeen = keep
	a.mu.Unlock()

	for _, cid := range rootCIDs {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		ok, err := a.refs.IsReferenced(ctx, cid)
		if err != nil {
			return out, fmt.Errorf("abuse: gc reference check: %w", err)
		}
		if ok {
			a.mu.Lock()
			delete(a.firstSeen, cid)
			a.mu.Unlock()
			continue
		}
		a.mu.Lock()
		first, seen := a.firstSeen[cid]
		if !seen {
			a.firstSeen[cid] = now
			a.mu.Unlock()
			continue
		}
		a.mu.Unlock()
		if now.Sub(first) >= a.orphanGrace {
			out = append(out, cid)
		}
	}
	return out, nil
}

// ForgetOrphan drops bookkeeping for the given rootCID.
func (a *Guard) ForgetOrphan(rootCID string) {
	a.mu.Lock()
	delete(a.firstSeen, rootCID)
	a.mu.Unlock()
}

func uploaderKey(pub ed25519.PublicKey) string {
	return hex.EncodeToString(pub)
}

// Check is the lightweight admission gate used by opportunistic
// fetchers that have a CID + bytes but no signed manifest.
func (a *Guard) Check(
	ctx context.Context,
	cid string,
	fromPeerID string,
	size int64,
	recordType RecordType,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cid == "" {
		return errors.New("abuse: Check requires a non-empty cid")
	}
	if size < 0 {
		return fmt.Errorf("abuse: Check size must be non-negative, got %d", size)
	}
	if recordType == "" {
		recordType = RecordTypeReplication
	}

	if len(a.replAllowedTypes) > 0 {
		if _, ok := a.replAllowedTypes[recordType]; !ok {
			return fmt.Errorf("%w: type=%s", ErrRecordTypeNotAllowed, recordType)
		}
	}
	if typeCap, ok := a.replTypeLimits[recordType]; ok && typeCap > 0 && size > typeCap {
		return fmt.Errorf("%w: type=%s size=%d cap=%d",
			ErrPerTypeExceeded, recordType, size, typeCap)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if _, held := a.replCIDs[cid]; held {
		return ErrAlreadyHeld
	}
	if a.replMaxBytesTotal > 0 && a.replBytesTotal+size > a.replMaxBytesTotal {
		return fmt.Errorf("%w: current=%d new=%d cap=%d",
			ErrReplicationQuotaExceeded, a.replBytesTotal, size, a.replMaxBytesTotal)
	}
	cur := a.replBytesPerPeer[fromPeerID]
	if a.replMaxBytesPerPeer > 0 && cur+size > a.replMaxBytesPerPeer {
		return fmt.Errorf("%w: peer=%s current=%d new=%d cap=%d",
			ErrPeerQuotaExceeded, fromPeerID, cur, size, a.replMaxBytesPerPeer)
	}
	a.replBytesPerPeer[fromPeerID] = cur + size
	a.replBytesTotal += size
	a.replCIDs[cid] = size
	return nil
}

// ReleaseReplication refunds a previously-accepted replication charge.
func (a *Guard) ReleaseReplication(cid, fromPeerID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	size, held := a.replCIDs[cid]
	if !held {
		return
	}
	delete(a.replCIDs, cid)
	a.replBytesTotal -= size
	if a.replBytesTotal < 0 {
		a.replBytesTotal = 0
	}
	cur := a.replBytesPerPeer[fromPeerID]
	cur -= size
	if cur <= 0 {
		delete(a.replBytesPerPeer, fromPeerID)
	} else {
		a.replBytesPerPeer[fromPeerID] = cur
	}
}

// ReplicationUsage returns bytes currently charged to a source peer.
func (a *Guard) ReplicationUsage(fromPeerID string) int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.replBytesPerPeer[fromPeerID]
}

// ReplicationTotal returns the aggregate replication bytes charged.
func (a *Guard) ReplicationTotal() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.replBytesTotal
}
