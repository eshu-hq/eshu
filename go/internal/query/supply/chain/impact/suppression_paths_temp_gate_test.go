// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "testing"

// TestSuppressionTempGateSplit pins the #7553 verdict-C per-path temp rule:
// paths that do not spill at stock work_mem gate on Temp == 0, while the
// three spilling paths keep the run-to-run stability gate. Pure contract
// test: no database needed.
func TestSuppressionTempGateSplit(t *testing.T) {
	tempProof := func(read, written int64) suppressionQueryPlanProof {
		var proof suppressionQueryPlanProof
		proof.Plan.TempReadBlocks = read
		proof.Plan.TempWrittenBlocks = written
		return proof
	}

	for _, name := range []string{"materialized_list", "explain"} {
		if !suppressionTempGateZero(name) {
			t.Errorf("%s should gate temp on zero, gates on stability", name)
		}
	}
	for _, name := range []string{"direct_list", "aggregate_count", "aggregate_priority_facet"} {
		if suppressionTempGateZero(name) {
			t.Errorf("%s spills at stock work_mem, should gate temp on stability", name)
		}
	}

	zero := tempProof(0, 0)
	if breach := suppressionTempGateBreach("materialized_list", zero, zero); breach != "" {
		t.Errorf("zero path at 0/0 breached: %s", breach)
	}
	// Stock-work_mem spill from the 2026-10-12 measurement: the zero-gated
	// path must reject it on either run.
	spilled := tempProof(2194, 3383)
	if breach := suppressionTempGateBreach("materialized_list", spilled, zero); breach == "" {
		t.Error("zero path with first-run spill passed, want breach")
	}
	if breach := suppressionTempGateBreach("explain", zero, spilled); breach == "" {
		t.Error("zero path with second-run spill passed, want breach")
	}
	// The same spill is the steady state of a spilling path: identical
	// observations on both runs must pass the stability gate.
	if breach := suppressionTempGateBreach("direct_list", spilled, spilled); breach != "" {
		t.Errorf("spilling path with identical runs breached: %s", breach)
	}
	if breach := suppressionTempGateBreach(
		"aggregate_count", tempProof(2194, 3383), tempProof(100, 100),
	); breach == "" {
		t.Error("spilling path with drifted temp passed, want breach")
	}
	// Below the noise floor a handful of blocks is not drift.
	if breach := suppressionTempGateBreach(
		"aggregate_priority_facet", tempProof(0, 0), tempProof(15, 15),
	); breach != "" {
		t.Errorf("spilling path below noise floor breached: %s", breach)
	}
}
