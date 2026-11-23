/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package chunker

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// SHA256Hex answers each piece's SHA-256 and the whole blob's, as lowercase hex,
// in one pass over the pieces Split made: the blob's digest is taken
// incrementally, piece by piece, so it never needs the blob in one buffer of its
// own. A browser verifies with SHA-256 because WebCrypto has no BLAKE2b; the
// manifest keeps both.
func SHA256Hex(pieces [][]byte) (perPiece []string, whole string) {
	h := sha256.New()
	perPiece = make([]string, len(pieces))
	for i, p := range pieces {
		h.Write(p)
		sum := sha256.Sum256(p)
		perPiece[i] = hex.EncodeToString(sum[:])
	}
	return perPiece, hex.EncodeToString(h.Sum(nil))
}

// VerifySHA256 hashes data incrementally in m's pieces and refuses it unless the
// digest is want, the whole blob's SHA-256 as lowercase hex, and data is exactly
// m's pieces; an empty want is refused, since nothing then vouches for the blob.
func VerifySHA256(data []byte, m *Manifest, want string) error {
	if m == nil {
		return errors.New("chunker: nil manifest")
	}
	if want == "" {
		return errors.New("chunker: no SHA-256 to verify the blob against")
	}
	h := sha256.New()
	off := 0
	for _, pc := range m.Pieces {
		end := off + pc.SizeBytes
		if pc.SizeBytes < 0 || end > len(data) {
			return fmt.Errorf("chunker: blob shorter than its manifest at piece %d", pc.Index)
		}
		h.Write(data[off:end])
		off = end
	}
	if off != len(data) {
		return fmt.Errorf("chunker: blob is %d bytes, its manifest %d", len(data), off)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("chunker: blob SHA-256 %s is not %s", got, want)
	}
	return nil
}
