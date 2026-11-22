// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// TestCoordinatorSourcePacingScopedParity proves the scoped-token
// redaction parity for the #10882 AC6 surface: the unscoped coordinator
// map carries source_pacing (with reason, engaged_since, and stuck,
// mirroring collector_backpressure beside it), while the scoped map
// withholds both process-global aggregates. Scoped callers cannot
// attribute those aggregates to their grants, so parity means both stay
// out — never one in and one out.
func TestCoordinatorSourcePacingScopedParity(t *testing.T) {
	t.Parallel()

	snapshot := &status.CoordinatorSnapshot{
		CollectorBackpressure: []collector.BackpressureSnapshot{
			{CollectorKind: "git", CollectorInstanceID: "git-primary", Pending: 7},
		},
		SourcePacing: []collector.SourcePacingSnapshot{
			{
				SourceSystem:       "vuln-intel",
				Slowed:             true,
				Reason:             "source_behind",
				OldestClaimableAge: 2 * time.Hour,
				ClaimableDepth:     400,
				EngagedSince:       time.Date(2026, 11, 21, 11, 0, 0, 0, time.UTC),
				Stuck:              true,
			},
		},
	}

	full := coordinatorToMap(snapshot)
	pacing, ok := full["source_pacing"].([]map[string]any)
	if !ok || len(pacing) != 1 {
		t.Fatalf("unscoped coordinator source_pacing = %#v, want one row", full["source_pacing"])
	}
	for _, want := range []string{"source_system", "slowed", "reason", "oldest_claimable_age", "claimable_depth", "engaged_since", "stuck"} {
		if _, ok := pacing[0][want]; !ok {
			t.Fatalf("unscoped source_pacing row missing %q: %#v", want, pacing[0])
		}
	}
	if _, ok := full["collector_backpressure"]; !ok {
		t.Fatalf("unscoped coordinator map missing collector_backpressure: %#v", full)
	}

	scoped := scopedCoordinatorToMap(snapshot)
	for _, withheld := range []string{"source_pacing", "collector_backpressure"} {
		if _, ok := scoped[withheld]; ok {
			t.Fatalf("scoped coordinator map carries %q, want withheld like every process-global aggregate", withheld)
		}
	}
}
