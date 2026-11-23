/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

// Package storepiece is the wire format for the upload write path: an uploader (the
// coordinator) pushes a single content-addressed piece to a holder, which stores it
// and echoes the CID. It is the write mirror of package fetch, shared so the holder
// (agent) and the pusher (coordinator, a different module) encode identically.
package storepiece

import "encoding/json"

// StoreRequest carries one piece to a holder. CID is the coordinator-computed content
// address; a holder MUST reject a payload that does not hash to it, so a corrupt or
// mislabelled piece is never stored under an expected name.
type StoreRequest struct {
	CID     string `json:"cid"`
	Payload []byte `json:"payload"`
}

// StoreResponse echoes the stored CID, or an Error (quota exceeded, CID mismatch,
// backend failure). CID is empty when Error is set.
type StoreResponse struct {
	CID   string `json:"cid,omitempty"`
	Error string `json:"error,omitempty"`
}

func EncodeStoreRequest(r StoreRequest) ([]byte, error) { return json.Marshal(&r) }

func DecodeStoreRequest(b []byte) (StoreRequest, error) {
	var r StoreRequest
	err := json.Unmarshal(b, &r)
	return r, err
}

func EncodeStoreResponse(r StoreResponse) ([]byte, error) { return json.Marshal(&r) }

func DecodeStoreResponse(b []byte) (StoreResponse, error) {
	var r StoreResponse
	err := json.Unmarshal(b, &r)
	return r, err
}
