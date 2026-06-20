/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
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
