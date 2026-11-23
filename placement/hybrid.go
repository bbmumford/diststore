/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package placement

// Default returns the recommended Policy (Hybrid with empty region/AZ).
// Callers should construct a Hybrid{} themselves once they know the
// local region / AZ.
func Default() Policy { return Hybrid{} }
