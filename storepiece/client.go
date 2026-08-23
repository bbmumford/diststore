/*
 * Copyright (c) 2026 ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@orbtr.io
 */

package storepiece

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/bbmumford/diststore/chunker"
	"github.com/bbmumford/diststore/streamfetch"
)

// StoreBlob pushes a chunked blob to a holder over an already-open store stream — the
// client mirror of streamfetch.FetchBlob. It writes the manifest frame the holder
// scopes writes on, then each piece in order, verifying the CID the holder echoes for
// every piece. It returns on the first rejection (bad CID, quota exceeded, out of the
// authorized blob), so a partial push surfaces as an error rather than a silent gap.
// ctx is honoured between pieces so a cancelled upload stops promptly.
func StoreBlob(ctx context.Context, s io.ReadWriter, m *chunker.Manifest, pieces [][]byte) error {
	if len(pieces) != len(m.Pieces) {
		return fmt.Errorf("storepiece: %d pieces for a %d-piece manifest", len(pieces), len(m.Pieces))
	}
	// JSON to match the holder's ServeStoreStream, which json.Unmarshals the frame.
	mBytes, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("storepiece: encode manifest: %w", err)
	}
	if err := streamfetch.WriteFrame(s, mBytes); err != nil {
		return err
	}
	for i, p := range pieces {
		if err := ctx.Err(); err != nil {
			return err
		}
		wantCID := m.Pieces[i].CID
		reqBytes, err := EncodeStoreRequest(StoreRequest{CID: wantCID, Payload: p})
		if err != nil {
			return err
		}
		if err := streamfetch.WriteFrame(s, reqBytes); err != nil {
			return err
		}
		respBytes, err := streamfetch.ReadFrame(s)
		if err != nil {
			return err
		}
		resp, err := DecodeStoreResponse(respBytes)
		if err != nil {
			return err
		}
		if resp.Error != "" {
			return fmt.Errorf("storepiece: holder rejected piece %d (%s): %s", i, wantCID, resp.Error)
		}
		if resp.CID != wantCID {
			return fmt.Errorf("storepiece: holder echoed cid %s for piece %d, want %s", resp.CID, i, wantCID)
		}
	}
	return nil
}
