/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
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
