/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package onchain

import (
	"crypto/rand"
	"fmt"
)

// ChallengeNonceSize is the length of the random nonce in bytes.
const ChallengeNonceSize = 32

// NewNonce returns ChallengeNonceSize cryptographically random bytes.
func NewNonce() ([]byte, error) {
	n := make([]byte, ChallengeNonceSize)
	if _, err := rand.Read(n); err != nil {
		return nil, fmt.Errorf("onchain: nonce generation: %w", err)
	}
	return n, nil
}
