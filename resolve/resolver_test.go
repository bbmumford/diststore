/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package resolve

import (
	"context"
	"encoding/json"
	"testing"
)

type stubHas struct{ have map[string]bool }

func (s stubHas) Has(_ context.Context, cid string) (bool, error) { return s.have[cid], nil }

type cannedQuery struct{ responses [][]byte }

func (c cannedQuery) Query(_ context.Context, _ string, _ []byte) ([][]byte, error) {
	return c.responses, nil
}

func TestOnMessageHoldsCID(t *testing.T) {
	r, err := New(
		stubHas{have: map[string]bool{"cid-1": true}},
		nil,
		Config{PeerID: "node-1", LatencyHintMs: 42},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	reqPayload, _ := json.Marshal(&FindRequest{CID: "cid-1", Nonce: "n-1"})
	resp, err := r.OnMessage(context.Background(), "peer-a", TopicBlobFind, reqPayload)
	if err != nil {
		t.Fatalf("OnMessage: %v", err)
	}
	if len(resp) == 0 {
		t.Fatal("expected non-empty response")
	}
	var parsed FindResponse
	if err := json.Unmarshal(resp, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Nonce != "n-1" || len(parsed.Answers) != 1 {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestOnMessageDoesNotHave(t *testing.T) {
	r, _ := New(stubHas{have: map[string]bool{}}, nil, Config{PeerID: "n"})
	reqPayload, _ := json.Marshal(&FindRequest{CID: "x"})
	resp, err := r.OnMessage(context.Background(), "p", TopicBlobFind, reqPayload)
	if err != nil {
		t.Fatalf("OnMessage: %v", err)
	}
	if resp != nil {
		t.Errorf("expected nil response, got %d bytes", len(resp))
	}
}

func TestFindDedups(t *testing.T) {
	mkResp := func(peer string, ts int64) []byte {
		b, _ := json.Marshal(&FindResponse{Answers: []PeerAnswer{{PeerID: peer, LastSeenUnixMs: ts}}})
		return b
	}
	q := cannedQuery{responses: [][]byte{
		mkResp("peer-A", 100),
		mkResp("peer-B", 150),
		mkResp("peer-A", 200),
	}}
	r, _ := New(stubHas{}, q, Config{PeerID: "asker"})
	answers, err := r.Find(context.Background(), "cid-z")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(answers) != 2 {
		t.Fatalf("answers = %d want 2", len(answers))
	}
}

func TestFindRejectsNilTransport(t *testing.T) {
	r, _ := New(stubHas{}, nil, Config{PeerID: "asker"})
	if _, err := r.Find(context.Background(), "cid"); err == nil {
		t.Error("expected error")
	}
}
