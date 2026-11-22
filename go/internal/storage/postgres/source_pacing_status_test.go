// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
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
		{rows: [][]any{
			{"vuln-intel", true, now.Add(-time.Hour)},
			{"drained", true, now.Add(-2 * time.Hour)},
		}},
		{rows: [][]any{{"git", float64(60), int64(3), int64(0), int64(0)}, {"vuln-intel", float64(7200), int64(400), int64(400), int64(400)}}},
	}}

	rows, err := readSourcePacingStatus(context.Background(), queryer, now, sourceBackpressureTestConfig())
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
	if drained := rows[bySource["drained"]]; drained.Stuck {
		t.Fatalf("drained = %+v, want unstuck (queue cleared, age below release)", drained)
	}
	if !vuln.Stuck {
		t.Fatalf("vuln-intel = %+v, want stuck (engaged 1h past 30m bound, still behind)", vuln)
	}
}

// TestReadSourcePacingStatusLeavesStuckUnevaluatedWithoutConfig proves a
// disabled config never marks stuck: publication without operator
// thresholds reports slowed state only.
func TestReadSourcePacingStatusLeavesStuckUnevaluatedWithoutConfig(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
	queryer := &sourceBackpressureFakeDB{responses: []fakeRows{
		{rows: [][]any{{"vuln-intel", true, now.Add(-time.Hour)}}},
		{rows: [][]any{{"vuln-intel", float64(7200), int64(400), int64(400), int64(400)}}},
	}}

	rows, err := readSourcePacingStatus(context.Background(), queryer, now, workflow.SourceBackpressureConfig{})
	if err != nil {
		t.Fatalf("readSourcePacingStatus() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if !rows[0].Slowed || rows[0].Stuck {
		t.Fatalf("row = %+v, want slowed but unstuck without config", rows[0])
	}
}

// TestReadSourcePacingStatusSkipsGroupedScanWhenNothingCanBeSlowed proves
// #10882 F3: with the per-source leg unconfigured and no engaged latch, the
// status read costs the small latch read and never the whole-backlog grouped
// claimable scan, so a deployment that never enables the leg pays nothing for
// it on every /admin/status read and summary pass.
func TestReadSourcePacingStatusSkipsGroupedScanWhenNothingCanBeSlowed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	queryer := &sourceBackpressureFakeDB{responses: []fakeRows{
		{rows: [][]any{{"vuln-intel", false, time.Time{}}}}, // a released latch row, not engaged
	}}

	rows, err := readSourcePacingStatus(context.Background(), queryer, now, workflow.SourceBackpressureConfig{})
	if err != nil {
		t.Fatalf("readSourcePacingStatus() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %+v, want none: nothing is slowed or observable", rows)
	}
	if len(queryer.queries) != 1 || !strings.Contains(queryer.queries[0], "workflow_claim_pacing") {
		t.Fatalf("queries = %d (%v), want exactly the latch read", len(queryer.queries), queryer.queries)
	}
}

// TestReadSourcePacingStatusScansWhenConfiguredOrEngaged proves the scan still
// runs when either condition holds: a configured leg publishes every source's
// observation, and an engaged latch must stay visible even on a process
// without the thresholds (gating on config alone would hide it).
func TestReadSourcePacingStatusScansWhenConfiguredOrEngaged(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		cfg    workflow.SourceBackpressureConfig
		latch  [][]any
		reason string
	}{
		{"configured, no latch", sourceBackpressureTestConfig(), nil, "configured"},
		{"engaged latch, zero config", workflow.SourceBackpressureConfig{}, [][]any{{"git", true, now.Add(-time.Hour)}}, "engaged"},
	} {
		queryer := &sourceBackpressureFakeDB{responses: []fakeRows{
			{rows: tc.latch},
			{rows: [][]any{{"git", float64(60), int64(3), int64(0), int64(0)}}},
		}}
		rows, err := readSourcePacingStatus(context.Background(), queryer, now, tc.cfg)
		if err != nil {
			t.Fatalf("%s: readSourcePacingStatus() error = %v, want nil", tc.name, err)
		}
		if len(queryer.queries) != 2 {
			t.Fatalf("%s: queries = %d, want latch read then grouped scan", tc.name, len(queryer.queries))
		}
		if len(rows) != 1 || rows[0].SourceSystem != "git" {
			t.Fatalf("%s: rows = %+v, want the git observation", tc.name, rows)
		}
	}
}

// TestReadSourcePacingStatusStuckMirrorsTheAgedRowVerdict proves the
// published stuck flag follows the same aged-row rule as the consult verdict
// (#10882 review R3-1). An engagement past MaxEngagement is stuck only while
// DepthFloor claimable rows are still past the release age; one poisoned old
// row among a thousand fresh ones is a release the next consult will make,
// so publishing it as stuck would contradict the verdict.
func TestReadSourcePacingStatusStuckMirrorsTheAgedRowVerdict(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	cfg := sourceBackpressureTestConfig() // floor 25, max engagement 30m
	for _, tc := range []struct {
		name        string
		pastRelease int64
		wantStuck   bool
	}{
		{"one old row among 1000 fresh", 1, false},
		{"floor minus one rows past release", cfg.DepthFloor - 1, false},
		{"floor rows past release", cfg.DepthFloor, true},
	} {
		queryer := &sourceBackpressureFakeDB{responses: []fakeRows{
			{rows: [][]any{{"busy", true, now.Add(-time.Hour)}}}, // engaged past the 30m bound
			{rows: [][]any{{"busy", float64((72 * time.Hour).Seconds()), int64(1001), tc.pastRelease, tc.pastRelease}}},
		}}
		rows, err := readSourcePacingStatus(context.Background(), queryer, now, cfg)
		if err != nil {
			t.Fatalf("%s: readSourcePacingStatus() error = %v, want nil", tc.name, err)
		}
		if len(rows) != 1 || !rows[0].Slowed {
			t.Fatalf("%s: rows = %+v, want one row still slowed (latch truth)", tc.name, rows)
		}
		if rows[0].Stuck != tc.wantStuck {
			t.Fatalf("%s: stuck = %v, want %v (past release age: %d rows, floor %d)",
				tc.name, rows[0].Stuck, tc.wantStuck, tc.pastRelease, cfg.DepthFloor)
		}
	}
}
