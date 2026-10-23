// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8780 disposable PostgreSQL proof that a cumulative delta over an active delta (diffed from the newest activated full, its fence commit the active delta's) projects over that full through the production projector service and activates, with no base-missing refusal and no reindex watermark; isolated bootstrapped schema per test; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

// Live proof for #8780 on the #8779 harness (delta_union_projection_live_test.go).

import (
	"bytes"
	"context"
	"log/slog"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

const (
	deltaChainCommitD2 = "dddddddddddddddddddddddddddddddddddd8780"
	deltaChainGenD2    = "gen-8780-delta-d2"
)

// deltaChainD2 is the cumulative delta at commit D2 over the active delta B:
// keep.go reverts to A's bytes, x.go (added by B) is deleted, other.go
// changes, and y.go is added. Its changed set is diffed from the newest
// activated full A (union base A), its deleted set covers D(A..D2) = {w.go}
// and D(B..D2) = {x.go}, and its fence commit is B's.
var deltaChainD2 = deltaProjGen{
	id: deltaChainGenD2, commit: deltaChainCommitD2,
	baseline: deltaProjCommitB, unionBase: deltaProjCommitA, delta: true,
	files: []deltaProjFile{
		{"other.go", "package p\nfunc other2() {}\n", "other2"},
		{"y.go", "package p\nfunc y() {}\n", "y"},
	},
	changed: []string{"other.go", "y.go"},
	deleted: []string{"w.go", "x.go"},
}

// deltaUnionB is delta B recorded the #8780 way: diffed from the active full
// A, so its union base and fence commit coincide.
func deltaUnionB() deltaProjGen {
	gen := deltaProjDeltaB
	gen.unionBase = deltaProjCommitA
	return gen
}

// reindexRequests counts the scope's repository_reindex_requests rows; the
// table exists only once a watermark was ever requested.
func (h *deltaProjHarness) reindexRequests() int {
	h.t.Helper()
	var exists bool
	if err := h.proof.db.QueryRowContext(context.Background(),
		`SELECT to_regclass('repository_reindex_requests') IS NOT NULL`).Scan(&exists); err != nil {
		h.t.Fatalf("probe repository_reindex_requests: %v", err)
	}
	if !exists {
		return 0
	}
	var count int
	if err := h.proof.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM repository_reindex_requests WHERE scope_id = $1`, h.proof.scope.ScopeID).Scan(&count); err != nil {
		h.t.Fatalf("count repository_reindex_requests: %v", err)
	}
	return count
}

// contentBody returns the content store body of one proof file.
func (h *deltaProjHarness) contentBody(rel string) string {
	h.t.Helper()
	bodies := h.queryStrings(`SELECT content FROM content_files WHERE repo_id = $1 AND relative_path = $2`, h.repo.ID, rel)
	if len(bodies) != 1 {
		h.t.Fatalf("content_files rows for %s = %d, want 1", rel, len(bodies))
	}
	return bodies[0]
}

// TestChainedDeltaProjectsOverNewestFullLive is the #8780 AC1 proof: full A
// activates, delta B (union base A, fence A) activates, then the cumulative
// delta D2 (union base A, fence B) activates over A. On main D2's base is
// resolved at its fence commit B, where no full exists, so it refuses
// base-missing and records a reindex watermark that forces a full.
func TestChainedDeltaProjectsOverNewestFullLive(t *testing.T) {
	h := newDeltaProjHarness(t)
	var logs bytes.Buffer
	h.factLog = slog.New(slog.NewJSONHandler(&logs, nil))

	h.commit(deltaProjFullA)
	h.project()
	h.commit(deltaUnionB())
	h.project()
	if row := h.projectorRow(deltaProjGenB); row.generationStatus != "active" {
		t.Fatalf("delta B = %+v, want active before the chained delta", row)
	}

	canonicalBefore := len(h.canonical.calls)
	h.commit(deltaChainD2)
	h.project()

	row := h.projectorRow(deltaChainGenD2)
	if row.workStatus != "succeeded" || row.generationStatus != "active" || row.failureClass != "" {
		t.Fatalf("chained delta D2 = %+v; want it projected over the newest full A and activated "+
			"(failure class %q means its base was resolved at the fence commit)", row, projector.DeltaBaselineBaseMissingClass)
	}
	if active := h.projectorRow(deltaProjGenB); active.generationStatus != "superseded" {
		t.Fatalf("delta B generation status = %q after D2 activated, want superseded", active.generationStatus)
	}
	if got := h.reindexRequests(); got != 0 {
		t.Fatalf("repository_reindex_requests rows = %d, want 0 (no forced full)", got)
	}
	found := strings.Count(logs.String(), `"outcome":"union_base_found"`)
	if found != 2 || strings.Contains(logs.String(), "union_base_missing_refused") {
		t.Fatalf("union decisions: %d union_base_found, want 2 (B and D2) and no refusal; log:\n%s", found, logs.String())
	}

	if len(h.canonical.calls) <= canonicalBefore {
		t.Fatal("chained delta D2 wrote no canonical materialization")
	}
	mat := h.canonical.calls[len(h.canonical.calls)-1]
	if mat.GenerationID != deltaChainGenD2 || !mat.DeltaProjection {
		t.Fatalf("canonical generation = %q, delta projection = %v; want %q and true",
			mat.GenerationID, mat.DeltaProjection, deltaChainGenD2)
	}
	files := make([]string, 0, len(mat.Files))
	for _, file := range mat.Files {
		files = append(files, file.RelativePath)
	}
	sort.Strings(files)
	if got, want := strings.Join(files, ","), "keep.go,other.go,y.go"; got != want {
		t.Fatalf("canonical files = %s, want %s (the tree at D2)", got, want)
	}
	entitiesByFile := make(map[string][]string)
	for _, entity := range mat.Entities {
		entitiesByFile[entity.RelativePath] = append(entitiesByFile[entity.RelativePath], entity.EntityName)
	}
	for file, want := range map[string]string{"keep.go": "keep", "other.go": "other2", "y.go": "y"} {
		if got := entitiesByFile[file]; len(got) != 1 || got[0] != want {
			t.Fatalf("canonical entities under %s = %v, want exactly [%s]", file, got, want)
		}
	}
	if len(entitiesByFile) != 3 {
		t.Fatalf("canonical entities by file = %v, want only keep.go, other.go, and y.go", entitiesByFile)
	}
	if got, want := strings.Join(h.contentFiles(), ","), "keep.go,other.go,y.go"; got != want {
		t.Fatalf("content_files = %s, want %s (x.go deleted since the active commit)", got, want)
	}
	if got, want := h.contentBody("keep.go"), deltaProjFullA.files[0].body; got != want {
		t.Fatalf("content of reverted keep.go = %q, want A's bytes %q", got, want)
	}
}
