/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package chunker

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// The pieces' digests and the whole blob's, taken in one pass, match one-shot
// digests; the blob verifies against its own and is refused against another
// digest, none, or when cut short.
func TestSHA256HexAndVerifySHA256(t *testing.T) {
	data := make([]byte, 2*PieceSize+100)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	m, pieces, err := Split(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	perPiece, whole := SHA256Hex(pieces)
	sum := sha256.Sum256(data)
	if whole != hex.EncodeToString(sum[:]) || len(perPiece) != len(pieces) {
		t.Fatalf("whole %s, %d piece digests", whole, len(perPiece))
	}
	for i, p := range pieces {
		s := sha256.Sum256(p)
		if perPiece[i] != hex.EncodeToString(s[:]) {
			t.Errorf("piece %d digest %s", i, perPiece[i])
		}
	}
	if err := VerifySHA256(data, m, whole); err != nil {
		t.Fatalf("its own digest: %v", err)
	}
	other := sha256.Sum256([]byte("other"))
	for name, try := range map[string]struct {
		data []byte
		want string
	}{
		"another digest": {data, hex.EncodeToString(other[:])},
		"no digest":      {data, ""},
		"cut short":      {data[:len(data)-1], whole},
		"one byte more":  {append(append([]byte{}, data...), 0), whole},
	} {
		if err := VerifySHA256(try.data, m, try.want); err == nil {
			t.Errorf("%s: verified", name)
		}
	}
}
