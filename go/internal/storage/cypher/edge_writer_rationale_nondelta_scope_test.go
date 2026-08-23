// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// rationaleWholeScopeRetractRepoIDs returns the repo_ids parameter bound by
// every whole-repository EXPLAINS DELETE the executor recorded. The
// whole-repository retract is one statement for the whole batch, so asserting
// on statement PRESENCE cannot tell a legitimate repository's retract from a
// bystander repository swept in beside it -- only the bound parameter can.
func rationaleWholeScopeRetractRepoIDs(t *testing.T, stmts []Statement) []string {
	t.Helper()
	var bound []string
	for _, stmt := range stmts {
		if !strings.Contains(stmt.Cypher, "rationale.repo_id IN $repo_ids") {
			continue
		}
		repoIDs, ok := stmt.Parameters["repo_ids"].([]string)
		if !ok {
			t.Fatalf("whole-repository retract bound repo_ids of type %T, want []string (stmt %q)",
				stmt.Parameters["repo_ids"], stmt.Cypher)
		}
		bound = append(bound, repoIDs...)
	}
	return bound
}

// TestEdgeWriterRetractEdgesRationaleNonDeltaBatchIgnoresUnmarkedLegacyRows is
// the non-delta-branch twin of
// TestEdgeWriterRetractEdgesRationaleMixedBatchIgnoresUnmarkedLegacyRows
// (#6166).
//
// The mixed-batch branch runs only when some row in the batch carries
// delta_projection. When NO row does, RetractEdges falls through to the shared
// repo-id path and binds collectRepoIDs(rows) -- the whole batch, with no
// intent_type filter -- straight into the whole-repository
// `rationale.repo_id IN $repo_ids` DELETE.
//
// So the same unmarked legacy per-edge row the mixed-batch collector is
// careful to exclude still pulls a whole-repository retract here: every
// EXPLAINS edge for that repository, across every file, is deleted, while only
// this batch's single per-edge row is rewritten. The batch's legitimate
// whole-scope refresh row (repo-full) must keep its retract; the bystander
// (repo-legacy) must not acquire one.
func TestEdgeWriterRetractEdgesRationaleNonDeltaBatchIgnoresUnmarkedLegacyRows(t *testing.T) {
	t.Parallel()

	executor := &probeGuardRecordingExecutor{probeFound: true}
	writer := NewEdgeWriter(executor, 0)
	rows := []reducer.SharedProjectionIntentRow{
		{
			// A legitimate whole-scope refresh on a full generation: this is
			// the row that asked for a whole-repository retract, and it must
			// still get one.
			IntentID:     "refresh-full",
			RepositoryID: "repo-full",
			Payload: map[string]any{
				"repo_id":     "repo-full",
				"intent_type": reducer.RepoRefreshIntentType,
				"action":      "refresh",
			},
		},
		{
			// An unmarked legacy per-edge row: an ordinary edge upsert with
			// neither delta_projection nor the refresh intent_type.
			// planRepoWideRetractWork routes exactly this shape into
			// retractRows so it drains instead of deferring forever
			// (shared_projection_worker_refresh_fence.go). It asked for a
			// per-edge write, not a whole-repository delete.
			IntentID:     "legacy-edge",
			RepositoryID: "repo-legacy",
			Payload: map[string]any{
				"repo_id":     "repo-legacy",
				"action":      "upsert",
				"target_path": "/repo/src/other.go",
			},
		},
	}

	if err := writer.RetractEdges(context.Background(), reducer.DomainRationaleEdges, rows, "reducer/rationale"); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}

	bound := rationaleWholeScopeRetractRepoIDs(t, executor.executeCalls)

	// Positive half first, so this test fails for the right reason rather than
	// because the whole branch went inert: repo-full asked for a
	// whole-repository refresh and must still get its DELETE.
	sawRepoFull := false
	for _, repoID := range bound {
		if repoID == "repo-full" {
			sawRepoFull = true
		}
	}
	if !sawRepoFull {
		t.Fatalf("repo-full's whole-repository EXPLAINS DELETE never ran; bound repo_ids = %v", bound)
	}

	// Negative half: the bystander must not be swept into that same DELETE.
	for _, repoID := range bound {
		if repoID == "repo-legacy" {
			t.Fatalf("an unmarked legacy per-edge row was bound into the whole-repository EXPLAINS DELETE "+
				"(repo_ids = %v); that erases every file's edges for a repository that only asked for a "+
				"per-edge write", bound)
		}
	}
}

// rationaleNonDeltaBenchBatch builds a production-shaped non-delta rationale
// retract batch: one whole-scope refresh row per repository, at the default
// shared-projection BatchLimit of 100 (reducer/shared_projection_runner.go).
// The refresh fence routes only refresh rows into retractRows for this domain,
// so this is the batch the non-delta branch actually sees.
func rationaleNonDeltaBenchBatch(size int) []reducer.SharedProjectionIntentRow {
	rows := make([]reducer.SharedProjectionIntentRow, 0, size)
	for i := range size {
		repoID := "repo-" + string(rune('a'+i%26)) + "-" + string(rune('0'+i/26))
		rows = append(rows, reducer.SharedProjectionIntentRow{
			RepositoryID: repoID,
			Payload: map[string]any{
				"repo_id":     repoID,
				"intent_type": reducer.RepoRefreshIntentType,
				"action":      "refresh",
			},
		})
	}
	return rows
}

// BenchmarkCollectRepoIDsRationaleNonDeltaBatch measures the collector the
// non-delta branch binds today (#6166).
func BenchmarkCollectRepoIDsRationaleNonDeltaBatch(b *testing.B) {
	rows := rationaleNonDeltaBenchBatch(100)
	b.ReportAllocs()
	for b.Loop() {
		_ = collectRepoIDs(rows)
	}
}

// BenchmarkCollectWholeScopeRefreshRepoIDsRationaleNonDeltaBatch measures the
// intent_type-filtered candidate on the identical batch, so the two figures are
// comparable. The added work is one map lookup and one string compare per row;
// no Cypher shape changes, and the bound $repo_ids list can only narrow.
func BenchmarkCollectWholeScopeRefreshRepoIDsRationaleNonDeltaBatch(b *testing.B) {
	rows := rationaleNonDeltaBenchBatch(100)
	b.ReportAllocs()
	for b.Loop() {
		_ = collectWholeScopeRefreshRepoIDs(rows)
	}
}
