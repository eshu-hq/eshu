// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"testing"
)

// TestCappedMassesDecideLikeRawMasses is the #10681 decision-equivalence
// property: capping a per-id mass at HolderMassProbeCap must never change the
// split plan. The cap sits one row over the hot threshold, so every raw mass
// at or under the threshold reports exactly (cold packs the same way) and
// every raw mass over the threshold still reports hot (a capped hot id still
// runs alone). It fails until the probe cap exists and stays single-sourced
// from fanOutHolderHotMassRows.
func TestCappedMassesDecideLikeRawMasses(t *testing.T) {
	t.Parallel()

	capMass := func(m int64) int64 {
		if m > HolderMassProbeCap {
			return HolderMassProbeCap
		}
		return m
	}
	threshold := int64(fanOutHolderHotMassRows)
	cases := []struct {
		name string
		raw  map[string]int64
	}{
		{"zero", map[string]int64{"id-a": 0}},
		{"at threshold", map[string]int64{"id-a": threshold}},
		{"just hot", map[string]int64{"id-a": threshold + 1}},
		{"large", map[string]int64{"id-a": 1 << 40}},
		{"overlap double-count at threshold", map[string]int64{"id-a": 2 * (threshold / 2)}},
		{"overlap double-count over threshold", map[string]int64{"id-a": 2 * (threshold/2 + 1)}},
		{"tombstone-only", map[string]int64{"id-a": 0}},
		{"unknown id", map[string]int64{}},
		{
			"mixed chunk",
			map[string]int64{
				"id-hot":   threshold + 1,
				"id-cold":  12,
				"id-zero":  0,
				"id-huge":  1 << 40,
				"id-bound": threshold,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			chunk := []string{"id-a"}
			if tc.name == "mixed chunk" {
				chunk = []string{"id-hot", "id-cold", "id-zero", "id-huge", "id-bound", "id-unknown"}
			}
			capped := make(map[string]int64, len(tc.raw))
			for id, m := range tc.raw {
				capped[id] = capMass(m)
			}
			rawPlan := planHolderReadStatements(chunk, tc.raw)
			cappedPlan := planHolderReadStatements(chunk, capped)
			if !reflect.DeepEqual(cappedPlan, rawPlan) {
				t.Fatalf("planHolderReadStatements(%v) capped = %v, raw = %v: the cap moved a split decision", chunk, cappedPlan, rawPlan)
			}
		})
	}
}

// TestCapBoundaryKeepsHotAloneAndColdPacked pins the two sides of the #10681
// boundary directly: a mass at the hot threshold still packs with cold ids,
// while the capped value for anything hotter still runs alone.
func TestCapBoundaryKeepsHotAloneAndColdPacked(t *testing.T) {
	t.Parallel()

	threshold := int64(fanOutHolderHotMassRows)
	chunk := []string{"id-edge", "id-cold"}
	if got := planHolderReadStatements(chunk, map[string]int64{"id-edge": threshold, "id-cold": 0}); !reflect.DeepEqual(got, [][]string{chunk}) {
		t.Fatalf("threshold mass plan = %v, want the chunk whole (at-threshold still packs)", got)
	}
	if got := planHolderReadStatements(chunk, map[string]int64{"id-edge": HolderMassProbeCap, "id-cold": 12}); !reflect.DeepEqual(got, [][]string{{"id-edge"}, {"id-cold"}}) {
		t.Fatalf("capped mass plan = %v, want the hot id alone", got)
	}
}
