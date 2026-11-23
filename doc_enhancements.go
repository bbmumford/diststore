/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package diststore

// Extension points and current limitations across the sub-packages.
// These are notes for consumers integrating diststore so they know
// which seams accept additional wiring.
//
// ec/: Reed-Solomon K-of-N encoder/decoder primitives are usable
// standalone. Integration with replicate.Replicator (fan-out K-of-N
// shards instead of N full replicas) and with fetch (reconstruct from
// any K shards when the manifest carries an EC header) is wired at the
// consumer level; the manifest must carry (K, N, shardCIDs[N]) when
// EC is in use.
//
// repair/: the priority-queue sweeper orders by (replica count ASC,
// last-verified-at ASC). The consumer supplies the re-replication
// callback that pops items and drives replicate.Replicator with a
// per-blob target. BandwidthFraction is enforced via the per-peer
// budget in fetch/budget.go.
//
// proto/: JSON envelopes defined under advertise/ and fetch/ are the
// canonical wire form today. The protobuf surface under proto/ is the
// preferred form for new peers; consumers that ship a mixed fleet
// should keep JSON decoding enabled until every node speaks protobuf.
//
// transport/swarm/: ships an Unimplemented stub that returns a typed
// error. The consumer binds Publish / Query / FetchPiece to its active
// swarm Engine so callers can swap the whisper adapter for swarm
// without code changes.
//
// placement/locality: LocalRegion / LocalAZ are caller-supplied. The
// consumer's heartbeat/route layer is the canonical source for those
// values; rebind on topology change to keep placement decisions fresh.
//
// metrics/otel: metrics.Sink is the package-internal counter/histogram
// surface. Consumers that want OTel meters bridge metrics.Sink to
// their meter provider when trace.Tracer is non-nil.
