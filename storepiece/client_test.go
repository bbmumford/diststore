/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package storepiece

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"reflect"
	"testing"

	"github.com/bbmumford/diststore/chunker"
	"github.com/bbmumford/diststore/streamfetch"
)

// holder reads the manifest frame and then each piece frame from conn until it
// closes, echoing every piece's CID, and returns the CIDs it was sent.
func holder(t *testing.T, conn net.Conn, got chan<- []string) {
	defer conn.Close()
	var cids []string
	frame, err := streamfetch.ReadFrame(conn)
	if err != nil {
		t.Errorf("manifest frame: %v", err)
		got <- nil
		return
	}
	var m chunker.Manifest
	if err := json.Unmarshal(frame, &m); err != nil {
		t.Errorf("manifest: %v", err)
	}
	for {
		frame, err := streamfetch.ReadFrame(conn)
		if err != nil {
			got <- cids
			return
		}
		req, err := DecodeStoreRequest(frame)
		if err != nil {
			t.Errorf("piece frame: %v", err)
			got <- cids
			return
		}
		cids = append(cids, req.CID)
		resp, _ := EncodeStoreResponse(StoreResponse{CID: req.CID})
		if err := streamfetch.WriteFrame(conn, resp); err != nil {
			got <- cids
			return
		}
	}
}

// StorePieces sends the whole manifest and only the named pieces, in the order
// named; StoreBlob sends every piece in manifest order.
func TestStorePiecesPushesOnlyTheNamedPieces(t *testing.T) {
	data := bytes.Repeat([]byte("orbtr-storage-piece "), 3*256*1024/20+1)
	m, pieces, err := chunker.Split(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pieces) < 3 {
		t.Fatalf("split into %d pieces, want at least 3", len(pieces))
	}
	for name, tc := range map[string]struct {
		push func(c net.Conn) error
		want []string
	}{
		"subset": {func(c net.Conn) error { return StorePieces(context.Background(), c, m, pieces, []int{2, 0}) },
			[]string{m.Pieces[2].CID, m.Pieces[0].CID}},
		"whole blob": {func(c net.Conn) error { return StoreBlob(context.Background(), c, m, pieces) },
			func() []string {
				var all []string
				for _, p := range m.Pieces {
					all = append(all, p.CID)
				}
				return all
			}()},
	} {
		client, server := net.Pipe()
		got := make(chan []string, 1)
		go holder(t, server, got)
		if err := tc.push(client); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		_ = client.Close()
		if cids := <-got; !reflect.DeepEqual(cids, tc.want) {
			t.Errorf("%s: the holder was sent %v, want %v", name, cids, tc.want)
		}
	}
}

// An index outside the manifest, or one named twice, is refused before any frame
// is written.
func TestStorePiecesRefusesABadIndexBeforeWriting(t *testing.T) {
	m, pieces, err := chunker.Split(bytes.Repeat([]byte{7}, 600*1024), 0)
	if err != nil {
		t.Fatal(err)
	}
	for name, indices := range map[string][]int{
		"negative":    {-1},
		"past":        {len(pieces)},
		"named twice": {0, 0},
	} {
		var buf bytes.Buffer
		if err := StorePieces(context.Background(), &buf, m, pieces, indices); err == nil {
			t.Errorf("%s: StorePieces(%v) accepted", name, indices)
		}
		if buf.Len() != 0 {
			t.Errorf("%s: %d bytes written before the refusal", name, buf.Len())
		}
	}
}
