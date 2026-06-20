/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

// Package whisper bridges diststore's transport interface to a
// github.com/bbmumford/whisper engine. To keep diststore
// dependency-free, whisper is not imported directly — the bridge is
// structural: any *whisper.Engine whose method set matches
// PublishTopic / RequestTopic is accepted via the EngineAdapter
// wrapper below.
//
// Pair with transport/swarm when callers prefer the swarm-topics
// transport; both adapters satisfy the same diststore-side surface
// so the caller picks per deployment without touching diststore.
package whisper

import "context"

// PublishEngine is the subset of *whisper.Engine the advertiser needs.
type PublishEngine interface {
	Publish(topic string, payload []byte) error
}

// QueryEngine is the subset of *whisper.Engine the resolver needs.
type QueryEngine interface {
	Query(ctx context.Context, topic string, payload []byte) ([][]byte, error)
}

// EngineAdapter wraps a whisper engine value and exposes the small
// interfaces diststore consumes.
type EngineAdapter struct {
	Pub PublishEngine
	Q   QueryEngine
}

// Publish forwards to the wrapped engine.
func (e EngineAdapter) Publish(topic string, payload []byte) error {
	return e.Pub.Publish(topic, payload)
}

// Query forwards to the wrapped engine.
func (e EngineAdapter) Query(ctx context.Context, topic string, payload []byte) ([][]byte, error) {
	return e.Q.Query(ctx, topic, payload)
}
