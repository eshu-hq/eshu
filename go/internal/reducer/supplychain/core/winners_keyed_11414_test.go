// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"testing"
)

// TestFirstPassOverUnchangedPriorGenerationStagesNothing is the #11414
// regression: a re-match pass over unchanged inputs (a new generation that
// derives the same findings as the scope's previous generation) must stage
// zero winners dirty rows. Before the fix the capture consulted only the
// pass's own generation, so every first pass of a new generation re-staged
// its whole window even with zero content change.
func TestFirstPassOverUnchangedPriorGenerationStagesNothing(t *testing.T) {
	t.Parallel()

	a, b := changedTestFinding("CVE-2026-0001"), changedTestFinding("CVE-2026-0002")
	next := changedTestWrite(a, b)
	prior := changedTestWrite(a, b)
	writer, beginner, batches, _ := captureWriterRecording()
	beginner.state.stored = []fakeStoredFinding{priorLike(prior, a), priorLike(prior, b)}
	result, err := writer.WriteSupplyChainImpactFindings(context.Background(), next)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	if len(*batches) != 0 {
		t.Fatalf("captured = %+v, want no capture for a re-match over unchanged inputs (#11414)", *batches)
	}
	if len(result.ChangedCVEIDs) != 0 {
		t.Fatalf("ChangedCVEIDs = %v, want none for a re-match over unchanged inputs", result.ChangedCVEIDs)
	}
}

// TestDirtyIdentifiersToCaptureConsultsPriorGeneration pins the capture
// contract behind #11414 at the function seam: a first pass (no rows of its
// own generation stored yet) is judged against the previous generation by
// stable key. Identical content stages nothing; a digest move, an evidence
// move, an unknown stable key, and a digest-less prior row each stage.
func TestDirtyIdentifiersToCaptureConsultsPriorGeneration(t *testing.T) {
	t.Parallel()

	ident := WinnersDirtyIdentifier{PackageID: "pkg:npm/x", RepositoryID: "repo-1", C0: "CVE-2026-1", AdvisoryID: "GHSA-1"}
	row := func(stableKey string, evidence ...string) changedRow {
		return changedRow{factID: "fact-new-" + stableKey, stableKey: stableKey, digest: "digest-1", ident: ident, evidenceFactIDs: evidence}
	}
	prior := func(stableKey, digest string, evidence ...string) storedFinding {
		return storedFinding{factID: "fact-old-" + stableKey, generationID: "gen-0", stableKey: stableKey, digest: digest, evidenceFactIDs: evidence}
	}

	for _, tc := range []struct {
		name    string
		rows    []changedRow
		stored  []storedFinding
		wantLen int
	}{
		{name: "identical prior content stages nothing", rows: []changedRow{row("k1", "ev-a")}, stored: []storedFinding{prior("k1", "digest-1", "ev-a")}, wantLen: 0},
		{name: "digest move stages", rows: []changedRow{row("k1", "ev-a")}, stored: []storedFinding{prior("k1", "digest-2", "ev-a")}, wantLen: 1},
		{name: "evidence move stages", rows: []changedRow{row("k1", "ev-b")}, stored: []storedFinding{prior("k1", "digest-1", "ev-a")}, wantLen: 1},
		{name: "unknown stable key stages", rows: []changedRow{row("k9", "ev-a")}, stored: []storedFinding{prior("k1", "digest-1", "ev-a")}, wantLen: 1},
		{name: "digest-less prior row stages", rows: []changedRow{row("k1", "ev-a")}, stored: []storedFinding{prior("k1", "", "ev-a")}, wantLen: 1},
		{name: "no prior baseline stages", rows: []changedRow{row("k1", "ev-a")}, stored: nil, wantLen: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := dirtyIdentifiersToCapture(tc.rows, tc.stored, "gen-1")
			if len(got) != tc.wantLen {
				t.Fatalf("dirtyIdentifiersToCapture = %v, want %d identifiers", got, tc.wantLen)
			}
		})
	}
}
