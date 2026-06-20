/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

//go:build !js

package quota

// defaultStatter delegates to the OS-specific implementation in
// quota_unix.go / quota_windows.go.
type defaultStatter struct{}

func (defaultStatter) DiskStats(path string) (int64, int64, error) {
	return platformDiskStats(path)
}
