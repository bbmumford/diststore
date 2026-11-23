/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package replicate

import "github.com/bbmumford/diststore/abuse"

// DefaultReplicationAllowedRecordTypes mirrors the abuse package's
// list. Re-exported so consumers don't need both imports.
var DefaultReplicationAllowedRecordTypes = abuse.DefaultReplicationAllowedRecordTypes
