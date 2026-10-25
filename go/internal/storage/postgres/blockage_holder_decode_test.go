// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"testing"
)

// TestActiveWorkSummaryDecodesBlockageHolderDomain pins the #8954 decode
// contract: a blockage row carries holder_domain through to the report, a
// stored row written before the field existed decodes with an empty holder
// instead of failing the whole summary, and a wrongly typed value still fails.
func TestActiveWorkSummaryDecodesBlockageHolderDomain(t *testing.T) {
	t.Parallel()

	const tail = `"stage":"reducer","domain":"security_alert_reconciliation","conflict_domain":"scope","conflict_key":"s1","blocked_count":1,"oldest_blocked_age_seconds":3`
	for _, tc := range []struct {
		name, row, want string
		wantErr         bool
	}{
		{name: "holder present", row: `{` + tail + `,"holder_domain":"supply_chain_impact"}`, want: "supply_chain_impact"},
		{name: "holder empty for a readiness gate", row: `{` + tail + `,"holder_domain":""}`, want: ""},
		{name: "row stored before the field existed", row: `{` + tail + `}`, want: ""},
		{name: "wrong type is rejected", row: `{` + tail + `,"holder_domain":7}`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var summary activeWorkSummary
			err := summary.add(activeWorkSectionBlockage, tc.row)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("add() error = nil, want a decode error; blockages=%+v", summary.Blockages)
				}
				return
			}
			if err != nil {
				t.Fatalf("add() error = %v", err)
			}
			if len(summary.Blockages) != 1 || summary.Blockages[0].HolderDomain != tc.want {
				t.Fatalf("Blockages = %+v, want one row with HolderDomain %q", summary.Blockages, tc.want)
			}
		})
	}
}

// leaseScanLoops returns the Actual Loops of every CTE Scan node over
// inflight_leases anywhere in the plan.
func leaseScanLoops(plan map[string]any) []int {
	var loops []int
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		if node["CTE Name"] == "inflight_leases" {
			if n, ok := node["Actual Loops"].(float64); ok {
				loops = append(loops, int(n))
			}
		}
		for _, child := range planChildren(node) {
			walk(child)
		}
	}
	walk(plan)
	return loops
}

// requireLeaseScansRunOnce fails unless the plan reads inflight_leases at
// least twice (the hashed blocked filter and the holder map) and every read
// runs once, so the holder lookup cannot degrade into a per-row rescan (the nested loop
// a join took with no statistics, which this guard caught).
func requireLeaseScansRunOnce(plan map[string]any) error {
	loops := leaseScanLoops(plan)
	if len(loops) < 2 {
		return fmt.Errorf("plan reads inflight_leases %d times, want the blocked filter and the holder map", len(loops))
	}
	for _, n := range loops {
		if n != 1 {
			return fmt.Errorf("an inflight_leases scan ran %d times, want 1 (loops %v)", n, loops)
		}
	}
	return nil
}

// TestRequireLeaseScansRunOnceRejectsRescan is the seeded violation for the
// holder plan guard: a lease scan that loops, or only one lease read,
// must be refused, and a lookup that reads the set once passes.
func TestRequireLeaseScansRunOnceRejectsRescan(t *testing.T) {
	t.Parallel()

	scan := func(loops float64) any {
		return map[string]any{"CTE Name": "inflight_leases", "Actual Loops": loops}
	}
	for name, tc := range map[string]struct {
		plans   []any
		wantErr bool
	}{
		"filter and map once": {plans: []any{scan(1), scan(1)}},
		"map rescans per row": {plans: []any{scan(1), scan(200)}, wantErr: true},
		"holder map missing":  {plans: []any{scan(1)}, wantErr: true},
	} {
		err := requireLeaseScansRunOnce(map[string]any{"Plans": tc.plans})
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: error = %v, wantErr %t", name, err, tc.wantErr)
		}
	}
}
