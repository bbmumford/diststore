/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

// Package swarm provides the swarm-topics implementation of the
// diststore transport interface.
//
// The package ships an Unimplemented stub that satisfies all three
// transport surfaces (PublishTopic, RequestTopic, PeerTransport) and
// returns ErrUnimplemented from every call. Consumers compile-test
// wiring against the stub, then substitute a concrete implementation
// that forwards Publish / Query / FetchPiece to an active swarm
// Engine in the host process.
package swarm

import (
	"context"
	"errors"
)

// ErrUnimplemented is returned by every method on Unimplemented so
// callers can distinguish "transport not wired" from real errors
// (network, signature, payload) without string matching.
var ErrUnimplemented = errors.New("transport/swarm: not yet implemented")

// Unimplemented satisfies PublishTopic, RequestTopic and PeerTransport
// by returning ErrUnimplemented from every method. Used in tests and
// during wiring bring-up before a real swarm Engine is available.
type Unimplemented struct{}

// Publish implements PublishTopic.
func (Unimplemented) Publish(_ string, _ []byte) error { return ErrUnimplemented }

// Query implements RequestTopic.
func (Unimplemented) Query(_ context.Context, _ string, _ []byte) ([][]byte, error) {
	return nil, ErrUnimplemented
}

// FetchPiece implements PeerTransport.
func (Unimplemented) FetchPiece(_ context.Context, _ string, _ []byte) ([]byte, error) {
	return nil, ErrUnimplemented
}

// Engine is the live swarm surface the transport forwards to. It is defined HERE, in diststore,
// rather than by importing the swarm module, so diststore stays standalone (zero mesh dependencies):
// the host process adapts its real swarm engine to this small interface and injects it. The three
// methods mirror the three transport surfaces the substrate needs — topic publish, request/response
// query, and a per-peer piece fetch.
type Engine interface {
	Publish(topic string, data []byte) error
	Query(ctx context.Context, topic string, data []byte) ([][]byte, error)
	FetchPiece(ctx context.Context, peerID string, payload []byte) ([]byte, error)
}

// EngineTransport is the concrete substitute for Unimplemented: it satisfies PublishTopic,
// RequestTopic, and PeerTransport by forwarding to an injected Engine. A nil Engine returns
// ErrUnimplemented from every call — the same "transport not wired" signal as the stub — so a
// half-wired host fails with a clear, distinguishable error rather than a panic.
type EngineTransport struct {
	Engine Engine
}

// Publish implements PublishTopic.
func (t EngineTransport) Publish(topic string, data []byte) error {
	if t.Engine == nil {
		return ErrUnimplemented
	}
	return t.Engine.Publish(topic, data)
}

// Query implements RequestTopic.
func (t EngineTransport) Query(ctx context.Context, topic string, data []byte) ([][]byte, error) {
	if t.Engine == nil {
		return nil, ErrUnimplemented
	}
	return t.Engine.Query(ctx, topic, data)
}

// FetchPiece implements PeerTransport.
func (t EngineTransport) FetchPiece(ctx context.Context, peerID string, payload []byte) ([]byte, error) {
	if t.Engine == nil {
		return nil, ErrUnimplemented
	}
	return t.Engine.FetchPiece(ctx, peerID, payload)
}
