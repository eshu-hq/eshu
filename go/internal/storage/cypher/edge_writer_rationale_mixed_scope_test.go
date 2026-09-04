// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestEdgeWriterRetractEdgesRationaleMixedBatchRetractsBothDeltaAndWholeScopeRepos
// is the #5998 review F6 shim proof at the RetractEdges dispatch layer.
// rationale.WholeScopePartitionKey hashes each repository's whole-scope
// refresh to exactly one partition among PartitionCount buckets, but with far
// more repositories than buckets, two different repositories' refresh rows
// can legitimately hash into the SAME partition and be selected into the SAME
// ProcessPartitionOnce batch (batchLimit defaults to 100). After
// buildRationaleRefreshIntents gates delta_projection per repository (review
// F6, rationale_edge_intents.go), a batch can contain one repo's row correctly
// flagged delta_projection:true and a sibling repo's row correctly carrying no
// delta_projection key at all. RetractEdges' DomainRationaleEdges branch MUST
// retract BOTH: the delta row via the file-scoped statements, and the
// non-delta row via the probe-guarded whole-scope retract -- not silently drop
// the non-delta repo just because collectDeltaFilePaths saw hasDeltaScope=true
// from its sibling.
func TestEdgeWriterRetractEdgesRationaleMixedBatchRetractsBothDeltaAndWholeScopeRepos(t *testing.T) {
	t.Parallel()

	executor := &probeGuardRecordingExecutor{probeFound: true}
	writer := NewEdgeWriter(executor, 0)
	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "refresh-delta",
			RepositoryID: "repo-delta",
			Payload: map[string]any{
				"repo_id":          "repo-delta",
				"intent_type":      reducer.RepoRefreshIntentType,
				"delta_projection": true,
				"delta_file_paths": []string{"/repo/src/handler.go"},
			},
		},
		{
			// A full-generation sibling in the SAME batch: no delta_projection
			// key at all, exactly buildRationaleRefreshIntents' post-F6 shape
			// for a repo with no entry in deltaScope.filePathsByRepoID. It
			// still carries the refresh intent_type, because every intent that
			// builder emits does (rationale_edge_intents.go).
			IntentID:     "refresh-full",
			RepositoryID: "repo-full",
			Payload: map[string]any{
				"repo_id":     "repo-full",
				"intent_type": reducer.RepoRefreshIntentType,
			},
		},
	}

	if err := writer.RetractEdges(context.Background(), reducer.DomainRationaleEdges, rows, "reducer/rationale"); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}

	if len(executor.executeCalls) == 0 {
		t.Fatal("no DELETE statements executed at all")
	}
	deltaStatementCount := 0
	for _, stmt := range executor.executeCalls {
		if !strings.Contains(stmt.Cypher, "target.path IN $file_paths") {
			continue
		}
		deltaStatementCount++
	}
	if deltaStatementCount == 0 {
		t.Fatal("repo-delta's file-scoped delta retract never ran")
	}

	// repo-full must reach the probe-guarded whole-scope path -- if it was
	// silently dropped from the mixed batch, ExecuteProbe is never called.
	if len(executor.probeCalls) == 0 {
		t.Fatal("repo-full's whole-scope retract never probed -- silently dropped from the mixed batch " +
			"just because its sibling repo-delta made collectDeltaFilePaths report hasDeltaScope=true")
	}
	sawWholeScopeDelete := false
	for _, stmt := range executor.executeCalls {
		if strings.Contains(stmt.Cypher, "rationale.repo_id IN $repo_ids") {
			sawWholeScopeDelete = true
		}
	}
	if !sawWholeScopeDelete {
		t.Fatal("repo-full's whole-scope DELETE (rationale.repo_id IN $repo_ids) never ran")
	}
}

// TestEdgeWriterRetractEdgesRationaleMixedBatchIgnoresUnmarkedLegacyRows is the
// negative half of the mixed-batch contract, and it guards a real over-delete.
//
// "Carries no delta_projection" is NOT the same as "is a whole-scope refresh".
// A batch can also contain unmarked legacy PER-EDGE rows: planRepoWideRetractWork
// deliberately routes those into retractRows so they drain instead of deferring
// forever (shared_projection_worker_refresh_fence.go), and ProcessPartitionOnce
// passes every row as retractRows when no refresh fence is configured at all.
// Those rows carry no delta_projection key either.
//
// If the whole-scope collector keyed only on the absence of delta_projection, a
// legacy per-edge row would hand its repository a
// `rationale.repo_id IN $repo_ids` DELETE that erases that repository's ENTIRE
// EXPLAINS set across every file, while only this batch's rows get rewritten.
// The pre-#5998 file-scoped path never did that, so it would be a new
// over-delete introduced by the guard rather than by anything the repository
// asked for.
func TestEdgeWriterRetractEdgesRationaleMixedBatchIgnoresUnmarkedLegacyRows(t *testing.T) {
	t.Parallel()

	executor := &probeGuardRecordingExecutor{probeFound: true}
	writer := NewEdgeWriter(executor, 0)
	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "refresh-delta",
			RepositoryID: "repo-delta",
			Payload: map[string]any{
				"repo_id":          "repo-delta",
				"intent_type":      reducer.RepoRefreshIntentType,
				"delta_projection": true,
				"delta_file_paths": []string{"/repo/src/handler.go"},
			},
		},
		{
			// An unmarked legacy per-edge row: an ordinary edge upsert, no
			// delta_projection and no refresh intent_type. It must NOT pull a
			// whole-repository DELETE for repo-legacy.
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

	// repo-delta's own file-scoped retract must still run: this test must fail
	// for the right reason, not because the whole branch went inert.
	sawDeltaStatement := false
	for _, stmt := range executor.executeCalls {
		if strings.Contains(stmt.Cypher, "target.path IN $file_paths") {
			sawDeltaStatement = true
		}
	}
	if !sawDeltaStatement {
		t.Fatal("repo-delta's file-scoped delta retract never ran")
	}

	for _, stmt := range executor.executeCalls {
		if strings.Contains(stmt.Cypher, "rationale.repo_id IN $repo_ids") {
			t.Fatalf("an unmarked legacy per-edge row triggered a whole-repository EXPLAINS DELETE (%q, params %#v); "+
				"that erases every file's edges for a repository that only asked for a per-edge write",
				stmt.Cypher, stmt.Parameters)
		}
	}
}
