/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package crypto

import (
	"crypto/ed25519"
	"errors"
)

// SignAdvertisement returns an Ed25519 signature over the envelope bytes.
// Mirrors the route package's signed-record pattern: the verifier needs
// the signing pubkey embedded alongside the signature in the envelope
// to avoid directory lookups.
func SignAdvertisement(priv ed25519.PrivateKey, envelope []byte) ([]byte, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, errors.New("crypto: bad private key size")
	}
	if len(envelope) == 0 {
		return nil, errors.New("crypto: empty envelope")
	}
	return ed25519.Sign(priv, envelope), nil
}

// VerifyAdvertisement returns nil when the signature is valid.
func VerifyAdvertisement(pub ed25519.PublicKey, envelope, sig []byte) error {
	if len(pub) != ed25519.PublicKeySize {
		return errors.New("crypto: bad public key size")
	}
	if !ed25519.Verify(pub, envelope, sig) {
		return errors.New("crypto: advertisement signature invalid")
	}
	return nil
}
