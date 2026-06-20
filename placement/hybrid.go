/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

package placement

// Default returns the recommended Policy (Hybrid with empty region/AZ).
// Callers should construct a Hybrid{} themselves once they know the
// local region / AZ.
func Default() Policy { return Hybrid{} }
