# diststore

A pluggable, content-addressed, encrypted-at-rest distributed blob store
for mesh / P2P networks.

## Layout

- **types.go / errors.go / config.go** — root types, sentinel errors,
  functional `Option`s.
- **chunker/** — Split / Reassemble + adaptive chunk sizing.
- **index/** — Merkle-root + signed piece manifest.
- **crypto/** — pluggable `Cipher` (XChaCha20-Poly1305 default,
  AES-256-GCM alternative), per-tenant KEK isolation, advertisement
  signing.
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
- **metrics/** — nil-safe counters / histograms, no Prom dep.
- **trace/** — optional OpenTelemetry hook.
- **health/** — per-blob health reports.
- **onchain/** — proof-of-storage + paid pinning + escrow client
  interface (the only sub-package that depends on an EVM escrow
  implementation).
- **proto/** — protobuf definitions for advertise + fetch.
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

## Import path

```
github.com/bbmumford/diststore
```

Sub-packages live under the same root, e.g. `diststore/cache`,
`diststore/fetch`, `diststore/onchain`.
