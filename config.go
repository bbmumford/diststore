/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package diststore

import "time"

// Config is the top-level wiring struct consumed by the cache.Store
// constructor (and indirectly by the rest of the sub-packages). All
// fields have sensible defaults so callers can pass a zero-Config and
// layer on Options.
//
// Concrete sub-package configs (FetcherConfig, ReplicatorConfig, ...)
// live with their owner; this struct only carries the cross-cutting
// knobs that touch multiple sub-packages.
type Config struct {
	// Root is the on-disk directory the filesystem backend writes to.
	// Ignored when InMemory is true or a custom Backend is wired.
	Root string

	// InMemory selects the in-memory storage backend (tests + ephemeral
	// nodes). When true, Root is ignored.
	InMemory bool

	// AdaptiveChunks switches the chunker between fixed-256KB and the
	// small/medium/large band policy. Off keeps fixed-256KB.
	AdaptiveChunks bool

	// MaxFileSizeBytes caps a single Split call. 0 = use chunker default
	// (50 MB).
	MaxFileSizeBytes int64

	// AdvertiseInterval overrides the advertise loop cadence. 0 =
	// package default (60s).
	AdvertiseInterval time.Duration

	// FetchOverallTimeout caps the full Fetch call. 0 = package default
	// (2 × per-peer timeout).
	FetchOverallTimeout time.Duration

	// MaxConcurrentFetches sets the parallel-piece-fetch ceiling. 0 =
	// package default (3).
	MaxConcurrentFetches int

	// SignAdvertisements turns on Ed25519 signing of HaveAdvertisement
	// envelopes. Off keeps the legacy plain JSON form for one-version
	// backcompat.
	SignAdvertisements bool

	// VerifyChunksOnRead enables the BLAKE3 pin re-check per read.
	// Off skips the check; on costs ~1 hash per chunk-read.
	VerifyChunksOnRead bool

	// AntiTamperSweepInterval controls how often the cache verifier
	// re-hashes stored chunks looking for on-disk corruption. 0 disables
	// the sweep.
	AntiTamperSweepInterval time.Duration
}

// Option is the functional-option type used to tweak Config.
type Option func(*Config)

// WithRoot sets the on-disk root for the filesystem storage backend.
func WithRoot(path string) Option { return func(c *Config) { c.Root = path } }

// WithInMemory selects the in-memory backend.
func WithInMemory() Option { return func(c *Config) { c.InMemory = true } }

// WithAdaptiveChunks enables small/medium/large chunk-size banding.
func WithAdaptiveChunks() Option { return func(c *Config) { c.AdaptiveChunks = true } }

// WithMaxFileSize sets the Split cap.
func WithMaxFileSize(n int64) Option { return func(c *Config) { c.MaxFileSizeBytes = n } }

// WithAdvertiseInterval overrides the gossip cadence.
func WithAdvertiseInterval(d time.Duration) Option {
	return func(c *Config) { c.AdvertiseInterval = d }
}

// WithFetchOverallTimeout caps a full Fetch call.
func WithFetchOverallTimeout(d time.Duration) Option {
	return func(c *Config) { c.FetchOverallTimeout = d }
}

// WithMaxConcurrentFetches caps parallel piece fetches.
func WithMaxConcurrentFetches(n int) Option {
	return func(c *Config) { c.MaxConcurrentFetches = n }
}

// WithSignedAdvertisements turns on Ed25519-signed envelopes.
func WithSignedAdvertisements() Option {
	return func(c *Config) { c.SignAdvertisements = true }
}

// WithVerifyChunksOnRead enables per-read BLAKE3 pin re-check.
func WithVerifyChunksOnRead() Option {
	return func(c *Config) { c.VerifyChunksOnRead = true }
}

// WithAntiTamperSweep enables the periodic on-disk re-hash sweep.
func WithAntiTamperSweep(interval time.Duration) Option {
	return func(c *Config) { c.AntiTamperSweepInterval = interval }
}

// NewConfig builds a Config from zero plus any provided options.
func NewConfig(opts ...Option) Config {
	var c Config
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	return c
}
