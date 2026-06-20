/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

// Package mocktransport supplies in-memory transport fakes used by
// diststore tests + chaos test harness.
package mocktransport

import (
	"context"
	"errors"
	"sync"

	"github.com/bbmumford/diststore/fetch"
)

// MapTransport implements fetch.PeerTransport with a peerID → bytes
// map. Suitable for unit + chaos tests.
type MapTransport struct {
	mu       sync.Mutex
	payloads map[string][]byte
	errs     map[string]error
}

// NewMapTransport returns an empty MapTransport.
func NewMapTransport() *MapTransport {
	return &MapTransport{
		payloads: make(map[string][]byte),
		errs:     make(map[string]error),
	}
}

// SetPayload sets the bytes returned for peerID.
func (m *MapTransport) SetPayload(peerID string, data []byte) {
	m.mu.Lock()
	m.payloads[peerID] = data
	m.mu.Unlock()
}

// SetError sets the error returned for peerID.
func (m *MapTransport) SetError(peerID string, err error) {
	m.mu.Lock()
	m.errs[peerID] = err
	m.mu.Unlock()
}

// FetchPiece implements fetch.PeerTransport.
func (m *MapTransport) FetchPiece(_ context.Context, peerID string, req fetch.FetchRequest) (fetch.FetchResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.errs[peerID]; err != nil {
		return fetch.FetchResponse{}, err
	}
	data, ok := m.payloads[peerID]
	if !ok {
		return fetch.FetchResponse{}, errors.New("mocktransport: peer not configured")
	}
	return fetch.FetchResponse{CID: req.CID, Payload: data}, nil
}
