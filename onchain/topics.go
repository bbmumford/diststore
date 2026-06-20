/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package onchain

// TopicBlobChallenge is the RequestResponse topic on which proof-of-
// storage challenges are exchanged.
const TopicBlobChallenge = "plutus.blob.challenge"

// TopicPinRequest / TopicPinAccept are the BroadcastOnly topics for
// paid pinning.
const (
	TopicPinRequest = "plutus.blob.pin.request"
	TopicPinAccept  = "plutus.blob.pin.accept"
)
