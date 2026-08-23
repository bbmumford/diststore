/*
 * Copyright (c) 2026 ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@orbtr.io
 */

package streamfetch

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"net"
	"testing"

	"github.com/bbmumford/diststore/chunker"
	"github.com/bbmumford/diststore/fetch"
)

// chunkMap splits data and returns the manifest, a cid→piece map, and the original.
func chunkMap(t *testing.T, size int) (*chunker.Manifest, map[string][]byte, []byte) {
	t.Helper()
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	m, pieces, err := chunker.Split(data, 0)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	byCID := make(map[string][]byte, len(pieces))
	for i, p := range pieces {
		byCID[m.Pieces[i].CID] = p
	}
	return m, byCID, data
}

// serveHolder is a minimal holder side: read the manifest frame, then answer each
// FetchRequest from its piece map (an in-band miss for a CID it does not hold).
func serveHolder(conn io.ReadWriter, held map[string][]byte) {
	if _, err := ReadFrame(conn); err != nil {
		return
	}
	for {
		reqBytes, err := ReadFrame(conn)
		if err != nil {
			return
		}
		req, err := fetch.DecodeFetchRequest(reqBytes)
		if err != nil {
			return
		}
		var resp fetch.FetchResponse
		if p, ok := held[req.CID]; ok {
			resp = fetch.FetchResponse{CID: req.CID, Payload: p}
		} else {
			resp = fetch.FetchResponse{CID: req.CID, Error: "not held here"}
		}
		rb, _ := fetch.EncodeFetchResponse(resp)
		if err := WriteFrame(conn, rb); err != nil {
			return
		}
	}
}

func TestFetchFromHolders_SkipsHoldersThatCannotServe(t *testing.T) {
	m, pieces, orig := chunkMap(t, 600*1024)

	held := map[string]map[string][]byte{
		"empty-holder":  {},     // holds nothing → serves misses
		"full-holder":   pieces, // holds all → serves the blob
	}
	dial := func(_ context.Context, agentID string, _ []byte) (io.ReadWriter, error) {
		server, client := net.Pipe()
		go serveHolder(server, held[agentID])
		return client, nil // net.Conn is an io.ReadWriteCloser
	}

	// Order the un-serving holder first — it must be skipped for the full one.
	order := []Holder{{AgentNodeID: "empty-holder"}, {AgentNodeID: "full-holder"}}
	blob, err := FetchFromHolders(context.Background(), m, order, dial)
	if err != nil {
		t.Fatalf("FetchFromHolders: %v", err)
	}
	if !bytes.Equal(blob, orig) {
		t.Fatal("fetched blob differs from the original")
	}
}

func TestFetchFromHolders_ErrorsWhenNoneServe(t *testing.T) {
	m, _, _ := chunkMap(t, 300*1024)
	dial := func(_ context.Context, agentID string, _ []byte) (io.ReadWriter, error) {
		server, client := net.Pipe()
		go serveHolder(server, map[string][]byte{}) // every holder holds nothing
		return client, nil
	}
	order := []Holder{{AgentNodeID: "a"}, {AgentNodeID: "b"}}
	if _, err := FetchFromHolders(context.Background(), m, order, dial); err == nil {
		t.Fatal("no holder serving must be an error, not a short blob")
	}
}
