// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import "testing"

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
