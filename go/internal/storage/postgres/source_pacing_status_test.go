// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"
)

// TestReadSourcePacingStatusPublishesLatchTruth proves the status read
// joins the grouped claimable signal with the shared latch: the engaged
// source publishes slowed with the source_behind reason and its engagement
// start, the healthy source publishes unslowed, and a latched source whose
// queue drained still appears so the operator sees the engagement until a
// consult releases it.
func TestReadSourcePacingStatusPublishesLatchTruth(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
	queryer := &sourceBackpressureFakeDB{responses: []fakeRows{
		{rows: [][]any{{"git", float64(60), int64(3)}, {"vuln-intel", float64(7200), int64(400)}}},
		{rows: [][]any{
			{"vuln-intel", true, now.Add(-time.Hour)},
			{"drained", true, now.Add(-2 * time.Hour)},
		}},
	}}

	rows, err := readSourcePacingStatus(context.Background(), queryer, now)
	if err != nil {
		t.Fatalf("readSourcePacingStatus() error = %v, want nil", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (git, vuln-intel, drained)", len(rows))
	}
	bySource := map[string]int{}
	for i := range rows {
		bySource[rows[i].SourceSystem] = i
	}
	vuln := rows[bySource["vuln-intel"]]
	if !vuln.Slowed || vuln.Reason != "source_behind" || !vuln.EngagedSince.Equal(now.Add(-time.Hour)) {
		t.Fatalf("vuln-intel = %+v, want slowed/source_behind engaged 1h ago", vuln)
	}
	if vuln.ClaimableDepth != 400 || vuln.OldestClaimableAge != 2*time.Hour {
		t.Fatalf("vuln-intel observation = %d/%s, want 400/2h", vuln.ClaimableDepth, vuln.OldestClaimableAge)
	}
	if git := rows[bySource["git"]]; git.Slowed {
		t.Fatalf("git = %+v, want unslowed", git)
	}
	if drained := rows[bySource["drained"]]; !drained.Slowed {
		t.Fatalf("drained = %+v, want slowed (latch still engaged)", drained)
	}
}
