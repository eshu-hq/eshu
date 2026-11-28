// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeDeltaUnionActiveSource is a canned ACTIVE source recording its scopes.
type fakeDeltaUnionActiveSource struct {
	snapshot DeltaUnionActiveSnapshot
	err      error
	scopes   []string
	calls    int
}

// ReadDeltaUnionActive records the call and returns the canned snapshot.
func (f *fakeDeltaUnionActiveSource) ReadDeltaUnionActive(_ context.Context, scopeIDs []string) (DeltaUnionActiveSnapshot, error) {
	f.calls++
	f.scopes = append([]string(nil), scopeIDs...)
	return f.snapshot, f.err
}

// fakeDeltaUnionGraphRunner is a canned graph runner recording statements.
type fakeDeltaUnionGraphRunner struct {
	rows       map[string][]map[string]any
	err        error
	calls      int
	statements []string
}

// Run records the statement and returns rows keyed by the repo_id param.
func (f *fakeDeltaUnionGraphRunner) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	f.calls++
	f.statements = append(f.statements, cypher)
	if f.err != nil {
		return nil, f.err
	}
	repoID, _ := params["repo_id"].(string)
	return f.rows[repoID], nil
}

// deltaUnionCheckSnapshot is the core-parity fixture: base keep+w, delta keep+x, w deleted.
func deltaUnionCheckSnapshot() DeltaUnionActiveSnapshot {
	return DeltaUnionActiveSnapshot{
		Rows: []DeltaUnionActiveRow{
			{RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-base", GenerationStatus: "superseded", Kind: "file", StableKey: "file:repo-1:keep.go", Path: "keep.go"},
			{RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-base", GenerationStatus: "superseded", Kind: "file", StableKey: "file:repo-1:w.go", Path: "w.go"},
			{RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-delta", GenerationStatus: "active", IsDelta: true, Kind: "file", StableKey: "file:repo-1:keep.go", Path: "keep.go"},
			{RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-delta", GenerationStatus: "active", IsDelta: true, Kind: "file", StableKey: "file:repo-1:x.go", Path: "x.go"},
		},
		Deltas: map[string]DeltaUnionDeltaPaths{"scope-1": {BaselineCommit: "commit-a", Changed: []string{"keep.go", "x.go"}, Deleted: []string{"w.go"}}},
	}
}

// newDeltaUnionChecker builds a complete-checker with fakes.
func newDeltaUnionChecker() (*DeltaUnionCompletenessChecker, *fakeDeltaUnionActiveSource, *fakeDeltaUnionGraphRunner) {
	active := &fakeDeltaUnionActiveSource{snapshot: deltaUnionCheckSnapshot()}
	graph := &fakeDeltaUnionGraphRunner{rows: map[string][]map[string]any{"repo-1": {{"rel": "keep.go", "entities": int64(0)}, {"rel": "x.go", "entities": float64(0)}}}}
	return &DeltaUnionCompletenessChecker{Active: active, Graph: graph}, active, graph
}

// TestDeltaUnionCheckerCompleteViaStorePort pins the checker satisfies
// DeltaUnionStore and uses the exported anchored Cypher.
func TestDeltaUnionCheckerCompleteViaStorePort(t *testing.T) {
	checker, active, graph := newDeltaUnionChecker()
	var store DeltaUnionStore = checker
	report, err := store.CheckDeltaUnionCompleteness(context.Background(), []string{"scope-1"})
	if err != nil {
		t.Fatalf("CheckDeltaUnionCompleteness() error = %v", err)
	}
	if len(report.Repos) != 1 || report.Repos[0].Outcome != DeltaUnionRepoComplete || report.Mismatches != 0 {
		t.Fatalf("report = %+v, want one complete repo", report)
	}
	if len(active.scopes) != 1 || active.scopes[0] != "scope-1" {
		t.Fatalf("scopes = %v, want [scope-1]", active.scopes)
	}
	if graph.calls != 1 || graph.statements[0] != DeltaUnionGraphFilesCypher {
		t.Fatalf("graph calls=%d statements=%v, want anchored Cypher", graph.calls, graph.statements)
	}
	if !strings.Contains(DeltaUnionGraphFilesCypher, "MATCH (r:Repository {id: $repo_id})") {
		t.Fatalf("Cypher = %q, want Repository anchor", DeltaUnionGraphFilesCypher)
	}
}

// TestDeltaUnionCheckerMismatchListsDiff pins missing/extra/drifted through Check.
func TestDeltaUnionCheckerMismatchListsDiff(t *testing.T) {
	checker, _, graph := newDeltaUnionChecker()
	graph.rows["repo-1"] = []map[string]any{{"rel": "keep.go", "entities": int(2)}, {"rel": "w.go", "entities": int64(0)}}
	report, err := checker.Check(context.Background(), []string{"scope-1"})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(report.Repos) != 1 || report.Repos[0].Outcome != DeltaUnionRepoMismatch || report.Mismatches != 1 {
		t.Fatalf("report = %+v, want one mismatch", report)
	}
	repo := report.Repos[0]
	if len(repo.Missing) != 1 || repo.Missing[0] != "x.go" || len(repo.Extra) != 1 || repo.Extra[0] != "w.go" || len(repo.Drifted) != 1 {
		t.Fatalf("diff = %+v, want missing x.go extra w.go drifted keep.go", repo)
	}
	if repo.Base.GenerationID != "gen-base" || repo.Active.GenerationID != "gen-delta" || !repo.Active.IsDelta {
		t.Fatalf("generations = %+v %+v", repo.Base, repo.Active)
	}
}

// TestDeltaUnionCheckerSkipsWithoutGraphRead pins both skipped reasons plus
// the unseen-scope entry, none of which may read the graph.
func TestDeltaUnionCheckerSkipsWithoutGraphRead(t *testing.T) {
	active := &fakeDeltaUnionActiveSource{snapshot: DeltaUnionActiveSnapshot{Rows: []DeltaUnionActiveRow{
		{RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-full", GenerationStatus: "active", Kind: "file", StableKey: "file:repo-1:a.go", Path: "a.go"},
	}, Deltas: map[string]DeltaUnionDeltaPaths{}}}
	graph := &fakeDeltaUnionGraphRunner{err: errors.New("graph must not be read")}
	checker := &DeltaUnionCompletenessChecker{Active: active, Graph: graph}
	report, err := checker.Check(context.Background(), []string{"scope-1", "nope-scope"})
	if err != nil {
		t.Fatalf("Check() error = %v, want nil", err)
	}
	if graph.calls != 0 {
		t.Fatalf("graph calls = %d, want 0", graph.calls)
	}
	if len(report.Repos) != 2 || report.Mismatches != 0 {
		t.Fatalf("report = %+v, want two skipped entries", report)
	}
	for _, repo := range report.Repos {
		if repo.Outcome != DeltaUnionRepoSkipped {
			t.Fatalf("repo = %+v, want skipped", repo)
		}
	}
}

// TestDeltaUnionCheckerScopeRequiredAndCaps pins scope-required, pre-fanout
// scope cap (no ACTIVE read), and post-assembly repository cap.
func TestDeltaUnionCheckerScopeRequiredAndCaps(t *testing.T) {
	checker, active, _ := newDeltaUnionChecker()
	if _, err := checker.Check(context.Background(), []string{"  "}); !errors.Is(err, ErrDeltaUnionScopeRequired) {
		t.Fatalf("empty scope error = %v, want ErrDeltaUnionScopeRequired", err)
	}
	checker.MaxRepos = 1
	callsBefore := active.calls
	_, err := checker.Check(context.Background(), []string{"scope-1", "scope-2"})
	var capErr *DeltaUnionCapError
	if !errors.As(err, &capErr) || capErr.Unit != "scopes" {
		t.Fatalf("scope cap error = %v, want DeltaUnionCapError scopes", err)
	}
	if active.calls != callsBefore {
		t.Fatal("scope cap must fail before ACTIVE read")
	}
	// Post-assembly cap: two delta-active scopes under one requested scope is
	// impossible, so use MaxRepos=1 with two delta scopes in snapshot but a
	// single-scope filter widened via snapshot rows for scope-2 as well.
	checker2, active2, _ := newDeltaUnionChecker()
	checker2.MaxRepos = 1
	active2.snapshot.Rows = append(active2.snapshot.Rows, DeltaUnionActiveRow{RepoID: "repo-2", ScopeID: "scope-2", GenerationID: "gen-delta-2", GenerationStatus: "active", IsDelta: true, Kind: "file", StableKey: "file:repo-2:a.go", Path: "a.go"})
	// Request only scope-1 by filter but fake returns both scopes; comparable=2 > 1.
	_, err = checker2.Check(context.Background(), []string{"scope-1"})
	if !errors.As(err, &capErr) || capErr.Unit != "repositories" {
		t.Fatalf("repo cap error = %v, want DeltaUnionCapError repositories", err)
	}
}

// TestDeltaUnionCheckerUnconfiguredAndGraphError pins no partial complete on
// graph failure and the unconfigured error.
func TestDeltaUnionCheckerUnconfiguredAndGraphError(t *testing.T) {
	if _, err := (&DeltaUnionCompletenessChecker{}).Check(context.Background(), []string{"scope-1"}); err == nil {
		t.Fatal("unconfigured Check() error = nil, want error")
	}
	var nilChecker *DeltaUnionCompletenessChecker
	if _, err := nilChecker.Check(context.Background(), []string{"scope-1"}); err == nil {
		t.Fatal("nil checker error = nil, want error")
	}
	checker, _, graph := newDeltaUnionChecker()
	graph.err = errors.New("graph: connection refused host=db.internal password=hunter2")
	report, err := checker.Check(context.Background(), []string{"scope-1"})
	if err == nil {
		t.Fatal("graph error = nil, want propagated error")
	}
	if len(report.Repos) != 0 {
		t.Fatalf("report = %+v, want no partial report", report)
	}
}
