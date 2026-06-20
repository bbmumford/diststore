/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

// Package chaos provides a small harness for property-style tests that
// simulate node failures, network partitions, and bit-flip corruption.
//
// Pair with the mocktransport / mockstore fakes in internal/ to drive
// the full advertiser -> resolver -> fetcher -> replicator stack
// under each scenario.
package chaos

// Scenario enumerates the failure modes the harness can apply to a
// stack-under-test.
type Scenario int

const (
	// ScenarioNormal disables all chaos injection.
	ScenarioNormal Scenario = iota
	// ScenarioDrop randomly drops messages or storage ops.
	ScenarioDrop
	// ScenarioPartition splits the peer set into non-communicating
	// halves.
	ScenarioPartition
	// ScenarioCorrupt flips bits in returned payloads to exercise the
	// fetch verifier + cache anti-tamper sweep.
	ScenarioCorrupt
)

// String returns the scenario's name.
func (s Scenario) String() string {
	switch s {
	case ScenarioNormal:
		return "normal"
	case ScenarioDrop:
		return "drop"
	case ScenarioPartition:
		return "partition"
	case ScenarioCorrupt:
		return "corrupt"
	default:
		return "unknown"
	}
}
