// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"

	gg "github.com/eshu-hq/eshu/go/internal/goldengate"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

type stubDeltaUnionGraphFiles struct {
	files map[string]map[string]int64
	err   error
}

func (s *stubDeltaUnionGraphFiles) DeltaUnionRepoFiles(_ context.Context, repoID string) (map[string]int64, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.files[repoID], nil
}

func deltaUnionPhaseSnapshot() pgstatus.DeltaUnionActiveSnapshot {
	return pgstatus.DeltaUnionActiveSnapshot{
		Rows: []gg.DeltaUnionActiveRow{
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
		Deltas: map[string]gg.DeltaUnionDeltaPaths{
			"scope-1": {BaselineCommit: "commit-a", Changed: []string{"keep.go", "x.go"}, Deleted: []string{"w.go"}},
		},
	}
}

func TestEvaluateDeltaUnionSnapshotPasses(t *testing.T) {
	var r Report
	graph := &stubDeltaUnionGraphFiles{files: map[string]map[string]int64{
		"repo-1": {"keep.go": 0, "x.go": 0},
	}}
	if err := evaluateDeltaUnionSnapshot(context.Background(), deltaUnionPhaseSnapshot(), graph, &r); err != nil {
		t.Fatalf("evaluate() error = %v, want nil", err)
	}
	if r.Failed() {
		t.Fatalf("report failed, want pass: %+v", r)
	}
}

func TestEvaluateDeltaUnionSnapshotMismatchFails(t *testing.T) {
	var r Report
	graph := &stubDeltaUnionGraphFiles{files: map[string]map[string]int64{
		"repo-1": {"keep.go": 0, "x.go": 0, "w.go": 0},
	}}
	if err := evaluateDeltaUnionSnapshot(context.Background(), deltaUnionPhaseSnapshot(), graph, &r); err != nil {
		t.Fatalf("evaluate() error = %v, want nil", err)
	}
	if !r.Failed() {
		t.Fatal("report passed, want required failure on resurrected file")
	}
}

func TestEvaluateDeltaUnionSnapshotGraphErrorFails(t *testing.T) {
	var r Report
	graph := &stubDeltaUnionGraphFiles{err: errors.New("bolt unavailable")}
	if err := evaluateDeltaUnionSnapshot(context.Background(), deltaUnionPhaseSnapshot(), graph, &r); err == nil {
		t.Fatal("evaluate() error = nil, want graph read failure")
	}
}

func TestDeltaUnionPhaseRegistered(t *testing.T) {
	phases := phaseSet("all")
	if !phases["delta-union"] {
		t.Fatal("phaseSet(all) omits delta-union")
	}
	if phases["backend-diff"] {
		t.Fatal("phaseSet(all) must keep excluding opt-in backend-diff")
	}
	if needsSnapshot(map[string]bool{"delta-union": true}) {
		t.Fatal("pure -phase=delta-union must skip the snapshot load")
	}
}
