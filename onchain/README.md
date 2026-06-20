# diststore/onchain

This is the only diststore sub-package that touches on-chain
primitives (proof-of-storage challenges, EVM-bound hashing, paid
pinning + escrow).

The `EscrowClient` interface defined here is implemented in the
`plutus` package — `diststore/onchain` declares only the surface so
the proof + pin runtime stays decoupled from any specific chain
adapter.
