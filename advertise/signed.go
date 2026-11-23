/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package advertise

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"

	dsCrypto "github.com/bbmumford/diststore/crypto"
)

// Signer attaches an Ed25519 signature + pubkey to a HaveAdvertisement.
// The signature covers the canonical JSON of the envelope with the
// PubKey + Signature fields cleared, so a verifier rebuilds the exact
// bytes that were signed.
type Signer interface {
	Sign(ad *HaveAdvertisement) error
}

// Ed25519Signer is the default Signer.
type Ed25519Signer struct {
	Priv ed25519.PrivateKey
}

// Sign implements Signer.
func (s Ed25519Signer) Sign(ad *HaveAdvertisement) error {
	if len(s.Priv) != ed25519.PrivateKeySize {
		return errors.New("advertise: bad signer key")
	}
	envelope := canonicalEnvelope(ad)
	enc, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("advertise: canonical marshal: %w", err)
	}
	sig, err := dsCrypto.SignAdvertisement(s.Priv, enc)
	if err != nil {
		return err
	}
	ad.PubKey = s.Priv.Public().(ed25519.PublicKey)
	ad.Signature = sig
	return nil
}

// Verify checks the signature embedded in ad. Returns nil on success;
// errors.Is matches diststore.ErrAdvertisementSignatureInvalid via the
// dsCrypto package's error wording.
func Verify(ad *HaveAdvertisement) error {
	if ad == nil {
		return errors.New("advertise: nil advertisement")
	}
	if len(ad.PubKey) != ed25519.PublicKeySize {
		return errors.New("advertise: missing or bad pubkey")
	}
	if len(ad.Signature) == 0 {
		return errors.New("advertise: missing signature")
	}
	envelope := canonicalEnvelope(ad)
	enc, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("advertise: canonical marshal: %w", err)
	}
	return dsCrypto.VerifyAdvertisement(ad.PubKey, enc, ad.Signature)
}

// canonicalEnvelope returns a copy of ad with PubKey + Signature
// cleared so signers + verifiers agree on the message bytes.
func canonicalEnvelope(ad *HaveAdvertisement) HaveAdvertisement {
	cp := *ad
	cp.PubKey = nil
	cp.Signature = nil
	return cp
}
