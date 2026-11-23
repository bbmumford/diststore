/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package onchain

import (
	"context"
	"math/big"
	"time"
)

// EscrowClient is the surface paid-pinning code needs to deploy and
// manage a PlutusStorageEscrow.sol instance. The concrete implementation
// lives in plutus/blob; tests pass a mock.
type EscrowClient interface {
	CreatePin(ctx context.Context, params CreatePinParams) (string, error)
	PostBond(ctx context.Context, escrowAddress string) (string, error)
	OpenChallenge(ctx context.Context, cid [32]byte, nonce [32]byte, expectedHash [32]byte) (string, error)
	ExpireChallenge(ctx context.Context, cid [32]byte) (string, error)
}

// CreatePinParams is the payload CreatePin receives.
type CreatePinParams struct {
	ChainID           string
	CID               string
	Seller            string
	Replicator        string
	Duration          time.Duration
	ReplicationFactor int
	FeePerReplicator  *big.Int
	BondDeadline      time.Time
}
