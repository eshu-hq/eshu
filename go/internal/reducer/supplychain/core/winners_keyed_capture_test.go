// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import "testing"

// TestDirtyIdentifiersToCaptureStagesEvidenceOnlyChange pins the #11598 Option C
// arbiter amendment to #10880 Decision 4: evidence_fact_ids is excluded from
// the content digest and is no longer secondary-compared in
// dirtyIdentifiersToCapture, so a same-generation or prior-generation rewrite
// whose content digest is unchanged skips staging even when its evidence_fact_ids
// set moved (daily EPSS fact_id churn). A digest move or a new row still stages.
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
		{name: "evidence added skips when digest equal (#11598)", rows: []changedRow{row("ev-a", "ev-b")}, stored: stored("ev-a"), wantLen: 0},
		{name: "evidence removed skips when digest equal (#11598)", rows: []changedRow{row()}, stored: stored("ev-a"), wantLen: 0},
		{name: "same evidence set reordered skips", rows: []changedRow{row("ev-b", "ev-a")}, stored: stored("ev-a", "ev-b"), wantLen: 0},
		{name: "identical rewrite skips", rows: []changedRow{row("ev-a")}, stored: stored("ev-a"), wantLen: 0},
		{name: "new row stages", rows: []changedRow{row("ev-a")}, stored: nil, wantLen: 1},
		{
			name:    "first pass skips on identical prior generation baseline",
			rows:    []changedRow{{factID: "fact-2", stableKey: "k-1", digest: "digest-1", ident: ident, evidenceFactIDs: []string{"ev-a"}}},
			stored:  []storedFinding{{factID: "fact-1", generationID: "gen-0", stableKey: "k-1", digest: "digest-1", evidenceFactIDs: []string{"ev-a"}}},
			wantLen: 0,
		},
		{
			name:    "first pass skips on evidence-only move with equal digest (#11598)",
			rows:    []changedRow{{factID: "fact-2", stableKey: "k-1", digest: "digest-1", ident: ident, evidenceFactIDs: []string{"ev-b"}}},
			stored:  []storedFinding{{factID: "fact-1", generationID: "gen-0", stableKey: "k-1", digest: "digest-1", evidenceFactIDs: []string{"ev-a"}}},
			wantLen: 0,
		},
		{
			name:    "first pass stages on prior digest move",
			rows:    []changedRow{{factID: "fact-2", stableKey: "k-1", digest: "digest-1", ident: ident, evidenceFactIDs: []string{"ev-a"}}},
			stored:  []storedFinding{{factID: "fact-1", generationID: "gen-0", stableKey: "k-1", digest: "digest-0", evidenceFactIDs: []string{"ev-a"}}},
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

// TestExcludeRetiredWinnersDirtyIdentifiersMirrorsTheRetraction pins the
// #11411 retired-key exclusion against the retraction's tombstone predicate:
// same generation, fact id outside the keep set, stored fencing token at or
// below the pass's, and, on a scoped pass, a cve id inside the stamp. A key
// dropped here is staged by the retraction in the same transaction, so the
// pass stages one dirty row per key. Order is preserved.
func TestExcludeRetiredWinnersDirtyIdentifiersMirrorsTheRetraction(t *testing.T) {
	t.Parallel()

	const generation = "gen-1"
	k1 := WinnersDirtyIdentifier{PackageID: "pkg:a", RepositoryID: "repo-1", SubjectDigest: "s", C0: "CVE-1"}
	k2 := WinnersDirtyIdentifier{PackageID: "pkg:b", RepositoryID: "repo-1", SubjectDigest: "s", C0: "CVE-2"}
	k3 := WinnersDirtyIdentifier{PackageID: "pkg:c", RepositoryID: "repo-1", SubjectDigest: "s", C0: "CVE-3"}
	row := func(factID, gen string, token int64, pkg, cve string) storedFinding {
		return storedFinding{
			factID: factID, generationID: gen, fencingToken: token,
			packageID: pkg, repositoryID: "repo-1", subjectDigest: "s", cveID: cve,
		}
	}

	for _, tc := range []struct {
		name         string
		idents       []WinnersDirtyIdentifier
		stored       []storedFinding
		keep         []string
		fencingToken int64
		partial      bool
		scoped       []string
		want         []WinnersDirtyIdentifier
	}{
		{
			name:         "retired same-generation row drops its key, order kept",
			idents:       []WinnersDirtyIdentifier{k1, k2, k3},
			stored:       []storedFinding{row("old-a", generation, 5, "pkg:a", "CVE-1")},
			keep:         []string{"new-a", "new-b", "new-c"},
			fencingToken: 10,
			want:         []WinnersDirtyIdentifier{k2, k3},
		},
		{
			name:         "kept row keeps its key",
			idents:       []WinnersDirtyIdentifier{k2},
			stored:       []storedFinding{row("new-b", generation, 5, "pkg:b", "CVE-2")},
			keep:         []string{"new-b"},
			fencingToken: 10,
			want:         []WinnersDirtyIdentifier{k2},
		},
		{
			name:         "prior generation never retires",
			idents:       []WinnersDirtyIdentifier{k1},
			stored:       []storedFinding{row("old-a", "gen-0", 5, "pkg:a", "CVE-1")},
			keep:         []string{"new-a"},
			fencingToken: 10,
			want:         []WinnersDirtyIdentifier{k1},
		},
		{
			name:         "row a fresher pass owns keeps the capture",
			idents:       []WinnersDirtyIdentifier{k1},
			stored:       []storedFinding{row("old-a", generation, 99, "pkg:a", "CVE-1")},
			keep:         []string{"new-a"},
			fencingToken: 10,
			want:         []WinnersDirtyIdentifier{k1},
		},
		{
			name:         "scoped pass retires only stamped rows",
			idents:       []WinnersDirtyIdentifier{k1, k2},
			stored:       []storedFinding{row("old-a", generation, 5, "pkg:a", "CVE-1"), row("old-b", generation, 5, "pkg:b", "CVE-2")},
			keep:         []string{"new-a", "new-b"},
			fencingToken: 10,
			scoped:       []string{"CVE-1"},
			want:         []WinnersDirtyIdentifier{k2},
		},
		{
			name:         "partial evidence pass excludes nothing",
			idents:       []WinnersDirtyIdentifier{k1},
			stored:       []storedFinding{row("old-a", generation, 5, "pkg:a", "CVE-1")},
			keep:         []string{"new-a"},
			fencingToken: 10,
			partial:      true,
			want:         []WinnersDirtyIdentifier{k1},
		},
		{
			name:         "empty capture stays empty",
			idents:       nil,
			stored:       []storedFinding{row("old-a", generation, 5, "pkg:a", "CVE-1")},
			keep:         []string{"new-a"},
			fencingToken: 10,
			want:         nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := excludeRetiredWinnersDirtyIdentifiers(tc.idents, tc.stored, tc.keep, generation, tc.fencingToken, tc.partial, tc.scoped)
			if len(got) != len(tc.want) {
				t.Fatalf("excludeRetiredWinnersDirtyIdentifiers = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("excludeRetiredWinnersDirtyIdentifiers = %v, want %v", got, tc.want)
				}
			}
		})
	}
}
