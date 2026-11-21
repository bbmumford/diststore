/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

// Fixed vectors for the recipient wrap's wire contract: data keys sealed to
// fixed keys, as stored envelopes and wrapping records hold them, and the
// canonical bytes and signature of one WrappingRecord. Every key is derived
// here from a label, and the sealed bytes were produced from the same labels,
// so a change to the KEK context, an Alg token, either scheme's framing or the
// record's canonical JSON turns one of these red.

package crypto

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func labelBytes(label string) []byte {
	h := sha256.Sum256([]byte(label))
	return h[:]
}

type vectorKeys struct {
	edPub  ed25519.PublicKey
	edPriv ed25519.PrivateKey
	p256   *ecdh.PrivateKey
	dek    []byte
	signer ed25519.PrivateKey
}

func fixedVectorKeys(t *testing.T) vectorKeys {
	t.Helper()
	edPriv := ed25519.NewKeyFromSeed(labelBytes("mesh-crypto recipientwrap vector: nacl recipient"))
	p256, err := ecdh.P256().NewPrivateKey(labelBytes("mesh-crypto recipientwrap vector: p256 recipient"))
	if err != nil {
		t.Fatalf("p256 vector key: %v", err)
	}
	return vectorKeys{
		edPub:  edPriv.Public().(ed25519.PublicKey),
		edPriv: edPriv,
		p256:   p256,
		dek:    labelBytes("mesh-crypto recipientwrap vector: data key"),
		signer: ed25519.NewKeyFromSeed(labelBytes("mesh-crypto recipientwrap vector: wrapper")),
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("vector hex: %v", err)
	}
	return b
}

// sealedVectors are Wrapped fields sealed to fixedVectorKeys, two per scheme,
// each with its own ephemeral key.
var sealedVectors = []struct {
	name       string
	alg        WrapAlg
	wrappedHex string
}{
	{"nacl-box 1", WrapAlgNaClBox,
		"509b59d2e02b25d5b31cb19c3624df3988110e1c3493b6a19fc968f0c970156a" +
			"3eec79619c9819d730694a5aaa071b37a7dc1263fc094070b51eeba21af381ed" +
			"266d63d9edd7ee1304aea81bceec9d75"},
	{"p256-aeskw 1", WrapAlgP256AESKW,
		"0492855c5109395611137c795e9498095e21babca296c9aab10aa116f96643bc" +
			"3912356fb84731f05e1ca35f87e23df2f66f19308012650b79638d9621319ebd" +
			"23681fdcea5aca2b87bb98397b22c5bd28050d6a1b337e76213343e4b8d0bf3f" +
			"89af214f27d6caad20"},
	{"nacl-box 2", WrapAlgNaClBox,
		"0103eb33b8031d506fb509ec5016b3462160efac19cfc6006ced8db24cec533c" +
			"288eaf1e981767dc0e50cbcf9c30f7e848f0450281f11f7fd17c96590516cc6b" +
			"68987653b1c6437e27a7b95850c03af8"},
	{"p256-aeskw 2", WrapAlgP256AESKW,
		"0445e9beca6eaa7f581b74e2055866d4ed36a67eee37710f04cc536704435e79" +
			"900de41fd117814c1fd810bfc93d94c4d7178484af87a95f1285b90bcc62141b" +
			"ecc870c5be4a0ca7a9c7db58406238dd18688dc5982cafa72f0b379d8eed8972" +
			"9f6265a80b2eed6b69"},
}

func TestStoredSealedVectorsOpen(t *testing.T) {
	k := fixedVectorKeys(t)
	for _, v := range sealedVectors {
		t.Run(v.name, func(t *testing.T) {
			wrapped := mustHex(t, v.wrappedHex)
			var got []byte
			var err error
			switch v.alg {
			case WrapAlgNaClBox:
				if len(wrapped) != 80 {
					t.Fatalf("nacl-box framing is %d bytes, want 80 (ephemeral key 32, tag 16, data key 32)", len(wrapped))
				}
				got, err = UnwrapToRecipient(RecipientWrappedDataKey{Alg: v.alg, RecipientPub: k.edPub, Wrapped: wrapped}, k.edPub, k.edPriv, EntitledAllowAll)
			case WrapAlgP256AESKW:
				if len(wrapped) != 105 {
					t.Fatalf("p256-aeskw framing is %d bytes, want 105 (SEC1 point 65, key wrap 40)", len(wrapped))
				}
				got, err = UnwrapToRecipientP256(RecipientWrappedDataKey{Alg: v.alg, RecipientPub: k.p256.PublicKey().Bytes(), Wrapped: wrapped}, k.p256, EntitledAllowAll)
			default:
				t.Fatalf("vector alg %q", v.alg)
			}
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if !bytes.Equal(got, k.dek) {
				t.Fatal("opened data key differs from the sealed one")
			}
		})
	}
}

// The canonical bytes SignWrapping signs and their Ed25519 signature are
// pinned by digest for a record carrying the first nacl-box vector, so a
// signed record already stored keeps verifying.
func TestWrappingRecordCanonicalBytesAndSignatureAreFixed(t *testing.T) {
	k := fixedVectorKeys(t)
	rec := &WrappingRecord{
		BlobCID: "bafy-mesh-crypto-vector",
		Recipient: RecipientWrappedDataKey{
			Alg:          WrapAlgNaClBox,
			RecipientPub: k.edPub,
			Wrapped:      mustHex(t, sealedVectors[0].wrappedHex),
		},
	}
	canonical, err := canonicalWrapping(rec)
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical) != 286 {
		t.Fatalf("canonical length %d, want 286", len(canonical))
	}
	if sum := sha256.Sum256(canonical); hex.EncodeToString(sum[:]) != "eaf6e6f40546481ab36416bec8113378016f3728ab0690fc97626f92bf332d79" {
		t.Fatalf("canonical bytes changed: %s", canonical)
	}
	if err := SignWrapping(k.signer, rec); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if !bytes.Equal(rec.WrapperPub, k.signer.Public().(ed25519.PublicKey)) {
		t.Fatal("SignWrapping must embed the signer's public key")
	}
	if sum := sha256.Sum256(rec.Signature); hex.EncodeToString(sum[:]) != "600bb8a0746cf350f14d91532c83cb18d17443d87b0c005c70a8ac3341a924e3" {
		t.Fatal("signature over the canonical bytes changed")
	}
	if err := VerifyWrapping(rec); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

// The JSON field names and the KEK context are the wire names and the
// derivation input that stored envelopes and wrapping records carry.
func TestWireNames(t *testing.T) {
	b, err := json.Marshal(RecipientWrappedDataKey{Alg: WrapAlgP256AESKW, RecipientPub: []byte{1}, Wrapped: []byte{2}})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"alg":"p256-aeskw","recipientPub":"AQ==","wrapped":"Ag=="}` {
		t.Fatalf("RecipientWrappedDataKey JSON = %s", b)
	}
	b, err = json.Marshal(WrappingRecord{BlobCID: "c", Recipient: RecipientWrappedDataKey{Alg: WrapAlgNaClBox}, WrapperPub: []byte{3}, Signature: []byte{4}})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"blobCid":"c","recipient":{"alg":"nacl-box","recipientPub":null,"wrapped":null},"wrapperPub":"Aw==","signature":"BA=="}` {
		t.Fatalf("WrappingRecord JSON = %s", b)
	}
	if recipientKEKContext != "diststore-recip-kek-v1" {
		t.Fatalf("KEK context = %q", recipientKEKContext)
	}
}

// SignWrapping refuses a private key of the wrong size instead of reaching
// ed25519.Sign, which panics on one, and leaves the record unsigned.
func TestSignWrappingRefusesAMalformedKey(t *testing.T) {
	k := fixedVectorKeys(t)
	w, err := WrapDataKeyForRecipient(k.dek, k.edPub)
	if err != nil {
		t.Fatal(err)
	}
	rec := &WrappingRecord{BlobCID: "cid", Recipient: w}
	if err := SignWrapping(ed25519.PrivateKey(k.signer[:31]), rec); err == nil {
		t.Fatal("a short private key must be refused")
	}
	if rec.WrapperPub != nil || rec.Signature != nil {
		t.Fatal("a refused sign must leave the record unsigned")
	}
}

// VerifyWrapping refuses a record whose embedded key is not an Ed25519 public
// key, whose signature is missing, or whose signature is another key's.
func TestVerifyWrappingRefusesAMalformedKeyOrSignature(t *testing.T) {
	k := fixedVectorKeys(t)
	w, err := WrapDataKeyForRecipient(k.dek, k.edPub)
	if err != nil {
		t.Fatal(err)
	}
	rec := &WrappingRecord{BlobCID: "cid", Recipient: w}
	if err := SignWrapping(k.signer, rec); err != nil {
		t.Fatal(err)
	}
	short := *rec
	short.WrapperPub = rec.WrapperPub[:31]
	if err := VerifyWrapping(&short); err == nil {
		t.Fatal("a short wrapper key must be refused")
	}
	unsigned := *rec
	unsigned.Signature = nil
	if err := VerifyWrapping(&unsigned); err == nil {
		t.Fatal("a missing signature must be refused")
	}
	foreign := *rec
	foreign.WrapperPub = k.edPub
	if err := VerifyWrapping(&foreign); err == nil {
		t.Fatal("a signature by another key must be refused")
	}
}

// Each unwrap path opens only its own scheme, and each wrap path refuses a
// data key that is not 32 bytes and a recipient key it cannot use.
func TestSchemeAndInputChecks(t *testing.T) {
	k := fixedVectorKeys(t)
	nacl, err := WrapDataKeyForRecipient(k.dek, k.edPub)
	if err != nil {
		t.Fatal(err)
	}
	p, err := WrapDataKeyForRecipientP256(k.dek, k.p256.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnwrapToRecipient(p, k.edPub, k.edPriv, EntitledAllowAll); err == nil || !strings.Contains(err.Error(), "is not") {
		t.Fatalf("nacl unwrap of a p256 wrap: %v", err)
	}
	if _, err := UnwrapToRecipientP256(nacl, k.p256, EntitledAllowAll); err == nil || !strings.Contains(err.Error(), "is not") {
		t.Fatalf("p256 unwrap of a nacl wrap: %v", err)
	}
	for _, n := range []int{0, 1, 64, 65} {
		short := RecipientWrappedDataKey{Alg: WrapAlgP256AESKW, Wrapped: p.Wrapped[:n]}
		if _, err := UnwrapToRecipientP256(short, k.p256, EntitledAllowAll); err == nil || !strings.Contains(err.Error(), "too short") {
			t.Fatalf("a p256 wrap of %d bytes, no longer than the SEC1 point: %v; want it refused as too short", n, err)
		}
	}
	if _, err := UnwrapToRecipientP256(p, nil, EntitledAllowAll); err == nil {
		t.Fatal("a nil P-256 private key must be refused")
	}
	if _, err := WrapDataKeyForRecipientP256(k.dek, nil); err == nil {
		t.Fatal("a nil P-256 public key must be refused")
	}
	if _, err := WrapDataKeyForRecipient(k.dek[:16], k.edPub); err == nil {
		t.Fatal("a 16-byte data key must be refused (nacl-box)")
	}
	if _, err := WrapDataKeyForRecipientP256(k.dek[:16], k.p256.PublicKey()); err == nil {
		t.Fatal("a 16-byte data key must be refused (p256-aeskw)")
	}
	if _, err := WrapDataKeyForRecipient(k.dek, k.edPub[:31]); err == nil {
		t.Fatal("a short Ed25519 recipient key must be refused")
	}
	if _, err := UnwrapToRecipient(nacl, k.edPub, k.edPriv[:63], EntitledAllowAll); err == nil {
		t.Fatal("a short Ed25519 private key must be refused")
	}
}
