// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

type stubDeltaUnionActive struct {
	snapshot pgstatus.DeltaUnionActiveSnapshot
	err      error
	scopes   []string
}

func (s *stubDeltaUnionActive) ReadDeltaUnionActive(_ context.Context, scopeIDs []string) (pgstatus.DeltaUnionActiveSnapshot, error) {
	s.scopes = scopeIDs
	return s.snapshot, s.err
}

type stubDeltaUnionGraph struct {
	rows map[string][]map[string]any
	err  error
}

func (s *stubDeltaUnionGraph) Run(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
	if s.err != nil {
		return nil, s.err
	}
	text, _ := params["repo_id"].(string)
	return s.rows[text], nil
}

func (s *stubDeltaUnionGraph) RunSingle(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return nil, nil
}

func deltaUnionActiveSnapshot() pgstatus.DeltaUnionActiveSnapshot {
	return pgstatus.DeltaUnionActiveSnapshot{
		Rows: []goldengate.DeltaUnionActiveRow{
			{
				RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-base", GenerationStatus: "superseded",
				Kind: "file", StableKey: "file:repo-1:keep.go", Path: "keep.go",
			},
			{
				RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-base", GenerationStatus: "superseded",
				Kind: "file", StableKey: "file:repo-1:w.go", Path: "w.go",
			},
			{
				RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-delta", GenerationStatus: "active", IsDelta: true,
				Kind: "file", StableKey: "file:repo-1:keep.go", Path: "keep.go",
			},
			{
				RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-delta", GenerationStatus: "active", IsDelta: true,
				Kind: "file", StableKey: "file:repo-1:x.go", Path: "x.go",
			},
		},
		Deltas: map[string]goldengate.DeltaUnionDeltaPaths{
			"scope-1": {BaselineCommit: "commit-a", Changed: []string{"keep.go", "x.go"}, Deleted: []string{"w.go"}},
		},
	}
}

func deltaUnionChecker() (*DeltaUnionCompletenessChecker, *stubDeltaUnionActive) {
	active := &stubDeltaUnionActive{snapshot: deltaUnionActiveSnapshot()}
	graph := &stubDeltaUnionGraph{rows: map[string][]map[string]any{
		"repo-1": {
			{"rel": "keep.go", "entities": int64(0)},
			{"rel": "x.go", "entities": int64(0)},
		},
	}}
	return &DeltaUnionCompletenessChecker{Active: active, Graph: graph}, active
}

func TestDeltaUnionCompletenessCheckPasses(t *testing.T) {
	checker, active := deltaUnionChecker()

	report, err := checker.Check(context.Background(), []string{"scope-1"})
	if err != nil {
		t.Fatalf("Check() error = %v, want nil", err)
	}
	if len(active.scopes) != 1 || active.scopes[0] != "scope-1" {
		t.Fatalf("scope filter = %v, want [scope-1]", active.scopes)
	}
	if len(report.Repos) != 1 || report.Repos[0].Outcome != "complete" {
		t.Fatalf("report = %+v, want one complete repo", report)
	}
	if report.Mismatches != 0 {
		t.Fatalf("mismatches = %d, want 0", report.Mismatches)
	}
}

func TestDeltaUnionCompletenessCheckMismatchListsDiff(t *testing.T) {
	checker, _ := deltaUnionChecker()
	checker.Graph.(*stubDeltaUnionGraph).rows["repo-1"] = []map[string]any{
		{"rel": "keep.go", "entities": int64(0)},
		{"rel": "x.go", "entities": int64(0)},
		{"rel": "w.go", "entities": int64(0)},
	}

	report, err := checker.Check(context.Background(), []string{"scope-1"})
	if err != nil {
		t.Fatalf("Check() error = %v, want nil", err)
	}
	if len(report.Repos) != 1 || report.Repos[0].Outcome != "mismatch" {
		t.Fatalf("report = %+v, want one mismatched repo", report)
	}
	if len(report.Repos[0].Extra) != 1 || report.Repos[0].Extra[0] != "w.go" {
		t.Fatalf("extra = %v, want [w.go]", report.Repos[0].Extra)
	}
	if report.Repos[0].Base.GenerationID != "gen-base" || report.Repos[0].Active.GenerationID != "gen-delta" {
		t.Fatalf("generations = %+v %+v, want base gen-base and active gen-delta",
			report.Repos[0].Base, report.Repos[0].Active)
	}
	if report.Mismatches != 1 {
		t.Fatalf("mismatches = %d, want 1", report.Mismatches)
	}
}

// A scope whose active generation is a full generation, not a delta, is
// reported skipped without touching the graph (#7797): the want side counts
// every content_entity fact row per file while the graph side counts only
// :Function nodes. The erroring graph stub proves no read is issued.
func TestDeltaUnionCompletenessCheckNonDeltaSkipsWithoutGraphRead(t *testing.T) {
	active := &stubDeltaUnionActive{snapshot: pgstatus.DeltaUnionActiveSnapshot{
		Rows: []goldengate.DeltaUnionActiveRow{
			{
				RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-full", GenerationStatus: "active",
				Kind: "file", StableKey: "file:repo-1:config.yaml", Path: "config.yaml",
			},
			{
				RepoID: "repo-1", ScopeID: "scope-1", GenerationID: "gen-full", GenerationStatus: "active",
				Kind: "content_entity", StableKey: "content_entity:cfg-a", Path: "config.yaml",
			},
		},
		Deltas: map[string]goldengate.DeltaUnionDeltaPaths{},
	}}
	graph := &stubDeltaUnionGraph{err: errors.New("graph must not be read for a non-delta scope")}
	checker := &DeltaUnionCompletenessChecker{Active: active, Graph: graph}

	report, err := checker.Check(context.Background(), []string{"scope-1"})
	if err != nil {
		t.Fatalf("Check() error = %v, want nil (non-delta scope skips without a graph read)", err)
	}
	if len(report.Repos) != 1 {
		t.Fatalf("repos = %+v, want one skipped repo", report.Repos)
	}
	if report.Repos[0].Outcome != "skipped" {
		t.Fatalf("outcome = %q, want skipped: %+v", report.Repos[0].Outcome, report.Repos[0])
	}
	if report.Mismatches != 0 {
		t.Fatalf("mismatches = %d, want 0", report.Mismatches)
	}
}

func TestDeltaUnionCompletenessCheckRequiresConfiguration(t *testing.T) {
	checker := &DeltaUnionCompletenessChecker{}
	if _, err := checker.Check(context.Background(), nil); err == nil {
		t.Fatal("Check() error = nil, want configuration error")
	}
}

func TestDeltaUnionCompletenessCheckCapsRepos(t *testing.T) {
	checker, active := deltaUnionChecker()
	checker.MaxRepos = 1
	// A second delta-active scope: non-delta scopes skip without a graph
	// read and stay outside the cap, so the cap fixture must be delta.
	active.snapshot.Rows = append(active.snapshot.Rows,
		goldengate.DeltaUnionActiveRow{
			RepoID: "repo-2", ScopeID: "scope-2", GenerationID: "gen-delta-2",
			GenerationStatus: "active", IsDelta: true,
			Kind: "file", StableKey: "file:repo-2:a.go", Path: "a.go",
		})
	if _, err := checker.Check(context.Background(), []string{"scope-1", "scope-2"}); err == nil {
		t.Fatal("Check() error = nil, want repo cap error")
	} else if got := err.Error(); !strings.Contains(got, "scope filter") {
		t.Fatalf("Check() error = %q, want a narrow-the-filter error", got)
	}
}

// An empty scope filter fans out over every active generation plus one
// anchored graph query per repository (#7797: p95 1.648s past the 1.5s
// default budget), so the route requires at least one explicit ?scope=.
func TestDeltaUnionCompletenessCheckRequiresScope(t *testing.T) {
	checker, _ := deltaUnionChecker()
	if _, err := checker.Check(context.Background(), nil); err == nil {
		t.Fatal("Check() error = nil, want scope-required error")
	} else if got := err.Error(); !strings.Contains(got, "scope") {
		t.Fatalf("Check() error = %q, want a pass-a-scope error", got)
	}
}

func TestDeltaUnionCompletenessHandlerRequiresScope(t *testing.T) {
	checker, _ := deltaUnionChecker()
	handler := &Handler{Completeness: checker}

	request := httptest.NewRequest(http.MethodGet, "/api/v0/admin/delta-union/completeness", nil)
	recorder := httptest.NewRecorder()
	handler.deltaUnionCompleteness(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an empty scope filter", recorder.Code)
	}
}

func TestDeltaUnionCompletenessHandlerServesReport(t *testing.T) {
	checker, _ := deltaUnionChecker()
	handler := &Handler{Completeness: checker}

	request := httptest.NewRequest(http.MethodGet, "/api/v0/admin/delta-union/completeness?scope=scope-1", nil)
	recorder := httptest.NewRecorder()
	handler.deltaUnionCompleteness(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var report map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	repos, _ := report["repos"].([]any)
	if len(repos) != 1 {
		t.Fatalf("repos = %v, want one entry", report["repos"])
	}
}

func TestDeltaUnionCompletenessHandlerUnavailableWithoutChecker(t *testing.T) {
	handler := &Handler{}

	request := httptest.NewRequest(http.MethodGet, "/api/v0/admin/delta-union/completeness", nil)
	recorder := httptest.NewRecorder()
	handler.deltaUnionCompleteness(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

var _ querycontract.GraphQuery = (*stubDeltaUnionGraph)(nil)
