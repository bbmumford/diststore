/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

// Package ec provides Reed-Solomon K-of-N erasure coding primitives.
//
// The encoder + decoder in ec.go produce / reconstruct shards
// independently of replicator/fetcher wiring, so the package is
// usable standalone.
//
// Consumer wiring (in callers, not in this package):
//   - replicator: when the placement policy returns UseEC, fan out
//     K-of-N shards across peers picked by placement.Policy instead
//     of N full replicas.
//   - fetcher: when reading a blob whose manifest carries an EC
//     header, fetch any K shards and Reconstruct + Join locally.
//   - manifest: when EC is in use the manifest must carry (K, N,
//     shardCIDs[N]) so the fetcher knows how to gather shards.
package ec
