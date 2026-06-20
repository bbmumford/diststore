/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package crypto

import (
	"crypto/aes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"filippo.io/edwards25519"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/nacl/box"
)

// WrapAlg selects the sealing scheme on a RecipientWrappedDataKey so a NaCl
// deployment and a FIPS deployment can coexist on the wire — the recipient
// reads Alg to pick the unwrap path.
type WrapAlg string

const (
	// WrapAlgNaClBox is X25519 + XSalsa20-Poly1305 (NaCl anonymous sealed
	// box). It is NOT FIPS-140 validated. It is the default because it reuses
	// the node's existing Ed25519 identity (mapped to X25519) so no second
	// keypair is needed. FIPS deployments must use WrapAlgP256AESKW.
	WrapAlgNaClBox WrapAlg = "nacl-box"

	// WrapAlgP256AESKW is ECDH-P256 + HKDF-SHA256 + RFC-3394 AES-256 Key Wrap
	// — all FIPS-140-approvable primitives. It targets a recipient's separate
	// P-256 key (NOT derived from Ed25519), which FIPS deployments must
	// provision and advertise.
	WrapAlgP256AESKW WrapAlg = "p256-aeskw"
)

// recipientKEKContext domain-separates the P-256 ECDH→HKDF key-encryption-key.
const recipientKEKContext = "diststore-recip-kek-v1"

// ErrRecipientUnwrap is returned when a recipient-sealed data key fails to open
// — wrong recipient key, tampered ciphertext, or an integrity-check failure.
var ErrRecipientUnwrap = errors.New("crypto: recipient unwrap failed")

// RecipientWrappedDataKey is a per-blob data key sealed to ONE recipient's
// public key. Unlike WrappedDataKey (tenant-symmetric, tenant_kek.go), no
// shared KEK is needed: only the holder of the recipient private key can
// unwrap, so the wrap composes with public gossip/advertise distribution.
type RecipientWrappedDataKey struct {
	Alg          WrapAlg `json:"alg"`          // sealing scheme
	RecipientPub []byte  `json:"recipientPub"` // recipient pub the DEK is sealed to (ed25519 32B for NaCl; P-256 SEC1 for FIPS)
	Wrapped      []byte  `json:"wrapped"`      // sealed DEK bytes (alg-specific framing)
}

// edPubToX25519Pub maps an Ed25519 public key to its X25519 (Montgomery)
// equivalent via the birational map, so the same identity that signs
// advertisements also receives sealed boxes. Pure-Go (filippo.io/edwards25519).
func edPubToX25519Pub(edPub ed25519.PublicKey) (*[32]byte, error) {
	if len(edPub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("crypto: ed25519 pub size %d", len(edPub))
	}
	p, err := new(edwards25519.Point).SetBytes(edPub)
	if err != nil {
		return nil, fmt.Errorf("crypto: ed25519 pub -> x25519: %w", err)
	}
	var x [32]byte
	copy(x[:], p.BytesMontgomery())
	return &x, nil
}

// edPrivToX25519Priv derives the X25519 private scalar from an Ed25519 private
// key: the RFC-8032 SHA-512 expansion of the 32-byte seed, RFC-7748-clamped.
func edPrivToX25519Priv(edPriv ed25519.PrivateKey) (*[32]byte, error) {
	if len(edPriv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("crypto: ed25519 priv size %d", len(edPriv))
	}
	h := sha512.Sum512(edPriv.Seed())
	h[0] &= 248
	h[31] &= 127
	h[31] |= 64
	var x [32]byte
	copy(x[:], h[:32])
	return &x, nil
}

// WrapDataKeyForRecipient seals a 32-byte DEK to recipientEdPub using a NaCl
// anonymous sealed box. Anonymous means there is no sender key in the wrap —
// the recipient pub alone decrypts, which is what composes with the public
// advertise distribution model. NaCl box generates its own ephemeral keypair
// and nonce internally, so there is no nonce-reuse risk.
func WrapDataKeyForRecipient(dek []byte, recipientEdPub ed25519.PublicKey) (RecipientWrappedDataKey, error) {
	if len(dek) != chacha20poly1305.KeySize {
		return RecipientWrappedDataKey{}, fmt.Errorf("crypto: dek size %d, want %d", len(dek), chacha20poly1305.KeySize)
	}
	xPub, err := edPubToX25519Pub(recipientEdPub)
	if err != nil {
		return RecipientWrappedDataKey{}, err
	}
	sealed, err := box.SealAnonymous(nil, dek, xPub, rand.Reader)
	if err != nil {
		return RecipientWrappedDataKey{}, fmt.Errorf("crypto: seal anonymous: %w", err)
	}
	return RecipientWrappedDataKey{
		Alg:          WrapAlgNaClBox,
		RecipientPub: append([]byte(nil), recipientEdPub...),
		Wrapped:      sealed,
	}, nil
}

// UnwrapToRecipient opens w using the recipient's Ed25519 keypair, AFTER the
// entitled gate passes. entitled==nil means the caller already authorised
// (test fixtures / self-wrap); otherwise its error is returned verbatim and NO
// crypto is attempted — fail-closed before key material is touched.
func UnwrapToRecipient(w RecipientWrappedDataKey, recipientEdPub ed25519.PublicKey,
	recipientEdPriv ed25519.PrivateKey, entitled func() error) ([]byte, error) {
	if entitled != nil {
		if err := entitled(); err != nil {
			return nil, err
		}
	}
	if w.Alg != WrapAlgNaClBox {
		return nil, fmt.Errorf("crypto: UnwrapToRecipient: alg %q is not %q", w.Alg, WrapAlgNaClBox)
	}
	xPub, err := edPubToX25519Pub(recipientEdPub)
	if err != nil {
		return nil, err
	}
	xPriv, err := edPrivToX25519Priv(recipientEdPriv)
	if err != nil {
		return nil, err
	}
	dek, ok := box.OpenAnonymous(nil, w.Wrapped, xPub, xPriv)
	if !ok {
		return nil, ErrRecipientUnwrap
	}
	return dek, nil
}

// WrapDataKeyForRecipientP256 is the FIPS path: a fresh ephemeral ECDH-P256
// key agrees a secret with recipientP256Pub, HKDF-SHA256 derives a 32-byte KEK,
// and the DEK is sealed with RFC-3394 AES-256 Key Wrap. The fresh ephemeral per
// wrap means a fresh KEK per DEK; AES-KW needs no nonce. Framing:
// Wrapped = ephemeralPubSEC1 || aesKeyWrap(KEK, dek).
func WrapDataKeyForRecipientP256(dek []byte, recipientP256Pub *ecdh.PublicKey) (RecipientWrappedDataKey, error) {
	if len(dek) != chacha20poly1305.KeySize {
		return RecipientWrappedDataKey{}, fmt.Errorf("crypto: dek size %d, want %d", len(dek), chacha20poly1305.KeySize)
	}
	if recipientP256Pub == nil {
		return RecipientWrappedDataKey{}, errors.New("crypto: nil recipient P-256 key (provision a FIPS keypair)")
	}
	eph, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return RecipientWrappedDataKey{}, fmt.Errorf("crypto: ephemeral p256: %w", err)
	}
	kek, err := p256KEK(eph, recipientP256Pub)
	if err != nil {
		return RecipientWrappedDataKey{}, err
	}
	wrapped, err := aesKeyWrap(kek, dek)
	if err != nil {
		return RecipientWrappedDataKey{}, err
	}
	ephPub := eph.PublicKey().Bytes()
	return RecipientWrappedDataKey{
		Alg:          WrapAlgP256AESKW,
		RecipientPub: append([]byte(nil), recipientP256Pub.Bytes()...),
		Wrapped:      append(ephPub, wrapped...),
	}, nil
}

// UnwrapToRecipientP256 reverses WrapDataKeyForRecipientP256 after the entitled
// gate passes (fail-closed, same as UnwrapToRecipient).
func UnwrapToRecipientP256(w RecipientWrappedDataKey, recipientP256Priv *ecdh.PrivateKey, entitled func() error) ([]byte, error) {
	if entitled != nil {
		if err := entitled(); err != nil {
			return nil, err
		}
	}
	if w.Alg != WrapAlgP256AESKW {
		return nil, fmt.Errorf("crypto: UnwrapToRecipientP256: alg %q is not %q", w.Alg, WrapAlgP256AESKW)
	}
	if recipientP256Priv == nil {
		return nil, errors.New("crypto: nil recipient P-256 private key")
	}
	// An uncompressed SEC1 P-256 point is 65 bytes (0x04 || X32 || Y32).
	const p256PubLen = 65
	if len(w.Wrapped) <= p256PubLen {
		return nil, fmt.Errorf("crypto: p256 wrap too short (%d)", len(w.Wrapped))
	}
	ephPub, err := ecdh.P256().NewPublicKey(w.Wrapped[:p256PubLen])
	if err != nil {
		return nil, fmt.Errorf("crypto: parse ephemeral p256 pub: %w", err)
	}
	kek, err := p256KEK(recipientP256Priv, ephPub)
	if err != nil {
		return nil, err
	}
	dek, err := aesKeyUnwrap(kek, w.Wrapped[p256PubLen:])
	if err != nil {
		return nil, ErrRecipientUnwrap
	}
	return dek, nil
}

// p256KEK derives the shared 32-byte KEK from an ECDH-P256 agreement.
func p256KEK(local *ecdh.PrivateKey, remote *ecdh.PublicKey) ([]byte, error) {
	secret, err := local.ECDH(remote)
	if err != nil {
		return nil, fmt.Errorf("crypto: p256 ecdh: %w", err)
	}
	kek := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, secret, nil, []byte(recipientKEKContext)), kek); err != nil {
		return nil, fmt.Errorf("crypto: p256 hkdf: %w", err)
	}
	return kek, nil
}

// aesKeyWrap implements RFC-3394 AES Key Wrap with the default IV. plaintext
// must be a multiple of 8 bytes and at least 16 (two blocks). Deterministic,
// no nonce, no CGO.
func aesKeyWrap(kek, plaintext []byte) ([]byte, error) {
	if len(plaintext)%8 != 0 || len(plaintext) < 16 {
		return nil, fmt.Errorf("crypto: keywrap plaintext length %d", len(plaintext))
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, fmt.Errorf("crypto: keywrap cipher: %w", err)
	}
	n := len(plaintext) / 8
	a := []byte{0xA6, 0xA6, 0xA6, 0xA6, 0xA6, 0xA6, 0xA6, 0xA6} // default IV
	r := make([]byte, len(plaintext))
	copy(r, plaintext)
	var buf [16]byte
	for j := 0; j < 6; j++ {
		for i := 0; i < n; i++ {
			copy(buf[:8], a)
			copy(buf[8:], r[i*8:i*8+8])
			block.Encrypt(buf[:], buf[:])
			copy(a, buf[:8])
			copy(r[i*8:i*8+8], buf[8:])
			xorRoundCount(a, uint64(n*j+i+1))
		}
	}
	out := make([]byte, 0, 8+len(plaintext))
	out = append(out, a...)
	out = append(out, r...)
	return out, nil
}

// aesKeyUnwrap reverses aesKeyWrap, verifying the RFC-3394 default IV as an
// integrity check.
func aesKeyUnwrap(kek, ciphertext []byte) ([]byte, error) {
	if len(ciphertext)%8 != 0 || len(ciphertext) < 24 {
		return nil, fmt.Errorf("crypto: keyunwrap length %d", len(ciphertext))
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, fmt.Errorf("crypto: keyunwrap cipher: %w", err)
	}
	n := len(ciphertext)/8 - 1
	a := make([]byte, 8)
	copy(a, ciphertext[:8])
	r := make([]byte, len(ciphertext)-8)
	copy(r, ciphertext[8:])
	var buf [16]byte
	for j := 5; j >= 0; j-- {
		for i := n - 1; i >= 0; i-- {
			xorRoundCount(a, uint64(n*j+i+1))
			copy(buf[:8], a)
			copy(buf[8:], r[i*8:i*8+8])
			block.Decrypt(buf[:], buf[:])
			copy(a, buf[:8])
			copy(r[i*8:i*8+8], buf[8:])
		}
	}
	for _, b := range a {
		if b != 0xA6 {
			return nil, errors.New("crypto: keyunwrap integrity check failed")
		}
	}
	return r, nil
}

// xorRoundCount XORs the big-endian round counter t into the trailing bytes of
// the 8-byte A register, per RFC-3394.
func xorRoundCount(a []byte, t uint64) {
	a[0] ^= byte(t >> 56)
	a[1] ^= byte(t >> 48)
	a[2] ^= byte(t >> 40)
	a[3] ^= byte(t >> 32)
	a[4] ^= byte(t >> 24)
	a[5] ^= byte(t >> 16)
	a[6] ^= byte(t >> 8)
	a[7] ^= byte(t)
}

// WrappingRecord is the signed, on-wire announcement that a blob's data key has
// been sealed to a recipient. It is canonical-JSON + Ed25519 (like the advertise
// path), NOT protobuf. BlobCID anchors the wrap to its blob the same way
// EncryptForStorage binds CID as AAD — a verifier rejects a wrap whose BlobCID
// does not match the blob it was requested for.
type WrappingRecord struct {
	BlobCID   string                  `json:"blobCid"`    // content-address of the blob (anchor)
	Recipient RecipientWrappedDataKey `json:"recipient"`  // sealed DEK + alg + recipient pub
	WrapperPub []byte                 `json:"wrapperPub"` // ed25519 pub of the issuing node (embedded)
	Signature []byte                  `json:"signature"`  // ed25519 over canonical(record, WrapperPub+Signature cleared)
}

// canonicalWrapping marshals r with WrapperPub + Signature cleared, producing
// the stable bytes SignWrapping signs and VerifyWrapping checks.
func canonicalWrapping(r *WrappingRecord) ([]byte, error) {
	clone := *r
	clone.WrapperPub = nil
	clone.Signature = nil
	return json.Marshal(&clone)
}

// SignWrapping signs r in place: it sets WrapperPub from priv and Signature
// over the canonical bytes, mirroring the advertise signed-record pattern (pub
// embedded so a verifier needs no directory lookup).
func SignWrapping(priv ed25519.PrivateKey, r *WrappingRecord) error {
	env, err := canonicalWrapping(r)
	if err != nil {
		return fmt.Errorf("crypto: canonical wrapping: %w", err)
	}
	sig, err := SignAdvertisement(priv, env)
	if err != nil {
		return err
	}
	r.WrapperPub = append([]byte(nil), priv.Public().(ed25519.PublicKey)...)
	r.Signature = sig
	return nil
}

// VerifyWrapping returns nil when r's signature is valid for its embedded
// WrapperPub. The caller is responsible for checking BlobCID against the blob
// it actually requested.
func VerifyWrapping(r *WrappingRecord) error {
	if len(r.WrapperPub) != ed25519.PublicKeySize {
		return errors.New("crypto: wrapping record missing wrapper pubkey")
	}
	env, err := canonicalWrapping(r)
	if err != nil {
		return fmt.Errorf("crypto: canonical wrapping: %w", err)
	}
	return VerifyAdvertisement(ed25519.PublicKey(r.WrapperPub), env, r.Signature)
}
