// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import (
	"testing"
)

func deltaUnionTreeB() (DeltaUnionExpectation, DeltaUnionObservation) {
	expected := DeltaUnionExpectation{
		RepoID: "repo-1", ScopeID: "scope-1",
		BaseGenerationID: "gen-base", BaseStatus: "superseded",
		ActiveGenerationID: "gen-delta", ActiveIsDelta: true,
		Files: map[string]int64{"keep.go": 1, "x.go": 1, "other.go": 1},
	}
	observed := DeltaUnionObservation{
		RepoID: "repo-1",
		Files:  map[string]int64{"keep.go": 1, "x.go": 1, "other.go": 1},
	}
	return expected, observed
}

func TestEvaluateDeltaUnionCompletenessPass(t *testing.T) {
	expected, observed := deltaUnionTreeB()
	var r Report
	EvaluateDeltaUnionCompleteness([]DeltaUnionExpectation{expected}, []DeltaUnionObservation{observed}, &r)
	if r.Failed() {
		t.Fatalf("report failed, want pass: %+v", r)
	}
}

func TestEvaluateDeltaUnionCompletenessMismatchFailsRequired(t *testing.T) {
	expected, _ := deltaUnionTreeB()
	observed := DeltaUnionObservation{
		RepoID: "repo-1",
		// The deleted file was resurrected: the union dropped its base rows
		// but the graph still carries it.
		Files: map[string]int64{"keep.go": 1, "x.go": 1, "other.go": 1, "w.go": 1},
	}
	var r Report
	EvaluateDeltaUnionCompleteness([]DeltaUnionExpectation{expected}, []DeltaUnionObservation{observed}, &r)
	if !r.Failed() {
		t.Fatal("report passed, want required failure on resurrected file")
	}
}

func TestEvaluateDeltaUnionCompletenessEntityCountMismatchFails(t *testing.T) {
	expected, _ := deltaUnionTreeB()
	observed := DeltaUnionObservation{
		RepoID: "repo-1",
		// Stale entities survived beside the delta's fresh set.
		Files: map[string]int64{"keep.go": 3, "x.go": 1, "other.go": 1},
	}
	var r Report
	EvaluateDeltaUnionCompleteness([]DeltaUnionExpectation{expected}, []DeltaUnionObservation{observed}, &r)
	if !r.Failed() {
		t.Fatal("report passed, want required failure on entity-count drift")
	}
}

func TestEvaluateDeltaUnionCompletenessEmptyRepoPasses(t *testing.T) {
	var r Report
	EvaluateDeltaUnionCompleteness([]DeltaUnionExpectation{{
		RepoID: "repo-empty", ScopeID: "scope-empty",
		BaseGenerationID: "gen-base", BaseStatus: "active",
		ActiveGenerationID: "gen-base", Files: map[string]int64{},
	}}, []DeltaUnionObservation{{RepoID: "repo-empty", Files: map[string]int64{}}}, &r)
	if r.Failed() {
		t.Fatalf("empty repo failed, want pass: %+v", r)
	}
}

func TestEvaluateDeltaUnionCompletenessNoActiveGenerationSkips(t *testing.T) {
	var r Report
	EvaluateDeltaUnionCompleteness([]DeltaUnionExpectation{{
		RepoID: "repo-1", ScopeID: "scope-1",
		Files: map[string]int64{"keep.go": 1},
	}}, []DeltaUnionObservation{{RepoID: "repo-1"}}, &r)
	if r.Failed() {
		t.Fatalf("scope without an active generation failed, want skip-with-reason: %+v", r)
	}
}

// A scope whose active generation is a full generation, not a delta, is
// out of the delta-union comparison's scope (#7797): the check covers
// delta-active scopes only, so such scopes skip without comparing.
func TestCompareDeltaUnionRepoNonDeltaActiveSkips(t *testing.T) {
	expected := DeltaUnionExpectation{
		RepoID: "repo-1", ScopeID: "scope-1",
		BaseGenerationID: "", BaseStatus: "",
		ActiveGenerationID: "gen-full", ActiveIsDelta: false,
		// Two content_entity rows behind keep.go: the want side counts both.
		Files: map[string]int64{"keep.go": 2},
	}
	// The graph counts only :Function nodes behind the same path.
	observed := DeltaUnionObservation{
		RepoID: "repo-1",
		Files:  map[string]int64{"keep.go": 1},
	}
	result := CompareDeltaUnionRepo(expected, observed)
	if result.Outcome != DeltaUnionRepoSkipped {
		t.Fatalf("CompareDeltaUnionRepo() outcome = %q, want %q (missing=%v extra=%v drifted=%v)",
			result.Outcome, DeltaUnionRepoSkipped, result.Missing, result.Extra, result.Drifted)
	}
}

func TestAssembleDeltaUnionExpectationOverlaysBaseWithDelta(t *testing.T) {
	rows := []DeltaUnionActiveRow{
		{
			RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-base", GenerationStatus: "superseded", IsDelta: false,
			Kind: "file", StableKey: "file:repo-1:keep.go", Path: "keep.go", Tombstone: false,
		},
		{
			RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-base", GenerationStatus: "superseded", IsDelta: false,
			Kind: "file", StableKey: "file:repo-1:w.go", Path: "w.go", Tombstone: false,
		},
		{
			RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-delta", GenerationStatus: "active", IsDelta: true,
			Kind: "file", StableKey: "file:repo-1:keep.go", Path: "keep.go", Tombstone: false,
		},
		{
			RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-delta", GenerationStatus: "active", IsDelta: true,
			Kind: "file", StableKey: "file:repo-1:x.go", Path: "x.go", Tombstone: false,
		},
		// Tombstoned-only rows never count on either side.
		{
			RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-delta", GenerationStatus: "active", IsDelta: true,
			Kind: "content_entity", StableKey: "content_entity:gone", Path: "w.go", Tombstone: true,
		},
	}
	got := AssembleDeltaUnionExpectations(rows, map[string]DeltaUnionDeltaPaths{
		"scope-1": {BaselineCommit: "commit-a", Changed: []string{"keep.go", "x.go"}, Deleted: []string{"w.go"}},
	})
	want := map[string]int64{"keep.go": 0, "x.go": 0}
	if len(got) != 1 {
		t.Fatalf("expectations = %d, want 1", len(got))
	}
	exp := got[0]
	if exp.BaseGenerationID != "gen-base" || exp.ActiveGenerationID != "gen-delta" || !exp.ActiveIsDelta {
		t.Fatalf("generation annotation = %+v, want base gen-base and active delta gen-delta", exp)
	}
	for path := range want {
		if _, ok := exp.Files[path]; !ok {
			t.Fatalf("expected files = %v, want %v", exp.Files, want)
		}
	}
	if _, ok := exp.Files["w.go"]; ok {
		t.Fatalf("expected files = %v, want no deleted w.go", exp.Files)
	}
}

// Only Function entity rows count toward a path (#7797 P1): a Module
// content_entity row rides the same file but the ACTIVE read leaves its
// Entity flag false (the graph readers count only :Function nodes), so a
// correct graph with the Function count still compares complete.
func TestAssembleDeltaUnionExpectationIgnoresNonFunctionEntities(t *testing.T) {
	rows := []DeltaUnionActiveRow{
		{
			RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-base", GenerationStatus: "superseded", IsDelta: false,
			Kind: "file", StableKey: "file:repo-1:other.go", Path: "other.go",
		},
		{
			RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-base", GenerationStatus: "superseded", IsDelta: false,
			Kind: "content_entity", StableKey: "content_entity:other-fn", Path: "other.go", Entity: true,
		},
		{
			RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-base", GenerationStatus: "superseded", IsDelta: false,
			Kind: "content_entity", StableKey: "content_entity:other-mod", Path: "other.go", Entity: false,
		},
		{
			RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-delta", GenerationStatus: "active", IsDelta: true,
			Kind: "file", StableKey: "file:repo-1:keep.go", Path: "keep.go",
		},
	}
	got := AssembleDeltaUnionExpectations(rows, map[string]DeltaUnionDeltaPaths{
		"scope-1": {BaselineCommit: "commit-a", Changed: []string{"keep.go"}},
	})
	if len(got) != 1 {
		t.Fatalf("expectations = %d, want 1", len(got))
	}
	// other.go is untouched so both its entity rows survive the merge, but
	// only the Function row counts: the Module row's Entity flag is false.
	if got[0].Files["other.go"] != 1 {
		t.Fatalf("expected other.go entities = %d, want 1 (Module row must not count)", got[0].Files["other.go"])
	}
	result := CompareDeltaUnionRepo(got[0], DeltaUnionObservation{
		RepoID: "repo-1", Files: map[string]int64{"keep.go": 0, "other.go": 1},
	})
	if result.Outcome != DeltaUnionRepoComplete {
		t.Fatalf("CompareDeltaUnionRepo() outcome = %q, want %q (missing=%v extra=%v drifted=%v)",
			result.Outcome, DeltaUnionRepoComplete, result.Missing, result.Extra, result.Drifted)
	}
}

// The git-selection + delta scenario the gate never runs live: the selector
// forces a delta (baseline equals the active commit), the union overlays it,
// and the comparison passes. A forced full (reindex watermark unsatisfied)
// compares the full generation the same way.
func TestDeltaUnionSelectionScenario(t *testing.T) {
	var r Report
	// Selection forced delta: baseline commit-a equals the active full's
	// commit, so the delta overlays gen-base.
	rows := []DeltaUnionActiveRow{
		{
			RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-base", GenerationStatus: "superseded", IsDelta: false,
			Kind: "file", StableKey: "file:repo-1:a.go", Path: "a.go",
		},
		{
			RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-delta", GenerationStatus: "active", IsDelta: true,
			Kind: "file", StableKey: "file:repo-1:a.go", Path: "a.go",
		},
		{
			RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-delta", GenerationStatus: "active", IsDelta: true,
			Kind: "file", StableKey: "file:repo-1:b.go", Path: "b.go",
		},
	}
	expected := AssembleDeltaUnionExpectations(rows, map[string]DeltaUnionDeltaPaths{
		"scope-1": {BaselineCommit: "commit-a", Changed: []string{"a.go", "b.go"}},
	})
	EvaluateDeltaUnionCompleteness(expected, []DeltaUnionObservation{{
		RepoID: "repo-1", Files: map[string]int64{"a.go": 0, "b.go": 0},
	}}, &r)
	if r.Failed() {
		t.Fatalf("selection-delta scenario failed, want pass: %+v", r)
	}
}
