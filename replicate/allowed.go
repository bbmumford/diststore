/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package replicate

import "github.com/bbmumford/diststore/abuse"

// DefaultReplicationAllowedRecordTypes mirrors the abuse package's
// list. Re-exported so consumers don't need both imports.
var DefaultReplicationAllowedRecordTypes = abuse.DefaultReplicationAllowedRecordTypes
