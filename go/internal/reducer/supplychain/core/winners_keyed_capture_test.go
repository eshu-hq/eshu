// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import "testing"

// TestDirtyIdentifiersToCaptureStagesEvidenceOnlyChange pins the #10880
// arbiter ruling (2026-11-21, decision 4): evidence_fact_ids is excluded
// from the content digest but served from the stored winner payload, so a
// same-generation rewrite whose digest is unchanged is still captured when
// its evidence set changed, and still skipped when only the volatile keys
// (intent, cause, generation) churned — those are stripped from the stored
// payload by the winner select (decision 3).
func TestDirtyIdentifiersToCaptureStagesEvidenceOnlyChange(t *testing.T) {
	t.Parallel()

	const generation = "gen-1"
	ident := WinnersDirtyIdentifier{PackageID: "pkg:npm/x", RepositoryID: "repo-1", C0: "CVE-2026-1"}
	row := func(evidence ...string) changedRow {
		return changedRow{factID: "fact-1", digest: "digest-1", ident: ident, evidenceFactIDs: evidence}
	}
	stored := func(evidence ...string) []storedFinding {
		return []storedFinding{{factID: "fact-1", generationID: generation, digest: "digest-1", evidenceFactIDs: evidence}}
	}

	for _, tc := range []struct {
		name    string
		rows    []changedRow
		stored  []storedFinding
		wantLen int
	}{
		{name: "evidence added stages", rows: []changedRow{row("ev-a", "ev-b")}, stored: stored("ev-a"), wantLen: 1},
		{name: "evidence removed stages", rows: []changedRow{row()}, stored: stored("ev-a"), wantLen: 1},
		{name: "same evidence set reordered skips", rows: []changedRow{row("ev-b", "ev-a")}, stored: stored("ev-a", "ev-b"), wantLen: 0},
		{name: "identical rewrite skips", rows: []changedRow{row("ev-a")}, stored: stored("ev-a"), wantLen: 0},
		{name: "new row stages", rows: []changedRow{row("ev-a")}, stored: nil, wantLen: 1},
		{
			name:    "prior generation baseline never skips",
			rows:    []changedRow{row("ev-a")},
			stored:  []storedFinding{{factID: "fact-1", generationID: "gen-0", digest: "digest-1", evidenceFactIDs: []string{"ev-a"}}},
			wantLen: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := dirtyIdentifiersToCapture(tc.rows, tc.stored, generation)
			if len(got) != tc.wantLen {
				t.Fatalf("dirtyIdentifiersToCapture = %v, want %d identifiers", got, tc.wantLen)
			}
			if tc.wantLen == 1 && got[0] != ident {
				t.Fatalf("captured identifier = %+v, want %+v", got[0], ident)
			}
		})
	}
}
