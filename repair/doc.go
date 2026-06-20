/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

// Package repair contains the priority repair queue + bandwidth-budgeted
// background sweeper.
//
// The queue orders items by (replica count ASC, last-verified-at ASC)
// so the most-degraded blobs repair first. The sweeper drains the
// queue under BandwidthFraction, which is enforced via the per-peer
// budget in fetch/budget.go.
//
// Consumer wiring:
//   - feed the queue from health.Report producers (the consumer
//     decides what counts as "under-replicated").
//   - supply the re-replication callback that pops items and calls
//     replicate.Replicator with a per-blob target.
//   - emit operational metrics (queue_depth, repairs_attempted,
//     repairs_succeeded) via metrics.Sink in the consumer.
package repair
