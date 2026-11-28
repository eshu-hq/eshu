// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import "testing"

// TestAssembleDeltaUnionExpectationsOverlaysBaseWithDelta pins the independent
// ACTIVE assembly: delta wins per stable key, touched base rows drop, deleted
// paths vanish, tombstones never count.
func TestAssembleDeltaUnionExpectationsOverlaysBaseWithDelta(t *testing.T) {
	rows := []DeltaUnionActiveRow{
		{RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-base", GenerationStatus: "superseded", Kind: "file", StableKey: "file:repo-1:keep.go", Path: "keep.go"},
		{RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-base", GenerationStatus: "superseded", Kind: "file", StableKey: "file:repo-1:w.go", Path: "w.go"},
		{RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-base", GenerationStatus: "superseded", Kind: "content_entity", StableKey: "content_entity:keep-fn", Path: "keep.go", Entity: true},
		{RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-delta", GenerationStatus: "active", IsDelta: true, Kind: "file", StableKey: "file:repo-1:keep.go", Path: "keep.go"},
		{RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-delta", GenerationStatus: "active", IsDelta: true, Kind: "file", StableKey: "file:repo-1:x.go", Path: "x.go"},
		{RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-delta", GenerationStatus: "active", IsDelta: true, Kind: "content_entity", StableKey: "content_entity:gone", Path: "w.go", Tombstone: true},
	}
	got := AssembleDeltaUnionExpectations(rows, map[string]DeltaUnionDeltaPaths{
		"scope-1": {BaselineCommit: "commit-a", Changed: []string{"keep.go", "x.go"}, Deleted: []string{"w.go"}},
	})
	if len(got) != 1 {
		t.Fatalf("expectations = %d, want 1", len(got))
	}
	exp := got[0]
	if exp.BaseGenerationID != "gen-base" || exp.ActiveGenerationID != "gen-delta" || !exp.ActiveIsDelta {
		t.Fatalf("generations = %+v, want base gen-base active delta gen-delta", exp)
	}
	if len(exp.Files) != 2 || exp.Files["keep.go"] != 0 || exp.Files["x.go"] != 0 {
		t.Fatalf("files = %v, want {keep.go:0 x.go:0}", exp.Files)
	}
	if _, ok := exp.Files["w.go"]; ok {
		t.Fatalf("files = %v, want no deleted w.go", exp.Files)
	}
}

// TestAssembleDeltaUnionExpectationsNoActiveYieldsSkippable pins that a scope
// known only through its delta entry still yields a skippable expectation.
func TestAssembleDeltaUnionExpectationsNoActiveYieldsSkippable(t *testing.T) {
	got := AssembleDeltaUnionExpectations(nil, map[string]DeltaUnionDeltaPaths{"scope-9": {Changed: []string{"a.go"}}})
	if len(got) != 1 || got[0].ScopeID != "scope-9" || got[0].ActiveGenerationID != "" {
		t.Fatalf("got = %+v, want one skippable scope-9 expectation", got)
	}
}

// TestCompareDeltaUnionRepoCompleteMismatchSkipped pins complete, mismatch
// (missing/extra/drifted), and both skipped reasons.
func TestCompareDeltaUnionRepoCompleteMismatchSkipped(t *testing.T) {
	exp := DeltaUnionExpectation{RepoID: "repo-1", ScopeID: "scope-1", BaseGenerationID: "gen-base", BaseStatus: "superseded", ActiveGenerationID: "gen-delta", ActiveIsDelta: true, Files: map[string]int64{"keep.go": 1, "x.go": 0}}
	complete := CompareDeltaUnionRepo(exp, DeltaUnionObservation{RepoID: "repo-1", Files: map[string]int64{"keep.go": 1, "x.go": 0}})
	if complete.Outcome != DeltaUnionRepoComplete {
		t.Fatalf("complete outcome = %q, want complete (%+v)", complete.Outcome, complete)
	}
	mismatch := CompareDeltaUnionRepo(exp, DeltaUnionObservation{RepoID: "repo-1", Files: map[string]int64{"keep.go": 3, "w.go": 0}})
	if mismatch.Outcome != DeltaUnionRepoMismatch {
		t.Fatalf("mismatch outcome = %q, want mismatch", mismatch.Outcome)
	}
	if len(mismatch.Missing) != 1 || mismatch.Missing[0] != "x.go" || len(mismatch.Extra) != 1 || mismatch.Extra[0] != "w.go" || len(mismatch.Drifted) != 1 || mismatch.Drifted[0] != "keep.go: graph=3 want=1" {
		t.Fatalf("mismatch diff = %+v, want missing x.go extra w.go drifted keep.go", mismatch)
	}
	nonDelta := exp
	nonDelta.ActiveIsDelta = false
	if got := CompareDeltaUnionRepo(nonDelta, DeltaUnionObservation{}); got.Outcome != DeltaUnionRepoSkipped {
		t.Fatalf("non-delta outcome = %q, want skipped", got.Outcome)
	}
	noActive := exp
	noActive.ActiveGenerationID = ""
	if got := CompareDeltaUnionRepo(noActive, DeltaUnionObservation{}); got.Outcome != DeltaUnionRepoSkipped {
		t.Fatalf("no-active outcome = %q, want skipped", got.Outcome)
	}
}

// TestSummarizeDeltaUnionCompletenessSortsAndCounts pins stable repo/scope
// ordering and the mismatch count.
func TestSummarizeDeltaUnionCompletenessSortsAndCounts(t *testing.T) {
	report := SummarizeDeltaUnionCompleteness([]DeltaUnionRepoReport{
		{RepoID: "repo-2", ScopeID: "scope-2", Outcome: DeltaUnionRepoComplete},
		{RepoID: "repo-1", ScopeID: "scope-1", Outcome: DeltaUnionRepoMismatch},
		{RepoID: "repo-1", ScopeID: "scope-0", Outcome: DeltaUnionRepoSkipped},
	})
	if report.Mismatches != 1 {
		t.Fatalf("mismatches = %d, want 1", report.Mismatches)
	}
	if len(report.Repos) != 3 || report.Repos[0].ScopeID != "scope-0" || report.Repos[1].ScopeID != "scope-1" || report.Repos[2].RepoID != "repo-2" {
		t.Fatalf("order = %+v, want repo/scope sorted", report.Repos)
	}
}
