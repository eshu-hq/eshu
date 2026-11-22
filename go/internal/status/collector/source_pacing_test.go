// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"strings"
	"testing"
	"time"
)

func sourcePacingFixture(now time.Time) []SourcePacingSnapshot {
	return []SourcePacingSnapshot{
		{SourceSystem: "npm", Slowed: false, OldestClaimableAge: -5 * time.Second, ClaimableDepth: 3},
		{
			SourceSystem: "git", Slowed: true, Reason: "source_behind", Stuck: true,
			OldestClaimableAge: 2 * time.Hour, ClaimableDepth: 400, EngagedSince: now.Add(-45 * time.Minute),
		},
	}
}

// TestCloneSourcePacingOrdersByAndClampsAges proves the clone discipline of the
// other status projections: stable source order, no negative age, an empty
// input stays nil, and the caller's slice is not reordered.
func TestCloneSourcePacingOrdersByAndClampsAges(t *testing.T) {
	t.Parallel()

	if got := CloneSourcePacing(nil); got != nil {
		t.Fatalf("CloneSourcePacing(nil) = %+v, want nil", got)
	}
	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	input := sourcePacingFixture(now)
	got := CloneSourcePacing(input)
	if len(got) != 2 || got[0].SourceSystem != "git" || got[1].SourceSystem != "npm" {
		t.Fatalf("CloneSourcePacing() = %+v, want git then npm", got)
	}
	if got[1].OldestClaimableAge != 0 {
		t.Fatalf("negative age = %s, want it clamped to 0", got[1].OldestClaimableAge)
	}
	if input[0].SourceSystem != "npm" || input[0].OldestClaimableAge != -5*time.Second {
		t.Fatalf("CloneSourcePacing mutated its input: %+v", input[0])
	}
}

// TestRenderSourcePacingLinesNamesStateAndObservation proves the text status
// surface: nothing for no rows, otherwise a header and one line per source with
// the slowed flag and the observation, the reason, stuck and engagement only
// when set.
func TestRenderSourcePacingLinesNamesStateAndObservation(t *testing.T) {
	t.Parallel()

	if got := RenderSourcePacingLines(nil); got != nil {
		t.Fatalf("RenderSourcePacingLines(nil) = %q, want nil", got)
	}
	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	lines := RenderSourcePacingLines(sourcePacingFixture(now))
	if len(lines) != 3 || lines[0] != "Source pacing:" {
		t.Fatalf("lines = %q, want a header and two source lines", lines)
	}
	slowed, calm := lines[1], lines[2]
	for _, want := range []string{"git slowed=true", "oldest_claimable=2h0m0s", "claimable_depth=400", "reason=source_behind", "stuck=true", "engaged_since=2026-11-22T11:15:00Z"} {
		if !strings.Contains(slowed, want) {
			t.Fatalf("slowed line %q missing %q", slowed, want)
		}
	}
	if !strings.Contains(calm, "npm slowed=false") || strings.Contains(calm, "reason=") || strings.Contains(calm, "stuck=") || strings.Contains(calm, "engaged_since=") {
		t.Fatalf("calm line %q should omit reason, stuck and engagement", calm)
	}
}

// TestSourcePacingJSONRowsProjectsWireShape proves the JSON projection: stable
// order, ages as string and seconds, engagement measured against the read
// time, and unslowed or future-dated engagements omit the duration.
func TestSourcePacingJSONRowsProjectsWireShape(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	rows := SourcePacingJSONRows(sourcePacingFixture(now), now)
	if len(rows) != 2 || rows[0].SourceSystem != "git" {
		t.Fatalf("rows = %+v, want git first", rows)
	}
	git := rows[0]
	if !git.Slowed || !git.Stuck || git.Reason != "source_behind" || git.ClaimableDepth != 400 {
		t.Fatalf("git row = %+v", git)
	}
	if git.OldestClaimableAge != "2h0m0s" || git.OldestClaimableAgeSeconds != 7200 {
		t.Fatalf("git age = %q / %v, want 2h0m0s / 7200", git.OldestClaimableAge, git.OldestClaimableAgeSeconds)
	}
	if git.EngagedSince != "2026-11-22T11:15:00Z" || git.EngagedDuration != "45m0s" || git.EngagedDurationSeconds != 2700 {
		t.Fatalf("git engagement = %q %q %v, want since 11:15Z, 45m0s, 2700", git.EngagedSince, git.EngagedDuration, git.EngagedDurationSeconds)
	}
	npm := rows[1]
	if npm.EngagedSince != "" || npm.EngagedDuration != "" || npm.OldestClaimableAgeSeconds != 0 {
		t.Fatalf("npm row = %+v, want no engagement and a clamped age", npm)
	}
	future := []SourcePacingSnapshot{{SourceSystem: "git", Slowed: true, EngagedSince: now.Add(time.Minute)}}
	if got := SourcePacingJSONRows(future, now)[0]; got.EngagedSince == "" || got.EngagedDuration != "" {
		t.Fatalf("future engagement = %+v, want the start time but no duration", got)
	}
}
