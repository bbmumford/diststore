/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package placement

import "testing"

func TestHybrid_PrefersLocality(t *testing.T) {
	h := Hybrid{LocalRegion: "syd"}
	picks, err := h.Select([]PeerHint{
		{NodeID: "a", Region: "iad", Reliability: 1.0},
		{NodeID: "b", Region: "syd", Reliability: 0.5},
	}, 1)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if picks[0].NodeID != "b" {
		t.Errorf("expected local-region b first, got %s", picks[0].NodeID)
	}
}

func TestLoad_PicksLeastLoaded(t *testing.T) {
	picks, err := Load{}.Select([]PeerHint{
		{NodeID: "a", FreeBytes: 1},
		{NodeID: "b", FreeBytes: 100},
		{NodeID: "c", FreeBytes: 50},
	}, 2)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if picks[0].NodeID != "b" || picks[1].NodeID != "c" {
		t.Errorf("expected [b,c], got %+v", picks)
	}
}
