# diststore/onchain

This is the only diststore sub-package that touches on-chain
primitives (proof-of-storage challenges, EVM-bound hashing, paid
pinning + escrow).

The `EscrowClient` interface defined here is implemented in the
`plutus` package — `diststore/onchain` declares only the surface so
the proof + pin runtime stays decoupled from any specific chain
adapter.

## Interfaces

Every chain and network effect reaches this package through an interface the caller supplies at construction:

| Interface | Methods | Supplied by | Taken by |
|---|---|---|---|
| `EscrowClient` | `CreatePin`, `PostBond`, `OpenChallenge`, `ExpireChallenge` | the client of a deployed `PlutusStorageEscrow` contract (plutus's `blob` package; tests pass a mock) | `NewPinAcceptor` |
| `PinPublisher` | `Publish(topic, payload)` | a gossip engine; whisper's `Engine.Publish` matches it | `NewPinner`, `NewPinAcceptor` |
| `PinAcceptanceSubscriber` | `SubscribeAcceptances`, `UnsubscribeAcceptances` | the seller's subscription to replicators' signed acceptances | `NewPinner` |
| `ChallengeTransport` | `Ask(ctx, peerID, payload)` | a per-peer request and reply transport | `NewChallengeIssuer`, `NewChallengeIssuerWithPieceSource` |
| `PieceSource` | `Get`, `Has` | the local content store | `NewChallengeIssuerWithPieceSource`, `NewChallengeResponder` |

## Dependency rule

diststore imports nothing of plutus. The interfaces are declared here, the chain-specific code that satisfies them lives in plutus, and the two meet only by injection at construction; the escrow contract and this package meet only through ABI-encoded calldata. plutus imports diststore (`diststore/fetch`), so an import of plutus from any diststore package would close an import cycle.
