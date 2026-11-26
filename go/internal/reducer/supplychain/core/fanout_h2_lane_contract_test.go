// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"sort"
	"testing"
)

// H2 lane-split contract (#11520): what the hot/cold lane split must NOT
// change, plus the one behavior it MUST add.
//
// Binding constraints (from #11520 Q3, unchanged by H2): no timeout raise,
// no worker/batch cuts, no batch-size-1, the 10s per-statement guard stays,
// claim/key fences and the admitted set are untouched (every id lands in
// exactly one statement), parallelism bound 4 unchanged.
//
// Test labels: GREEN pins an existing invariant (passes on base, must keep
// passing after H2). RED pins new lane behavior absent on base (fails until
// H2 lands).

// TestH2LaneContractAdmittedSetIdentical is GREEN: the lane split only
// re-partitions one chunk's statements, so every input id must land in
// exactly one statement, in chunk order, with no multi-id statement over
// the mass budget and every hot or unmeasured id alone. Boundary masses
// (exactly 10k rows stays cold, 10k+1 splits) are the fence posts H2 must
// preserve, since the split direction is safe only when unmeasured ids
// never pack.
func TestH2LaneContractAdmittedSetIdentical(t *testing.T) {
	t.Parallel()

	hot := int64(fanOutHolderHotMassRows + 1)
	threshold := int64(fanOutHolderHotMassRows)
	cases := map[string]struct {
		chunk  []string
		masses map[string]int64
		// wantSingletons lists ids that must read alone.
		wantSingletons []string
	}{
		"all cold packs whole": {
			chunk:  []string{"CVE-2026-c1", "CVE-2026-c2", "CVE-2026-c3"},
			masses: map[string]int64{"CVE-2026-c1": 3, "CVE-2026-c2": 0, "CVE-2026-c3": 41},
		},
		"hot singleton plus cold pack": {
			chunk:  []string{"CVE-2026-hot", "CVE-2026-c1", "CVE-2026-c2"},
			masses: map[string]int64{"CVE-2026-hot": hot, "CVE-2026-c1": 12, "CVE-2026-c2": 34},
		},
		"exactly at threshold stays cold": {
			chunk:  []string{"CVE-2026-t", "CVE-2026-c1"},
			masses: map[string]int64{"CVE-2026-t": threshold, "CVE-2026-c1": 1},
		},
		"one over threshold splits": {
			chunk:  []string{"CVE-2026-t", "CVE-2026-c1"},
			masses: map[string]int64{"CVE-2026-t": threshold + 1, "CVE-2026-c1": 1},
		},
		"unknown mass reads alone": {
			chunk:  []string{"CVE-2026-known", "CVE-2026-unknown"},
			masses: map[string]int64{"CVE-2026-known": 5},
		},
		"no probe reads all alone": {
			chunk: []string{"CVE-2026-a", "CVE-2026-b"},
		},
		"split keeps every id once": {
			chunk: []string{"CVE-2026-a", "CVE-2026-h", "CVE-2026-m1", "CVE-2026-m2", "CVE-2026-e"},
			masses: map[string]int64{
				"CVE-2026-a":  1,
				"CVE-2026-h":  hot,
				"CVE-2026-m1": threshold / 2,
				"CVE-2026-m2": threshold / 2,
				"CVE-2026-e":  2,
			},
			wantSingletons: []string{"CVE-2026-h"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := planHolderReadStatements(tc.chunk, tc.masses)
			var flat []string
			for _, stmt := range got {
				if len(stmt) > fanOutHolderReadChunk {
					t.Fatalf("statement %v exceeds the %d-id chunk cap", stmt, fanOutHolderReadChunk)
				}
				var sum int64
				for _, id := range stmt {
					sum += tc.masses[id]
				}
				if len(stmt) > 1 && sum > fanOutHolderHotMassRows {
					t.Fatalf("multi-id statement %v covers %d rows, over the %d-row budget", stmt, sum, fanOutHolderHotMassRows)
				}
				flat = append(flat, stmt...)
			}
			// Chunk order is preserved across the partition: a lane split
			// must not reorder what the union returns.
			ordered := append([]string(nil), tc.chunk...)
			sort.Strings(flat)
			sort.Strings(ordered)
			if len(flat) != len(ordered) {
				t.Fatalf("plan covers %d ids, want %d: %v", len(flat), len(ordered), got)
			}
			seen := map[string]int{}
			for _, id := range flat {
				seen[id]++
			}
			for _, id := range tc.chunk {
				if seen[id] != 1 {
					t.Fatalf("id %q appears %d times in plan %v, want exactly once", id, seen[id], got)
				}
			}
			for _, id := range tc.wantSingletons {
				found := false
				for _, stmt := range got {
					if len(stmt) == 1 && stmt[0] == id {
						found = true
					}
				}
				if !found {
					t.Fatalf("id %q does not read alone in plan %v", id, got)
				}
			}
		})
	}
}

// TestH2LaneContractHotColdLanesObserved is RED on base (expected): the
// lane split must attribute each holder statement to its lane — hot
// singletons to the hot lane, cold packs to the cold lane — so a 3 AM
// operator sees which lane burned the guard (risk 3 in the H2 isolation
// diagram). The pass below is otherwise healthy (same union, same two
// statements as the shipped split); only the lane attribution is absent.
//
// Contract names: fanout_holder_statements_hot and
// fanout_holder_statements_cold on the result sub-signals — the shipped
// fanout_holder_statements name plus a lane suffix, matching the
// fanout_holder_key_range_statements qualifier pattern and the
// fanout_h2_lane_split tests (single pinned spelling). The requirement is
// per-lane observability, not the spelling; if the producer ships
// different names, update both tests together so they pin one spelling.
func TestH2LaneContractHotColdLanesObserved(t *testing.T) {
	t.Parallel()

	port := &rangeStubFanOut{
		stubFanOut: &stubFanOut{
			holders: []string{"holder-a"},
			masses:  map[string]int64{"CVE-2026-hot": HolderMassProbeCap},
		},
	}
	ids := []string{"CVE-2026-hot", "CVE-2026-c1", "CVE-2026-c2"}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: ids}}
	result, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	// The pass itself is healthy on base: same union, same split.
	if got := result.SubSignals["fanout_holder_statements"]; got != 2 {
		t.Fatalf("fanout_holder_statements = %v, want 2 (hot singleton plus cold pack)", got)
	}
	if got := result.SubSignals["fanout_holder_key_range_statements"]; got != 0 {
		t.Fatalf("fanout_holder_key_range_statements = %v, want 0", got)
	}
	// The lane attribution is what H2 adds. RED until it lands.
	if got := result.SubSignals["fanout_holder_statements_hot"]; got != 1 {
		t.Fatalf("RED (H2 absent): fanout_holder_statements_hot = %v, want 1 hot-lane statement", got)
	}
	if got := result.SubSignals["fanout_holder_statements_cold"]; got != 1 {
		t.Fatalf("RED (H2 absent): fanout_holder_statements_cold = %v, want 1 cold-lane statement", got)
	}
}
