/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package swarm

import (
	"context"
	"errors"
	"testing"
)

// fakeEngine records calls and returns canned data — stands in for a live swarm engine.
type fakeEngine struct {
	pubTopic   string
	pubData    []byte
	queryResp  [][]byte
	piece      []byte
	gotPeer    string
	gotPayload []byte
}

func (e *fakeEngine) Publish(topic string, data []byte) error {
	e.pubTopic, e.pubData = topic, data
	return nil
}
func (e *fakeEngine) Query(_ context.Context, _ string, _ []byte) ([][]byte, error) {
	return e.queryResp, nil
}
func (e *fakeEngine) FetchPiece(_ context.Context, peerID string, payload []byte) ([]byte, error) {
	e.gotPeer, e.gotPayload = peerID, payload
	return e.piece, nil
}

func TestEngineTransport_ForwardsToEngine(t *testing.T) {
	eng := &fakeEngine{queryResp: [][]byte{[]byte("a"), []byte("b")}, piece: []byte("chunk")}
	tr := EngineTransport{Engine: eng}
	ctx := context.Background()

	if err := tr.Publish("cid-topic", []byte("payload")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if eng.pubTopic != "cid-topic" || string(eng.pubData) != "payload" {
		t.Fatalf("publish not forwarded: %+v", eng)
	}

	resp, err := tr.Query(ctx, "find", []byte("q"))
	if err != nil || len(resp) != 2 {
		t.Fatalf("query not forwarded: %v %v", resp, err)
	}

	piece, err := tr.FetchPiece(ctx, "peer-A", []byte("req"))
	if err != nil || string(piece) != "chunk" {
		t.Fatalf("fetchpiece not forwarded: %q %v", piece, err)
	}
	if eng.gotPeer != "peer-A" || string(eng.gotPayload) != "req" {
		t.Fatalf("fetchpiece args not forwarded: %+v", eng)
	}
}

func TestEngineTransport_NilEngineIsUnimplemented(t *testing.T) {
	var tr EngineTransport // nil engine
	ctx := context.Background()
	if err := tr.Publish("t", nil); !errors.Is(err, ErrUnimplemented) {
		t.Fatalf("nil engine publish must be ErrUnimplemented, got %v", err)
	}
	if _, err := tr.Query(ctx, "t", nil); !errors.Is(err, ErrUnimplemented) {
		t.Fatalf("nil engine query must be ErrUnimplemented, got %v", err)
	}
	if _, err := tr.FetchPiece(ctx, "p", nil); !errors.Is(err, ErrUnimplemented) {
		t.Fatalf("nil engine fetchpiece must be ErrUnimplemented, got %v", err)
	}
}
