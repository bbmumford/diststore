# Migration Plan — Deduplicate Plutus's Storage Layer into `diststore`

Status: PLAN (not yet executed). Audience: a future implementing agent. This document is self-contained; it assumes only the repository state described in the attached overlap/surface/consumer analyses and re-verified against source below.

Module roots (all under `_PACKAGES/`):
- `github.com/bbmumford/plutus` → `_PACKAGES/plutus`
- `github.com/bbmumford/diststore` → `_PACKAGES/diststore`
- `github.com/bbmumford/mercury` → `_PACKAGES/mercury`
- `github.com/bbmumford/ledger` (legacy source of the extraction) → `_PACKAGES/ledger`

`go.mod` `replace` directives already point mercury and plutus at `../diststore` and `../plutus` locally. Nothing in `ORBTR/ai`, `ORBTR/io`, `_APPS`, `HSTLES`, or any other `_PACKAGES` module imports either storage tree — blast radius is confined to `mercury` + `plutus`.

**Scope correction up front (resolves the largest false premise in the prior draft):** this migration retargets the `ledger/blob/distributed/onchain` **import path** to `diststore/onchain`. It does **not** remove the `github.com/bbmumford/ledger` **module** dependency. `ledger` (root) and `ledger/cache` remain imported by live, non-test code and stay required — see §2 "Dependency retarget" and the Deferred list.

---

## 1. Goal + Target Architecture

### Goal
Collapse all content-addressed / distributed-storage plumbing that currently lives in (or is duplicated by) `plutus/blob` into `diststore`, leaving `plutus` holding only **commerce** primitives: the on-chain EVM escrow *driver*, the Solidity settlement *contracts*, fee/earning settlement stores, attestation, and wallet custody. The plutus→diststore dependency edge must be **strictly one-way and acyclic**, with `diststore` never importing any `plutus` package.

### Target architecture (end state)

```
                 mercury (top-level consumer)
                /                         \
               v                           v
           plutus  ───────────────────►  diststore
   (commerce protocol + EVM escrow        (ALL content-addressed /
    driver + Solidity contracts +          distributed storage,
    fee/earning/attestation stores +       incl. Aether transport;
    wallet custody + BTC SPV)              declares on-chain escrow
                                            interfaces, implements none)

   plutus  ──► diststore/onchain   (implements diststore's escrow interfaces)
   plutus  ──► diststore/fetch      (already live via AetherTransport → moves down)
   diststore ──► (nothing in plutus)          [INVARIANT]

   plutus/mercury ──► ledger, ledger/cache    (RETAINED — still live elsewhere)
   plutus/mercury ──► ledger/blob/distributed/onchain   (RETARGETED → diststore/onchain)
```

Ownership cut lines:

- **plutus keeps** — (a) `contracts/evm/PlutusStorageEscrow.sol` (+ the other plutus `.sol` and their interfaces/libraries) + Foundry tests; (b) the concrete `StorageEscrowClient` EVM driver (`evmsign`, keccak ABI selectors, `TxSigner`/`TxBroadcaster`/`NonceFn`/`GasPriceFn`, `EscrowRegistry`); (c) commerce stores that merely share the word "store": fee-distribution rounds, earning/attestation log, attestation model, wallet key/seed custody, BTC SPV DB.
- **diststore owns** — every content-addressed / distribution / storage package (chunker/index/crypto/storage/cache/quota/abuse + transport/advertise/resolve/fetch/rarest/replicate/repair/placement/ec + onchain proof-of-storage + pinning orchestration), **plus the newly-relocated Aether transport**, **plus the newly-declared on-chain escrow interfaces**.

The contract↔driver relationship (`PlutusStorageEscrow.sol` ↔ `diststore/onchain`) is **RUNTIME/ABI only, never a Go import edge**: ABI encoding stays entirely inside the plutus driver; `diststore/onchain` only *declares* interfaces and receives concrete impls by constructor injection.

`diststore/onchain` is an **API-compatible reorganized superset** of `ledger/blob/distributed/onchain` — not byte-identical. The legacy package's monolithic `pinning.go`/`proof.go` were redistributed into `escrow.go`, `nonce.go`, `topics.go`, `proof.go`, `proof_onchain.go`, `pinning.go`. The 4-method `EscrowClient` signature matches exactly, and every identifier the consumers actually use resolves — but the retarget is validated by an **exported-symbol-parity check** (Phase 1), not by an assumption of textual identity.

---

## 2. File-Level Action List

Legend: **MOVE** (relocate into diststore), **KEEP+REWIRE** (stays in plutus, import/assertion retargeted), **KEEP** (commerce-essential, untouched), **RETARGET-DEP** (repoint an import path; the underlying module may or may not remain required).

### `plutus/blob/` — the duplication hotspot (split-brain)

| File | Action | Destination / Change |
|---|---|---|
| `_PACKAGES/plutus/blob/transport.go` | **MOVE** | → `_PACKAGES/diststore/transport/aether/transport.go`. `AetherTransport` (implements `fetch.PeerTransport`, `BlobFetchStreamID=300`, length-prefixed JSON frames, `MaxFrameSize` 300 KiB) sits beside `transport/swarm` + `transport/whisper`. Package becomes `package aether`. `import "github.com/bbmumford/diststore/fetch"` stays (now intra-module). Local `AetherSession`/`AetherStream`/`SessionProvider` interfaces + `writeFrame`/`readFrame` travel with it. **Also rewrite the hard-coded `"plutus/blob:"` error-string prefixes** (transport.go lines 89, 120, 123, 128, 131, 139, 146, 149, 154, 158, 210 and the `ErrFrameTooLarge` message) → `"diststore/transport/aether:"` so a diststore package no longer emits plutus-provenance errors. |
| `_PACKAGES/plutus/blob/transport_test.go` | **MOVE** | → `_PACKAGES/diststore/transport/aether/transport_test.go`. Package → `aether`; its `diststore/fetch` import is already correct. Update any error-string assertions to the new prefix (none currently assert the prefix — verify during the move). |
| `_PACKAGES/plutus/blob/doc.go` | **MOVE + REWRITE** | → `_PACKAGES/diststore/transport/aether/doc.go`. It is STALE — prose names `ledger/blob/distributed` while the code imports `diststore/fetch`. Rewrite to describe the Aether binding onto diststore's fetch subprotocol. |
| `_PACKAGES/plutus/blob/storage_escrow_client.go` | **KEEP + REWIRE** | Stays in plutus (commerce). Retarget the import at **line 56** and the compile-time assertion `var _ onchain.EscrowClient = (*StorageEscrowClient)(nil)` at **line 417** from `github.com/bbmumford/ledger/blob/distributed/onchain` → `github.com/bbmumford/diststore/onchain`. `evmsign` import unchanged. Group-A asserts added trivially; Group-B methods + asserts are NEW work (§3, Phase 5). `CreatePin` remains a documented loud-failing stub until the deploy seam lands. |
| `_PACKAGES/plutus/blob/storage_escrow_client_test.go` | **KEEP + REWIRE** | Retarget the onchain import at **line 26** to `diststore/onchain`. |

After the move, `plutus/blob` contains only the escrow driver (+ its helpers `CIDHashForString` at line 548, `IsBenignChallengeExpireRevert` at line 510 — see §2 "helper dedupe" for their disposition). Add a fresh short `plutus/blob/doc.go` describing the package as the on-chain storage-escrow driver home (no phase/plan framing).

### `plutus/contracts/evm/` — commerce, stays

| File | Action | Note |
|---|---|---|
| `_PACKAGES/plutus/contracts/evm/PlutusStorageEscrow.sol` | **KEEP** | On-chain settlement half of the proof-of-storage market. |
| `_PACKAGES/plutus/contracts/evm/PlutusEscrow.sol`, `ProtocolRegistry.sol`, `interfaces/`, `libraries/` | **KEEP** | The tree also contains these commerce contracts and their interface/library subdirs; all stay in plutus. **diststore ships no `.sol`.** ABI relationship is runtime-only → no import edge, no cycle. |
| `_PACKAGES/plutus/contracts/evm/test/PlutusStorageEscrow.t.sol` | **KEEP** | Foundry suite, travels with the contract. |

### `plutus/witness/` — name false-friends, all stay

| File | Action | Note |
|---|---|---|
| `_PACKAGES/plutus/witness/persistent_distribution_store.go` | **KEEP** | `PersistentDistributionStore` persists fee-payout **rounds** (double-pay idempotency), not content. No content-addressing, no blobs. |
| `_PACKAGES/plutus/witness/in_memory_distribution_store.go` | **KEEP** | Thin exporter over `memDistributionStore`. |
| `_PACKAGES/plutus/witness/fee_distributor.go` | **KEEP** | Owns the `DistributionStore` interface (~line 298) + default `memDistributionStore` (~line 1078). The `DistributionStore` name overlap with diststore is coincidental. |
| `_PACKAGES/plutus/witness/earning_log.go` | **KEEP** | Durable earnings + attestation-ID audit log; `BuildMerkleRoot()` keccak256 sorted-pair root feeds `UnsignedBatch.AttestationLogRoot`. Commerce settlement, not blob storage. |
| `_PACKAGES/plutus/witness/attestation.go` | **KEEP** | `ChainWitnessAttestation` + Ed25519 sign/verify. In-memory primitive. |
| `_PACKAGES/plutus/witness/registry.go` | **KEEP** | Imports `ledger` + `ledger/cache` — one of the live consumers that keeps the `ledger` module required. Untouched. |

### `plutus/wallet/` + `plutus/adapter/btc/` — custody / chain reader, stay

| File | Action | Note |
|---|---|---|
| `_PACKAGES/plutus/wallet/encrypted_keyfile.go` | **KEEP** | Argon2id + XChaCha20-Poly1305 key custody. Only superficial "both encrypt bytes" overlap with `diststore/crypto` (which encrypts blob *content*). |
| `_PACKAGES/plutus/wallet/keystore.go` | **KEEP** | BIP39 seed custody. (Pre-existing cosmetic doc drift: header says 16-byte salt but code uses `saltLen=32` — out of scope; optional side-fix.) |
| `_PACKAGES/plutus/adapter/btc/neutrino_balance.go` | **KEEP** | bbolt/walletdb SPV chain DB, build-tagged `!js`. Not a blob store. |

### Dependency retarget (import path only; `ledger` module RETAINED)

| Target | Action | Note |
|---|---|---|
| `github.com/bbmumford/ledger/blob/distributed/onchain` (import path) | **RETARGET-DEP** | Repoint the four import sites (§5) → `diststore/onchain`, the live successor. After this, no code imports the `.../blob/distributed/onchain` subpackage. |
| `github.com/bbmumford/ledger` (module) + `ledger/cache` | **RETAINED** | **Do NOT remove from `plutus/go.mod` or `mercury/go.mod`.** The root `ledger` package and `ledger/cache` remain imported by live, non-test code: `plutus/witness/registry.go` (both), `plutus/sdk.go`, `plutus/wasm/js_bridge.go`, `plutus/arbitrator/registry.go`, `plutus/reputation/reputation.go` (`ledger/cache`), and `mercury/wasm/js_bridge.go` (`ledger/cache`). `go mod tidy` will keep `github.com/bbmumford/ledger v0.0.1` and its `../ledger` replace directive. Forcibly deleting the require line breaks the wasm bridge and witness registry builds in both modules. Full removal of the `ledger` module is a separate, larger re-homing effort — see Deferred. |
| Content-addressed blob subsystem / proof-of-storage engine / pinning orchestration | **already-in-diststore** | Never forked into plutus. No files to delete; recorded here so the sweep is exhaustive. |

### Optional helper dedupe (`CIDHashForString`, `IsBenignChallengeExpireRevert`)

These two helpers are defined in `plutus/blob/storage_escrow_client.go` (lines 548, 510) **and** re-implemented locally in `mercury/runtime/mesh/proof_of_storage.go` (~lines 341, 430). The dedupe is **optional**. It has exactly two internally-consistent outcomes — pick one, do not half-apply:

- **(A) Take the dedupe:** promote both helpers into `diststore/onchain` (chain-generic, pure keccak/string-match). Then repoint **both** consumers — `plutus/blob/storage_escrow_client.go` **and** `mercury/runtime/mesh/proof_of_storage.go` — to call `onchain.CIDHashForString` / `onchain.IsBenignChallengeExpireRevert`, and delete **both** local copies. Exactly one definition remains, dependency still one-way (`plutus`,`mercury` → `diststore`).
- **(B) Skip the dedupe:** leave the plutus definition in place and the mercury re-implementation in place; state the duplication is intentional (avoids a `plutus/blob` import edge from mercury). No promotion, no deletion.

The prior draft did neither cleanly (promoted + deleted only mercury's copy, leaving two definitions). Do not reproduce that state.

---

## 3. New `diststore/onchain` Interfaces

These come from the `needs-new-diststore-interface` row plus the `gaps_vs_plutus_storage` list. **All are declared in `diststore/onchain` and implemented by `plutus`'s `StorageEscrowClient`; diststore imports none of them concretely.** Place them in `_PACKAGES/diststore/onchain/escrow.go` (alongside the existing `EscrowClient`, currently the only interface there) or a sibling `escrow_ext.go`. Pure interface + value-type declarations → WASM-safe.

**Two-tier framing (this is the correctness spine of the plan):**

- **Group A — satisfied by methods that already exist on `*StorageEscrowClient`.** Retarget + a trivially-passing compile-time assert. No new behavior.
- **Group B — NO method exists on the driver today.** These require genuinely new implementation: new calldata builders and/or a new `eth_call` read path (the driver is currently write-only — it holds only signer/broadcaster/nonceFn/gasPriceFn, with no read transport). They are **not** wiring. Any Group-B method not yet backed by real ABI must return an explicit not-implemented error (loud), never a silent success — the same discipline `CreatePin` already follows. See Phase 5.

Existing (unchanged, retained):

```go
// diststore/onchain/escrow.go (already present)
type EscrowClient interface {
    CreatePin(ctx context.Context, params CreatePinParams) (string, error)   // stub: loud error until EscrowDeployer is real
    PostBond(ctx context.Context, escrowAddress string) (string, error)
    OpenChallenge(ctx context.Context, cid [32]byte, nonce [32]byte, expectedHash [32]byte) (string, error)
    ExpireChallenge(ctx context.Context, cid [32]byte) (string, error)
}
```

### Group A — interfaces the existing driver already satisfies

```go
// EscrowResponder — replicator answers a challenge ON-CHAIN, short-circuiting
// the 24h challenge-expiry window. The nonce is NOT part of respond(): it was
// bound earlier at OpenChallenge(cid, nonce, expectedHash). respond() carries
// only the precomputed response hash. Signature MUST match the existing driver
// method (storage_escrow_client.go:398) and the respond(bytes32) selector
// (selRespond = 0x1982f1d3).
type EscrowResponder interface {
    Respond(ctx context.Context, cid [32]byte, response [32]byte) (txHash string, err error)
}

// EscrowRegistry — read-only resolution of the per-CID escrow contract address.
// Method name MUST be EscrowAddressForCID to match the existing MemoryRegistry
// AND NoopRegistry (storage_escrow_client.go:110-148). Both concrete registries
// satisfy this read seam as-is.
type EscrowRegistry interface {
    EscrowAddressForCID(cid [32]byte) (address string, ok bool)
}

// EscrowRegistrar — the OPTIONAL mutation seam, satisfied ONLY by the mutable
// MemoryRegistry (Set), never by NoopRegistry. Keeping registration off the
// read seam is what lets NoopRegistry remain a valid EscrowRegistry.
type EscrowRegistrar interface {
    EscrowRegistry
    Set(cid [32]byte, address string)
}
```

Group A also includes the driver's already-real `WithdrawBond(ctx, cid [32]byte) (string, error)` (storage_escrow_client.go:378) — it is folded into `EscrowSettler` below, but note that this one method of that interface is pre-existing.

### Group B — interfaces requiring NEW implementation (Phase 5)

```go
// EscrowSettler — advances the payout lifecycle of a pin escrow.
//   WithdrawBond: EXISTS today (Group A).
//   ReleaseWeekly / RefundUnbonded: NO method today — only the selectors
//     selReleaseWeekly (0x7289c3ad) and selRefundUnbonded (0xff9a717c) exist.
//     Phase 5 must add EncodeReleaseWeekly / EncodeRefundUnbonded calldata
//     builders and the corresponding driver methods.
type EscrowSettler interface {
    ReleaseWeekly(ctx context.Context, cid [32]byte) (txHash string, err error)   // NEW: weekly fee cursor
    RefundUnbonded(ctx context.Context, cid [32]byte) (txHash string, err error)  // NEW: seller refund if never bonded
    WithdrawBond(ctx context.Context, cid [32]byte) (txHash string, err error)    // EXISTS
}

// EscrowReader — observes on-chain escrow state for health/repair/slashing
// decisions. There is NO eth_call read path on the driver today; Phase 5 must
// add a read transport (an EthCaller/ChainReader injection) plus the result
// decoders. This is substantial new EVM read code — a stub here would silently
// corrupt slashing/repair decisions, so it must fail loud until real.
type PinState uint8
const (
    PinUnknown PinState = iota
    PinActive
    PinCompleted
    PinSlashed
)
type ReplicatorStats struct {
    Replicator    string
    MissCount     uint64
    Slashed       *big.Int
    LastResponse  int64
    OpenChallenge bool
}
type EscrowReader interface {
    PinStatus(ctx context.Context, cid [32]byte) (PinState, error)
    ReplicatorStats(ctx context.Context, cid [32]byte) (ReplicatorStats, error)
    HasOpenChallenge(ctx context.Context, cid [32]byte) (bool, error)
}

// TokenApprover — authorizes the escrow contract to pull an ERC20 fee/bond
// allowance before constructor pull / bondReplicator. NO Approve method or
// calldata builder exists today; Phase 5 adds Encode/Approve.
type TokenApprover interface {
    Approve(ctx context.Context, spender string, amount *big.Int) (txHash string, err error)
}

// EscrowDeployer — materializes a new escrow contract for a (cid, replicator)
// pair (forge script or on-chain factory). Fills the CreatePin gap. Its real
// impl is blocked on a Go-side bytecode/factory path (external artifact), so it
// stays a loud not-implemented error until that lands (see Deferred).
type EscrowDeployer interface {
    Deploy(ctx context.Context, p CreatePinParams) (escrowAddress, txHash string, err error)
}
```

Optional forward-looking multi-chain seam (`PinRequest.AcceptedPaymentChains []string` already exists in diststore, but there is no adapter). Fully realizable once Group B lands:

```go
// ChainAdapter groups the EVM-shaped escrow surface for one chain. BTC/other-
// chain escrow (plutus/adapter/btc) is NOT EVM-shaped and would get its own
// adapter — keep these interfaces EVM-shaped ([32]byte cid, keccak) and route
// by chain id.
type ChainAdapter interface {
    ChainID() string
    EscrowClient
    EscrowResponder
    EscrowSettler
    EscrowReader
    TokenApprover
}
type ChainRouter interface {
    For(chain string) (ChainAdapter, bool)
}
```

**Explicitly NOT promoted into diststore:** `TxSigner`/`TxBroadcaster`/`NonceFn`/`GasPriceFn`, keccak ABI-selector encoding, `evmsign.LegacyTx`, `ApproveCallData`, and `EVMChainID` handling. These are the concrete EVM plumbing and **stay in the plutus driver**.

Compile-time asserts in plutus (all land in Phase 5, since Group-B methods must exist first for the file to compile):

```go
// plutus/blob/storage_escrow_client.go (post-Phase-5)
var (
    _ onchain.EscrowClient    = (*StorageEscrowClient)(nil) // pre-existing (line 417), retargeted in Phase 1
    _ onchain.EscrowResponder = (*StorageEscrowClient)(nil) // Group A — trivially satisfied
    _ onchain.EscrowSettler   = (*StorageEscrowClient)(nil) // Group B — gated on ReleaseWeekly/RefundUnbonded methods
    _ onchain.EscrowReader    = (*StorageEscrowClient)(nil) // Group B — gated on the new eth_call read path
    _ onchain.TokenApprover   = (*StorageEscrowClient)(nil) // Group B — gated on Approve/EncodeApprove
    _ onchain.EscrowRegistry  = (*MemoryRegistry)(nil)      // Group A — EscrowAddressForCID exists
    _ onchain.EscrowRegistry  = (NoopRegistry)(nil)         // Group A — read-only, satisfied
    _ onchain.EscrowRegistrar = (*MemoryRegistry)(nil)      // Group A — Set exists on MemoryRegistry only
)
```

`diststore/onchain.PinAcceptor` and the challenger loop are extended to *optionally* accept the new interfaces by injection (nil-safe: absent settler/reader ⇒ existing behavior). Do **not** widen the required `NewPinAcceptor(cfg, EscrowClient, pub)` signature in a way that breaks existing callers; add optional setters or a config struct field instead.

**Speculative-until-wired note:** escrow/pinning has **no production injection site today** (see §5 and Phase 5). Until Phase 5 builds that path, the Group-A/B interfaces are declared-and-implemented but driven only by tests. Treat §3 as building a reachable seam, not decorating a live one.

---

## 4. Dependency-Inversion Mechanics (breaking the cycle)

There is **no Go import cycle today**, and the plan must actively preserve that. A `plutus↔diststore` cycle appears only if BOTH of these happen together:

1. `diststore` is made to import `plutus` concretely (e.g. someone drags `StorageEscrowClient`/`evmsign` INTO `diststore/onchain`, or has diststore construct the driver itself instead of taking it by injection); **AND**
2. `plutus` is made to import `diststore` concretely for its OWN persistence (e.g. `witness/earning_log.go` or the `DistributionStore` rehomed to persist through `diststore/cache`).

Either edge alone is acyclic; together they close the loop. The specific trap is a naive "consolidate ALL storage into diststore" that drags the escrow driver + `.sol` down with the blob transport — that single move inverts the graph.

Mechanics that keep the edge one-way:

1. **Interface in diststore, impl in plutus.** `diststore/onchain` *declares* `EscrowClient` and the new interfaces (§3). `plutus`'s `StorageEscrowClient` *implements* them. diststore source never names a plutus type.
2. **Construction-time injection only.** diststore consumes the concrete driver exactly as `onchain.PinAcceptor` already does — `NewPinAcceptor(cfg, escrow, pub)` holds an `EscrowClient` and calls `escrow.CreatePin` / `escrow.PostBond` before signing a `PinAcceptance`. New settler/reader/responder driving follows the same injection pattern. No package-level edge from diststore to plutus is ever introduced.
3. **Contract↔driver is ABI, not import.** `PlutusStorageEscrow.sol` interacts with `diststore/onchain` only through calldata/ABI at runtime. ABI encoding lives entirely in the plutus driver.
4. **Transport moves DOWN, not sideways.** The Aether transport is generic content-piece plumbing with zero commerce content; relocating it to `diststore/transport/aether` (which imports only `diststore/fetch`) keeps diststore self-contained and removes a plutus→diststore *concrete-package* dependency that was on the "storage plumbing lives in a commerce package" wrong side.
5. **Plutus-owned persistence stays injected.** If `witness` attestation/earning persistence ever needs distributed durability, plutus depends on a diststore **interface** (e.g. `cache.BlobStore`, or a narrow `AttestationSink`) passed in at construction — never an edge that lets diststore reach back into plutus.

Resulting graph: `mercury → {plutus, diststore}`; `plutus → diststore` (implements diststore's escrow interfaces, consumes `diststore/fetch`); `diststore → nothing in plutus`; `plutus`,`mercury` → `ledger`/`ledger/cache` (retained); `ledger/blob/distributed/onchain` import → retargeted away. Acyclic, one-way.

A CI guard enforces invariant #1/#5 mechanically (see §8): fail the build if `diststore` acquires any import matching `github.com/bbmumford/plutus`.

---

## 5. Consumer Migration

Each importer of the old plutus storage surface, with its exact change. Prefer the direct repoint (end-state form) over a long-lived shim; a re-export shim is only a transitional aid if landing across separate merges.

| Importer | Current import / usage | Action |
|---|---|---|
| `_PACKAGES/mercury/runtime/mesh/replicator.go` | `plutusblob.NewAetherTransport(...)` (line ~140); comment refs `plutusblob.MaxFrameSize` (line ~236). Also imports `diststore/{abuse,advertise,cache,crypto,fetch,quota,rarest,replicate,resolve,storage}`. Does **not** import `github.com/ORBTR/aether`. | Repoint the alias `plutusblob "github.com/bbmumford/plutus/blob"` → `aether "github.com/bbmumford/diststore/transport/aether"`; `aether.NewAetherTransport(...)`, `aether.MaxFrameSize`. No name collision here — bare `aether` is safe. No behavior change. |
| `_PACKAGES/mercury/runtime/mesh/blob_session.go` | Implements `blob.SessionProvider` / `blob.AetherSession` / `blob.AetherStream`; uses `blob.StreamConfig`. **Already imports `github.com/ORBTR/aether` under package name `aether` (line 10).** | **Package-name collision:** the moved diststore transport is also `package aether`. Importing it as bare `aether` in this file is a duplicate-import compile error. Import it with a distinct alias — `dsaether "github.com/bbmumford/diststore/transport/aether"` — and use `dsaether.SessionProvider` / `dsaether.AetherSession` / `dsaether.AetherStream` / `dsaether.StreamConfig`. The `minervaSessionProvider` / `minervaAetherSession` impl bodies are unchanged. |
| `_PACKAGES/plutus/blob/storage_escrow_client.go` | Imports `ledger/blob/distributed/onchain` (line 56); assert (line 417). | Retarget import → `diststore/onchain`. Add Group-A asserts + (Phase 5) Group-B methods & asserts (§3). `evmsign` stays. |
| `_PACKAGES/plutus/blob/storage_escrow_client_test.go` | Imports `ledger/blob/distributed/onchain` (line 26). | Retarget → `diststore/onchain`. |
| `_PACKAGES/mercury/runtime/mesh/config.go` | `mesh.Config.EscrowClient` typed as `onchain.EscrowClient` from `ledger/blob/distributed/onchain`; comments name `*plutus/blob.StorageEscrowClient` / `*plutus/blob.MemoryRegistry`. **Interface seam only — no `plutus/blob` import edge.** | Retarget the `onchain` type import → `diststore/onchain`. No shim needed (injected by interface). Update comments to reference the diststore interfaces. |
| `_PACKAGES/mercury/runtime/mesh/proof_of_storage.go` (+`_test.go`) | Imports `ledger/blob/distributed/onchain`; locally re-implements `CIDHashForString` (~line 341) and `IsBenignChallengeExpireRevert` (~line 430). | Retarget the `onchain` import → `diststore/onchain`. Helper-dedupe is **optional** and must be all-or-nothing per §2 outcome (A) or (B). |
| `_PACKAGES/plutus/sdk.go` | `DistributionStore *witness.PersistentDistributionStore`; `witness.NewPersistentDistributionStore`; `witness.NewInMemoryDistributionStore`; `FeeDistributorConfig.Store` = `witness.DistributionStore`. Also imports `ledger`/`ledger/cache`. | **No change** to storage wiring. The distribution store stays in `plutus/witness` (commerce false-friend); the `ledger` imports are retained (§2). Listed to confirm deliberate no-op. |
| `_PACKAGES/mercury/runtime/mesh/runtime.go` | `diststore/{abuse,advertise,cache,fetch,quota,rarest,replicate,resolve}`. | **No change** — none of these packages move. |

**Concrete escrow wiring — currently absent in production.** `mesh.Config.EscrowClient` is populated ONLY in `mercury/runtime/mesh/proof_of_storage_test.go` (test fakes); `NewStorageEscrowClient` is called ONLY in `plutus/blob/storage_escrow_client_test.go`; `onchain.NewPinAcceptor` is called ONLY in the legacy onchain package's own tests. `cmd/mercury-node/main.go` references none of `EscrowClient`/`StorageEscrowClient`/`PinAcceptor`. **There is no production injection site today.** Phase 5 therefore does not "extend an existing seam" — it **builds** the injection path: construct `plutus/blob.StorageEscrowClient` (with its signer/broadcaster/registry and the new read transport), populate `mesh.Config.EscrowClient`, and construct/wire `diststore/onchain.PinAcceptor` + the challenger in the mercury node bootstrap (`cmd/mercury-node/main.go`, creating the wiring where absent). This is the single intended `plutus`-concrete → `diststore`-interface injection site, and it is new code, not a repoint.

Transitional shim option (only if the transport move and mercury repoint land in separate merges): leave `plutus/blob` with a thin re-export —

```go
package blob
import aether "github.com/bbmumford/diststore/transport/aether"
type AetherTransport   = aether.AetherTransport
type SessionProvider   = aether.SessionProvider
type AetherSession     = aether.AetherSession
type AetherStream      = aether.AetherStream
type StreamConfig      = aether.StreamConfig
var  NewAetherTransport = aether.NewAetherTransport
const MaxFrameSize      = aether.MaxFrameSize
```

Delete the shim once mercury is repointed (end-state = no back-compat scaffolding).

`go.mod` changes: **none for the `ledger` module** — it stays required by the live `ledger`/`ledger/cache` consumers listed in §2. `../diststore` and `../plutus` replace directives are already present; add no new replace for the transport move (it is intra-`diststore`). `go mod tidy` after the retarget must continue to list `github.com/bbmumford/ledger`; it should show the `.../blob/distributed/onchain` subpackage no longer in the import graph while `ledger` and `ledger/cache` remain.

---

## 6. WASM / Build-Tag Considerations

Both trees ship browser `js/wasm` builds; the move must not break either.

- **Aether transport is WASM-safe and must stay so.** `transport.go` uses only local interfaces (`AetherSession`/`AetherStream`/`SessionProvider`), JSON framing, and `diststore/fetch` — no `os`, no `syscall`, no non-wasm imports. Landing it in `diststore/transport/aether` keeps it beside the already-wasm-safe `transport/swarm` + `transport/whisper`. Add no `os`-touching code; do not introduce a build tag. The error-string prefix rewrite (§2) is cosmetic and WASM-neutral.
- **`diststore/onchain` stays pure-Go / WASM-safe.** The new interface + value-type declarations (§3) import only `context`, `math/big`, `time` — all WASM-safe. Do not pull any EVM/`evmsign` code into `diststore/onchain`. The new `eth_call` read path for `EscrowReader` lives in the **plutus** driver, not here.
- **Concrete EVM driver stays in plutus.** `StorageEscrowClient` + `evmsign` + the new read transport remain in `plutus/blob` — outside diststore's WASM surface. If plutus itself has a wasm build that excludes the driver, preserve whatever tag currently gates it; the retarget changes only the *interface* import, not the tag.
- **`storage.FilesystemBackend`** remains non-functional in browser wasm (documented; use `MemoryBackend`). No change — no storage file moves.
- **`quota`** keeps its `js/wasm` `navigator.storage.estimate` statter — unaffected.
- **BTC SPV DB** (`adapter/btc/neutrino_balance.go`) keeps its `!js` build tag; stays in plutus.
- The retained `ledger`/`ledger/cache` imports include the `wasm/js_bridge.go` files in both plutus and mercury — do not disturb their build tags.
- After the move, run `GOOS=js GOARCH=wasm go build ./...` in **both** modules to confirm neither the new `diststore/transport/aether` nor the retargeted `diststore/onchain` regresses the wasm build.

---

## 7. Security-Preserving Checks (must not regress)

- **Blob content encryption** — `diststore/crypto` (XChaCha20-Poly1305 default, CID-derived nonce, CID-as-AAD, per-tenant KEK, per-recipient NaCl/P-256 wrap, fail-closed `EntitledAllowAll` gate) does not move and is not touched. Confirm `diststore/crypto` tests stay green; the Aether transport carries ciphertext only and adds no plaintext path.
- **Wallet key/seed custody** — `wallet/encrypted_keyfile.go` (Argon2id + XChaCha20-Poly1305, `O_EXCL 0600`, atomic `RewrapKeyfile`) and `wallet/keystore.go` stay in plutus, unchanged. Verify KDF/AEAD parameters are byte-for-byte unchanged (no accidental salt/nonce length edits during the sweep).
- **Attestation integrity** — `witness/earning_log.go` `BuildMerkleRoot()` (keccak256 OpenZeppelin sorted-pair over `AttestationIDs` → `UnsignedBatch.AttestationLogRoot`) and `witness/attestation.go` Ed25519 sign/verify stay intra-plutus, untouched. Guard with a golden-vector test: same inputs ⇒ identical Merkle root and identical attestation hash before/after the migration.
- **Escrow / fee / slash enforcement** — the `EscrowClient` 4-method surface is signature-compatible between `ledger/blob/distributed/onchain` and `diststore/onchain`; the retarget must preserve it exactly (the assert at line 417 must pass against `diststore/onchain`). The new Group-B interfaces are **new implementations**, not wiring: `EscrowSettler.ReleaseWeekly`/`RefundUnbonded`, `EscrowReader.*`, and `TokenApprover.Approve` require real calldata builders / an `eth_call` read path and must **fail loud** (explicit not-implemented error) until real — a silent-success stub in `EscrowReader` would corrupt the slashing/repair decisions it feeds. `CreatePin`/`EscrowDeployer` likewise stay loud-failing until the deploy seam is real. The `.sol` state machine (`releaseWeekly`/`refundUnbonded`/`withdrawBond`/slash) is the source of truth; the driver never invents a path that skips `PostBond`/`bondReplicator`.
- **Double-pay idempotency** — the `DistributionStore` round dedup (`PersistentDistributionStore` JSON-lines + fsync + replay) stays in `plutus/witness`; do not let the diststore `DistributionStore` name-collision cause an accidental rehoming that drops idempotency. Add an assertion in review that `sdk.go` still injects the plutus store.
- **Nonce / anti-replay** — the on-chain `OpenChallenge(cid, nonce, expectedHash)` binds the nonce **at challenge-open time**; the contract's `respond(bytes32)` accepts only the precomputed response hash and does not re-take the nonce, so `EscrowResponder.Respond` correctly carries `(cid, response)` and no nonce. Preserve the off-chain `NewNonce` / `ComputeProofHash` (BLAKE2b) vs on-chain `ComputeOnChainExpectedHash` (keccak256) split; a `Respond` that tried to carry a nonce would mis-shape the `respond(bytes32)` calldata and break settlement.
- **One-way-edge invariant as a security property** — a diststore→plutus edge would let generic storage code reach commerce/escrow code; the CI import guard (§8) is a security control, not just hygiene.

---

## 8. Test + Verification Strategy

Windows/OneDrive note: the go-build cache corrupts mid-link on this host (AV/sync). Use an isolated `GOCACHE` under `%TEMP%`, `-vet=off` for the corrupting steps, and run long builds in the background; run `go vet` separately once the cache is warm. (Recorded operational hazard, not a code change.)

Exported-symbol-parity gate (Phase 1 — replaces the false "byte-identical" assumption):
- Prove every identifier the consumers reference resolves under `diststore/onchain`, not just the 4-method `EscrowClient`. Consumed set to verify: `ChallengeResult`, `ChallengeIssuer`, `NewChallengeIssuerWithPieceSource`, `ChallengeResponder`, `NewNonce`, `ComputeOnChainExpectedHash`, `ChallengeTransport`, `PieceSource`, `TopicBlobChallenge` / `TopicPinRequest` / `TopicPinAccept`, `CreatePinParams`, plus the `EscrowClient` methods. Easiest proof: compile `plutus` + `mercury` against `diststore/onchain` (a green build IS the parity proof); optionally diff the exported symbol sets of the two packages to catch any consumer added later.

Per-package green (unit):
- `diststore/transport/aether` — moved `transport_test.go` passes in its new home (`package aether`); framing round-trip (`writeFrame`/`readFrame`, `MaxFrameSize` boundary) intact; error strings now carry the `diststore/transport/aether:` prefix.
- `diststore/onchain` — builds with the new interfaces; existing proof-of-storage + pinning tests green; any Group-B interface exercised in tests uses a mock/impl in a test-only file (diststore ships no concrete EVM impl).
- `plutus/blob` — builds with the retargeted `diststore/onchain` import; `storage_escrow_client_test.go` passes; Group-A asserts (`EscrowResponder`, `EscrowRegistry`/`EscrowRegistrar`) resolve; Group-B asserts (`EscrowSettler`/`EscrowReader`/`TokenApprover`) resolve **only after** their real methods land in Phase 5, with loud not-implemented behavior verified where a real ABI path is not yet available.
- `plutus/witness`, `plutus/wallet`, `plutus/adapter/btc` — unchanged; run to confirm no collateral breakage. Golden-vector tests for Merkle root + attestation hash + wallet KDF (see §7).

Cross-module build:
- `go build ./...` green in `diststore`, `plutus`, `mercury` (native).
- `GOOS=js GOARCH=wasm go build ./...` green in `diststore` and `plutus` (§6).
- `go mod tidy` in `plutus` and `mercury` leaves `github.com/bbmumford/ledger` **present** (still used by `ledger`/`ledger/cache` consumers) and shows the `ledger/blob/distributed/onchain` subpackage gone from the import graph. Verify via `go list -deps ./...`: `github.com/bbmumford/ledger` and `github.com/bbmumford/ledger/cache` present; `github.com/bbmumford/ledger/blob/distributed/onchain` absent.

Integration (the Mercury consumer):
- `mercury/runtime/mesh` compiles and its tests pass with `replicator.go`/`blob_session.go` repointed to `diststore/transport/aether` (bare `aether` in replicator.go, `dsaether` alias in blob_session.go) and `config.go`/`proof_of_storage.go` repointed to `diststore/onchain`.
- End-to-end mesh smoke: build an `AetherTransport` from `diststore/transport/aether`, drive one `FetchRequest`/`FetchResponse` over a `minervaAetherSession`, assert CID-verified payload — same behavior as pre-move.
- Escrow injection (Phase 5): the newly-built `cmd/mercury-node/main.go` wiring constructs the driver, populates `mesh.Config.EscrowClient`, and drives `PinAcceptor`/challenger. Assert the challenger issues/answers challenges against `diststore/onchain`; assert `EscrowReader`-fed slashing/repair uses real on-chain state (not a stub) before treating any miss as slashable.
- If the helper dedupe (§2 outcome A) is taken, assert `CIDHashForString`/`IsBenignChallengeExpireRevert` produce identical results to the removed copies (golden vectors before deleting them) in **both** plutus and mercury.

Invariant enforcement (CI guard):
- Fail the build if `diststore` imports any `github.com/bbmumford/plutus/...` path — e.g. a `go list -deps ./... | grep plutus` gate in diststore's CI. This mechanically prevents the cycle-closing edge for all future changes.

Adversarial audit: after the ≥3-file build lands, run a sub-agent audit of the diff against this plan and the surface specs; fix every finding; confirm no bootstrap stub was left where an end-state impl was intended (except the deliberately-documented `CreatePin`/`EscrowDeployer` gap and any Group-B method still loud-failing pending a real ABI path).

---

## 9. Dependency-Ordered Phases (ordering only)

Phases express order, not schedule. Each phase is independently verifiable and leaves all three modules building green.

**Phase 0 — Baseline & preservation.** Confirm the three modules build green at HEAD. Back up any uncommitted working-tree WIP before large moves (assume uncommitted WIP exists). Confirm `../diststore`/`../plutus` replace directives. Enumerate and record the live `ledger`/`ledger/cache` consumers (§2) so the retarget does not disturb them. Record golden vectors for Merkle root, attestation hash, wallet KDF, and the two `proof_of_storage` helpers. No code change.

**Phase 1 — Retarget the onchain import + prove symbol parity (unblocks everything).** Repoint the four sites from `ledger/blob/distributed/onchain` → `diststore/onchain`: `plutus/blob/storage_escrow_client.go` (line 56 import, line 417 assert) + `_test.go` (line 26); `mercury/runtime/mesh/config.go` (type seam); `mercury/runtime/mesh/proof_of_storage.go` (+`_test.go`). Run the exported-symbol-parity gate (§8) — a green `plutus`+`mercury` build against `diststore/onchain` is the proof. **Do not touch the `ledger` module require line** — it stays. Verify: both modules build; escrow assert passes against diststore; `ledger`/`ledger/cache` still resolve.

**Phase 2 — Declare new diststore/onchain interfaces (additive).** Add `EscrowResponder`, `EscrowRegistry`/`EscrowRegistrar`, `EscrowSettler`, `EscrowReader`, `TokenApprover`, `EscrowDeployer` + `PinState`/`ReplicatorStats` (§3) in `diststore/onchain`. No consumer break (purely additive; no plutus edits here). Optionally extend `PinAcceptor`/challenger to accept them nil-safely. Verify: diststore builds native + wasm; existing onchain tests green.

**Phase 3 — Move the Aether transport into diststore.** Relocate `transport.go` + `transport_test.go` + rewritten `doc.go` to `diststore/transport/aether` (`package aether`, `diststore/fetch` import). Rewrite the `plutus/blob:` error-string prefixes → `diststore/transport/aether:` (and `ErrFrameTooLarge`'s message). Verify: diststore builds native + wasm; new package tests green. (Depends only on Phase 0.)

**Phase 4 — Migrate mercury transport consumers.** Repoint `replicator.go` (bare `aether`) and `blob_session.go` (`dsaether` alias — package-name collision with the pre-existing `github.com/ORBTR/aether`). Delete the transitional `plutus/blob` re-export shim if one was used. Verify: `mercury/runtime/mesh` builds + tests green; mesh fetch smoke test passes. (Depends on Phase 3.)

**Phase 5 — Implement the Group-B driver methods + build the production escrow-injection path.** In `plutus`: add the Group-A asserts (trivially satisfied) and implement the Group-B surface — `EncodeReleaseWeekly`/`EncodeRefundUnbonded` + `ReleaseWeekly`/`RefundUnbonded`; an `eth_call` read transport + `PinStatus`/`ReplicatorStats`/`HasOpenChallenge` decoders; `EncodeApprove` + `Approve`; then add the Group-B asserts. Any sub-capability not yet backed by real ABI (notably `EscrowDeployer`/`CreatePin`, blocked on a forge/factory bytecode path) returns an explicit not-implemented error — never a silent success. **Build the injection path from scratch** (no production site exists today): construct `StorageEscrowClient` (signer/broadcaster/registry + read transport), populate `mesh.Config.EscrowClient`, and wire `diststore/onchain.PinAcceptor` + the challenger in the mercury node bootstrap (`cmd/mercury-node/main.go`). Verify: injection compiles and runs; escrow-fee/slash enforcement smoke tests pass against real on-chain state (not a stub). This is substantial new implementation, not low-risk wiring. (Depends on Phases 1 + 2.)

**Phase 6 — Optional helper dedupe.** Either take §2 outcome (A) — promote `CIDHashForString`/`IsBenignChallengeExpireRevert` into `diststore/onchain` and repoint **both** plutus and mercury, deleting both local copies (golden-vector-checked) — or explicitly record outcome (B), keeping the duplication intentional. **No `go.mod` change** (the `ledger` module is retained; there is no dependency to drop here). Verify: whichever outcome, exactly the intended number of definitions remains and all three modules build. (Dedupe depends on Phase 5's mercury retarget.)

**Phase 7 — Full verification + invariant lock.** Run the full suite across all three modules (native + wasm), the mesh integration smoke, the Phase-5 escrow-injection smoke, and the CI import guard (`diststore` imports zero `plutus`). Confirm `go list -deps` shows `ledger`/`ledger/cache` present and `ledger/blob/distributed/onchain` absent. Run the adversarial audit against this plan. Confirm the acyclic one-way graph. (Depends on all prior phases.)

Ordering summary: `0 → 1 → {2, 3}` (2 and 3 independent) `→ 4` (needs 3) and `→ 5` (needs 1+2) `→ 6` (needs 5) `→ 7` (needs all).

---

## 10. Rollback + Risk Notes

Rollback:
- Every phase leaves all three modules green, so rollback is per-phase revert of that phase's diff. No data migration, no schema change, no on-chain state change is involved — this is a source/import reorganization plus additive interface declarations plus (Phase 5) new driver methods and injection wiring.
- **Phase 1** is the highest-value / lowest-risk step (signature-compatible retarget, parity-proven); if a later phase must be abandoned, Phase 1 can stand alone as a net improvement (kills the split-brain) without Phases 3–6.
- **Phase 3/4** (transport move) can be reverted independently of the escrow work; the transitional shim exists precisely so Phase 3 can land before Phase 4 and be rolled back without touching mercury.
- **No `go.mod` revert is ever needed for the `ledger` module** — it is never removed. Phases 1–6 involve no `require`-line change for `ledger`, so none can be broken by reverting them.

Risks:
- **Cycle inversion (highest).** The dominant risk is a well-meaning "consolidate ALL storage into diststore" that drags `StorageEscrowClient`/`evmsign`/`.sol` down with the transport. Mitigation: the §8 CI import guard + explicit review checklist that the escrow driver and `.sol` never leave plutus, and diststore never constructs the driver itself.
- **Group-B stub masquerading as done.** `EscrowReader`/`EscrowSettler(ReleaseWeekly,RefundUnbonded)`/`TokenApprover`/`EscrowDeployer` have no method on the driver today and require real calldata builders / an `eth_call` read path. Risk: a silent-success stub satisfies the compile-time assert but corrupts slashing/repair/settlement. Mitigation: loud not-implemented errors until real (CreatePin discipline); Phase 5 gated on real on-chain-state-verified behavior; adversarial audit checks for stubs.
- **Package-name collision.** The moved `package aether` collides with the pre-existing `github.com/ORBTR/aether` in `blob_session.go`. Risk: duplicate-import compile error if imported bare. Mitigation: `dsaether` alias in `blob_session.go` (called out in §5/§6); `replicator.go` has no collision.
- **False-friend rehoming.** `witness/*DistributionStore*`, `earning_log`, `attestation`, wallet crypto, BTC SPV share the word "store"/"encrypt" but are commerce, not content. Risk: an over-eager sweep moves one into diststore, closing the cycle from the plutus side. Mitigation: §2 marks each KEEP with rationale; the `sdk.go` injection assertion in review.
- **WASM regression.** Introducing an `os`/`syscall` import into `diststore/transport/aether` or `diststore/onchain` (or dragging the new `eth_call` read path into `diststore/onchain`) breaks the browser build silently on native-only CI. Mitigation: keep the read path in the plutus driver; explicit `GOOS=js GOARCH=wasm` build in Phases 2/3 and 7.
- **Security regression via parameter drift.** An accidental edit to KDF/AEAD params, Merkle-root construction, the escrow selector set, or the `respond(bytes32)` calldata shape during the mechanical sweep. Mitigation: golden-vector tests captured in Phase 0 and re-run in Phase 7; the corrected 2-arg `Respond` signature preserves the `respond(bytes32)` calldata.
- **Helper-dedupe behavioral drift / half-application.** Promoting the two helpers must be a byte-for-byte behavioral no-op AND must repoint both consumers (not just mercury). Mitigation: golden vectors before deleting copies; §2 makes it strictly outcome (A) or (B), never a mixed state.
- **Windows go-build cache corruption** (operational, not code). Mitigation: isolated `%TEMP%` `GOCACHE`, `-vet=off` on the corrupting step, background long builds.

---

## Deferred / out-of-scope (deliberate)

- **Full removal of the `github.com/bbmumford/ledger` module.** Out of scope. It requires re-homing the root `ledger` package and `ledger/cache` away from their live consumers — `plutus/witness/registry.go`, `plutus/sdk.go`, `plutus/wasm/js_bridge.go`, `plutus/arbitrator/registry.go`, `plutus/reputation/reputation.go`, and `mercury/wasm/js_bridge.go` — which is a separate, larger effort unrelated to storage dedupe. This plan retargets only the `.../blob/distributed/onchain` import path.
- **Helper dedupe (§2).** Optional; either fully applied (outcome A) or explicitly declined (outcome B). Not required for the core migration.
- **`EscrowDeployer` / `CreatePin` real deployment.** Blocked on a Go-side bytecode/factory deployment path (currently forge-based onboarding). Stays a loud not-implemented error until that external artifact exists; documented in the driver's `doc.go`.