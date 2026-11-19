/*
 * Copyright (c) 2026 ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@orbtr.io
 */

// Package streamfetch is the framed point-to-point fetch protocol that runs over one
// established stream to a holder: the fetcher presents a blob's manifest, then
// requests each piece and CID-verifies it. It is the shared wire between a holder
// (which serves scoped pieces — see the agent's ServeFetchStream, built on ReadFrame
// / WriteFrame here) and a fetcher (a browser WASM client or an agent consumer, which
// calls FetchBlob). Keeping it here, next to chunker + fetch, lets both ends share
// one implementation instead of re-deriving the framing and CID checks separately.
package streamfetch

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/bbmumford/diststore/chunker"
	"github.com/bbmumford/diststore/fetch"
)

// Holder is one place a blob can be fetched from: the agent node hosting it and the
// dial ticket that authorizes this session to reach it.
type Holder struct {
	AgentNodeID string
	Ticket      []byte
}

// DialFunc opens a fetch stream to a holder, using its ticket to establish the
// authorized session (a browser bridges into the agent mesh; an agent consumer dials
// directly). It returns the stream to run FetchBlob over. A returned io.Closer is
// closed by FetchFromHolders when the holder is done with.
type DialFunc func(ctx context.Context, agentNodeID string, ticket []byte) (io.ReadWriter, error)

// FetchFromHolders fetches a blob by trying its holders until one serves the whole
// blob: each is dialed with its ticket, presented the manifest, and asked for every
// piece; a holder that cannot serve it all (unreachable, a miss, a closed stream) is
// skipped and the next tried. It returns the reassembled, CID-verified blob, or an
// error naming the last failure if no holder could serve it. Holders are tried in
// the given order, so callers order by preference (nearest/fastest first).
func FetchFromHolders(ctx context.Context, m *chunker.Manifest, holders []Holder, dial DialFunc) ([]byte, error) {
	if len(holders) == 0 {
		return nil, errors.New("streamfetch: no holders to fetch from")
	}
	var lastErr error
	for _, h := range holders {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		s, err := dial(ctx, h.AgentNodeID, h.Ticket)
		if err != nil {
			lastErr = fmt.Errorf("dial %s: %w", h.AgentNodeID, err)
			continue
		}
		blob, err := FetchBlob(ctx, s, m)
		if c, ok := s.(io.Closer); ok {
			_ = c.Close()
		}
		if err == nil {
			return blob, nil
		}
		lastErr = fmt.Errorf("fetch from %s: %w", h.AgentNodeID, err)
	}
	return nil, fmt.Errorf("streamfetch: no holder served the blob: %w", lastErr)
}

// FetchFromHoldersSHA256 is FetchFromHolders whose reassembled blob must also hash to
// want, the whole blob's SHA-256 as lowercase hex (chunker.VerifySHA256): the check a
// browser makes against the root the uploader declared, since WebCrypto has no
// BLAKE2b. An empty want is refused.
func FetchFromHoldersSHA256(ctx context.Context, m *chunker.Manifest, holders []Holder, dial DialFunc, want string) ([]byte, error) {
	blob, err := FetchFromHolders(ctx, m, holders, dial)
	if err != nil {
		return nil, err
	}
	if err := chunker.VerifySHA256(blob, m, want); err != nil {
		return nil, err
	}
	return blob, nil
}

// MaxFrame bounds one framed message. A manifest for the chunker's file cap is a few
// hundred piece records; a FetchResponse carries one plaintext chunk (256 KB default,
// a few MB at most). 8 MiB is generous headroom and caps a hostile peer's per-frame
// allocation.
const MaxFrame = 8 << 20

// WriteFrame writes one length-prefixed frame: [4-byte big-endian length][payload].
func WriteFrame(w io.Writer, payload []byte) error {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// ReadFrame reads one length-prefixed frame. A length over MaxFrame is a hard error,
// so a hostile peer cannot force an unbounded allocation. io.EOF on a clean half-close
// is returned as-is so a serve loop can end cleanly.
func ReadFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > MaxFrame {
		return nil, fmt.Errorf("streamfetch: frame too large (%d > %d)", n, MaxFrame)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// FetchBlob is the fetcher half: over an established stream to one holder, it presents
// m (the blob's manifest, from the download plan), requests each of m's pieces in
// turn, CID-verifies every returned chunk against the manifest, and reassembles the
// whole blob. The holder admits the manifest only if it recomputes to a RootCID the
// session's ticket authorized, so a substituted manifest yields no pieces; every chunk
// is re-hashed here as well, so wrong bytes for a CID are caught by the fetcher
// regardless of what the holder claims.
func FetchBlob(ctx context.Context, s io.ReadWriter, m *chunker.Manifest) ([]byte, error) {
	mBytes, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("streamfetch: marshal manifest: %w", err)
	}
	if err := WriteFrame(s, mBytes); err != nil {
		return nil, err
	}

	pieces := make([][]byte, len(m.Pieces))
	for i, ref := range m.Pieces {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		req, err := fetch.EncodeFetchRequest(fetch.FetchRequest{CID: ref.CID})
		if err != nil {
			return nil, err
		}
		if err := WriteFrame(s, req); err != nil {
			return nil, err
		}
		respBytes, err := ReadFrame(s)
		if err != nil {
			return nil, err
		}
		resp, err := fetch.DecodeFetchResponse(respBytes)
		if err != nil {
			return nil, err
		}
		if resp.Error != "" {
			return nil, fmt.Errorf("streamfetch: piece %d (%s): %s", i, ref.CID, resp.Error)
		}
		if chunker.ComputePieceCID(resp.Payload) != ref.CID {
			return nil, fmt.Errorf("streamfetch: piece %d (%s): returned bytes do not hash to the requested CID", i, ref.CID)
		}
		pieces[i] = resp.Payload
	}

	// Reassemble re-checks each piece CID + size + the total length against the
	// manifest — a structural second line of defence over the per-piece check above.
	return chunker.Reassemble(m, pieces)
}
