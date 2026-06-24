// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"encoding/json"
	"testing"
)

// TestSnapshotRoundTripReloads proves a Store survives a JSON round-trip:
// versions are preserved, and re-upserting the same effects on the reloaded
// Store recomputes nothing, which proves the reloaded state matches what a fresh
// computation would produce.
func TestSnapshotRoundTripReloads(t *testing.T) {
	t.Parallel()

	original := NewStore()
	original.Upsert(chainFixture())

	data, err := json.Marshal(original.Snapshot())
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	reloaded := Load(snap)

	for _, fn := range original.IDs() {
		want, _ := original.Version(fn)
		got, ok := reloaded.Version(fn)
		if !ok || got != want {
			t.Fatalf("reloaded version for %s = %q (ok=%v), want %q", fn, got, ok, want)
		}
	}

	// The reloaded store is consistent: re-upserting identical effects is a no-op.
	if recomputed := reloaded.Upsert(chainFixture()); len(recomputed) != 0 {
		t.Fatalf("reloaded store recomputed %v on unchanged effects, want none", recomputed)
	}

	// And an incremental change on the reloaded store still recomposes correctly.
	recomputed := reloaded.Upsert(map[FunctionID]Effects{
		id("C"): {ParamToReturn: []int{0}, ParamToSink: []ParamSink{{Param: 0, SinkKind: "sql"}}},
	})
	if len(recomputed) != 3 {
		t.Fatalf("reloaded incremental recompute = %v, want 3 (A,B,C)", recomputed)
	}
}
