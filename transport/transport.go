/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

// Package transport defines the small interface diststore needs from
// the underlying P2P gossip / RPC stack. Concrete adapters live in
// transport/swarm (swarm-topics engine) and transport/whisper
// (whisper engine).
//
// The interface split lets diststore-using code swap transports
// without touching the cache, advertise, resolve or fetch
// sub-packages.
package transport

import "context"

// PublishTopic publishes an envelope to a broadcast topic. The
// transport is expected to attach its own integrity / sender metadata
// at the wire level.
type PublishTopic interface {
	Publish(topic string, payload []byte) error
}

// RequestTopic broadcasts a query on a request/response topic and
// awaits replies from any peer that holds the asked-for content.
//
// Implementations may apply per-call timeouts and limit the number of
// responses they collect.
type RequestTopic interface {
	Query(ctx context.Context, topic string, payload []byte) ([][]byte, error)
}

// PeerTransport ships a single fetch RPC to a specific peer over the
// underlying connection (typically a stream multiplexed over the
// transport's existing session).
//
// The interface is intentionally narrow: callers send a serialized
// FetchRequest envelope (defined in the fetch sub-package) and receive
// back the serialized FetchResponse — or an error on transport
// failure / context cancellation.
type PeerTransport interface {
	FetchPiece(ctx context.Context, peerID string, request []byte) (response []byte, err error)
}
