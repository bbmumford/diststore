/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package replicate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bbmumford/diststore/chunker"
	"github.com/bbmumford/diststore/fetch"
	"github.com/bbmumford/diststore/rarest"
	"github.com/bbmumford/diststore/resolve"
)

// warnings records each warning the replicator logs, with its arguments.
type warnings struct {
	mu   sync.Mutex
	msgs []string
	args [][]any
	got  chan struct{}
}

func (w *warnings) Warn(msg string, args ...any) {
	w.mu.Lock()
	w.msgs = append(w.msgs, msg)
	w.args = append(w.args, args)
	w.mu.Unlock()
	select {
	case w.got <- struct{}{}:
	default:
	}
}

// The seed loop has no caller to return a scan's error to, so a scan that fails
// on its own tick is logged with the error that stopped it.
func TestTheSeedLoopLogsAFailedScan(t *testing.T) {
	down := errors.New("candidate source down")
	rep, err := New(Config{
		PeerID:        "self",
		FetchInterval: time.Hour,
		RarestFirst:   rarest.New(time.Minute),
		Candidates: CandidateSourceFunc(func(context.Context) ([]string, error) {
			return nil, down
		}),
	}, newTestStore(t), nil, &fetch.Fetcher{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := &warnings{got: make(chan struct{}, 1)}
	rep.log = w
	if err := rep.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rep.Stop()
	select {
	case <-w.got:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop's first scan failed and nothing was logged")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.msgs) != 1 || w.msgs[0] != "replicate: scan failed" {
		t.Fatalf("logged %q, want one %q", w.msgs, "replicate: scan failed")
	}
	if a := w.args[0]; len(a) != 2 || a[0] != "err" || !errors.Is(a[1].(error), down) {
		t.Fatalf("logged arguments %v, want err and the candidate source's error", a)
	}
}

// A piece whose only peer answers with an error is not stored, and the scan that
// met it still completes: one peer failing to serve a piece is not the scan's
// failure, and the next tick asks again.
func TestTickSkipsAPieceItsOnlyPeerFailsToServe(t *testing.T) {
	data := []byte("piece-bytes-the-peer-fails-to-serve")
	cid := chunker.ComputePieceCID(data)
	tr := newMapTransport()
	tr.setPayload("peer-A", data)
	tr.setErr("peer-A", errors.New("peer-A refused the piece"))
	store := newTestStore(t)
	rf := rarest.New(time.Minute)
	rf.Observe("peer-A", cid, time.Now())
	fetcher, err := fetch.New(newStubResolver(t, []resolve.PeerAnswer{{PeerID: "peer-A"}}), tr, fetch.Config{})
	if err != nil {
		t.Fatalf("fetch.New: %v", err)
	}
	rep, err := New(Config{
		PeerID:          "self",
		FetchInterval:   time.Hour,
		MaxParallelJobs: 2,
		RarestFirst:     rf,
	}, store, nil, fetcher)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rep.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	has, err := store.Has(ctx, cid)
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if has {
		t.Fatal("a piece whose only peer failed to serve it was stored")
	}
}
