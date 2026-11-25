# diststore

diststore is a distributed blob store for peer-to-peer meshes. It splits a blob
into content-addressed chunks under a signed Merkle manifest, keeps them
encrypted at rest in a quota-aware local cache, advertises what it holds,
answers "who has piece X?", fetches pieces from several peers at once and
verifies each against its CID, and can seed, place, erasure-code and repair
replicas across the mesh. Each layer is its own package, so a caller takes
only what it needs: a local encrypted cache alone, or the whole mesh path.

diststore talks to the network only through its `transport` interfaces
(`PublishTopic`, `RequestTopic`, `PeerTransport`); `transport/swarm` and
`transport/whisper` adapt them to those gossip engines without diststore
importing either.

## Packages

- **types.go / errors.go / config.go** — root types, sentinel errors,
  functional `Option`s.
- **chunker/** — Split / Reassemble + adaptive chunk sizing.
- **index/** — Merkle-root + signed piece manifest.
- **crypto/** — pluggable `Cipher` (XChaCha20-Poly1305 default,
  AES-256-GCM alternative), a key-encryption key per partition
  (`DerivePartitionKEK`), the storage key derived from a node's identity key
  (`DeriveStorageKey`, HKDF-SHA256), advertisement signing, and the
  per-recipient data-key wrap (`nacl-box`, `p256-aeskw`) with its signed
  `WrappingRecord`.
- **storage/** — pluggable `Backend` (filesystem default, in-memory).
- **cache/** — local encrypted store + anti-tamper verifier.
- **quota/** — headroom-based dynamic disk budget.
- **abuse/** — per-uploader + per-peer + per-type byte caps.
- **transport/** — `PublishTopic / RequestTopic / PeerTransport`
  interface + `swarm` and `whisper` adapters.
- **advertise/** — "I have piece X" gossip (signed envelopes).
- **resolve/** — "who has piece X?" request/response.
- **fetch/** — parallel chunk fetch + per-peer bandwidth budget +
  BLAKE3 verifier.
- **rarest/** — rarest-first replica state.
- **replicate/** — opt-in seed loop.
- **repair/** — priority-queue repair sweeper.
- **placement/** — pluggable placement policy (random / locality / load
  / hybrid default).
- **ec/** — Reed-Solomon K-of-N erasure coding primitives.
- **storepiece/** — the upload write path: a coordinator pushes one piece to a
  holder, which stores it and echoes the CID.
- **streamfetch/** — the framed fetch protocol over one stream to a holder:
  present a manifest, then request and CID-verify each piece.
- **metrics/** — nil-safe counters / histograms with no Prometheus dependency,
  and a lock-free EWMA.
- **trace/** — optional OpenTelemetry hook.
- **health/** — per-blob health reports.
- **onchain/** — proof-of-storage + paid pinning + escrow client
  interface (the only sub-package that depends on an EVM escrow
  implementation).
- **internal/** — `mocktransport`, `mockstore`, `chaos` test helpers.

## When to use what

- Embed `cache.Store` for a local-only encrypted blob cache.
- Add `quota.Manager` to enforce headroom-based caps.
- Add `abuse.Guard` to enforce per-uploader / per-peer caps.
- Add `advertise.Advertiser` + `resolve.Resolver` + `fetch.Fetcher` to
  participate in a peer mesh.
- Add `replicate.Replicator` to opportunistically pull rarer pieces.
- Add `onchain.ChallengeIssuer` + `onchain.PinAcceptor` if you want
  proof-of-storage + paid pinning (depends on plutus EVM contracts).

## Use

```sh
go get github.com/bbmumford/diststore
```

```go
key, err := crypto.DeriveStorageKey(identityPriv) // the node's Ed25519 private key bytes
if err != nil {
	return err
}
backend, err := storage.NewFilesystemBackend(dir) // or storage.NewMemoryBackend()
if err != nil {
	return err
}
s, err := cache.New(cache.Config{
	Backend:    backend,
	Cipher:     crypto.Default(), // XChaCha20-Poly1305
	StorageKey: key,
})
if err != nil {
	return err
}
cid, err := s.Put(ctx, data)
data, err = s.Get(ctx, cid)
```

Sub-packages live under the same root: `diststore/cache`, `diststore/fetch`,
`diststore/onchain` and the rest above.

## Dependencies

- No other package of this estate.
- `golang.org/x/crypto` (XChaCha20-Poly1305, HKDF, NaCl box, BLAKE2b, SHA-3),
  `filippo.io/edwards25519` (Ed25519 to X25519 for the recipient wrap),
  `github.com/klauspost/reedsolomon` (erasure coding) and `golang.org/x/sys`
  (disk headroom for the quota).

## Used by

Importers in the ORBTR estate:

- `github.com/bbmumford/loom/secrets` (`crypto`): the per-recipient wrap of
  role secrets.
- `github.com/bbmumford/mercury/runtime/mesh` (`abuse`, `advertise`, `cache`,
  `crypto`, `fetch`, `onchain`, `quota`, `rarest`, `replicate`, `resolve`,
  `storage`) and `github.com/bbmumford/plutus/blob` (`fetch`, `onchain`).
- `github.com/ORBTR/agent/internal/core/storage`, `internal/core/remote` and
  `systempart`: the device agent's blob storage.
- `github.com/ORBTR/constellation/domain/storage/storage/...` (`assign`,
  `handlers`, `manifest`, `meshxport`, `plan` and others: `chunker`,
  `placement`, `fetch`, `advertise`, `resolve`, `transport`).
- `github.com/ORBTR/app.orbtr.io/wasm/aether` (`chunker`, `storepiece`,
  `streamfetch`): the browser's upload and fetch path.

## Platforms and build

Pure Go, no cgo. `quota` reads disk headroom per platform (Unix systems,
Windows, and a js/wasm build with no disk). The module's `go` directive is
1.25.0.

## Licence

MIT. Copyright (c) 2026 Brandon Miller-Mumford. The full text is in
[LICENSE](LICENSE).

## Contributing

[AGENTS.md](AGENTS.md) holds the repository's two rules, for people and coding
agents alike: no temporary documents in the repository, and conventional commit
messages that stand on their own. [CLAUDE.md](CLAUDE.md) imports it for Claude
Code.
