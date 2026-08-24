// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestEdgeWriterRetractEdgesSiblingNonDeltaBatchIgnoresUnmarkedLegacyRows is
// the inheritance / SQL-relationship / shell-exec twin of
// TestEdgeWriterRetractEdgesRationaleNonDeltaBatchIgnoresUnmarkedLegacyRows
// (#6166).
//
// All three are FENCED repo-wide-retract domains that share rationale's shape:
// planRepoWideRetractWork routes unmarked legacy per-edge rows into retractRows
// alongside the per-repo refresh rows, and before #6166 each domain's non-delta
// branch bound the batch-wide collectRepoIDs into a whole-repository DELETE. A
// row that asked for a per-edge write would therefore erase its repository's
// edges across every file while only that batch's rows were rewritten.
//
// The assertion is on the bound repo_ids, not on statement presence. One
// statement (or one per label) covers the whole batch either way, so the
// parameter is the only thing that separates the repository that asked for the
// delete from a bystander swept in beside it -- and a Cypher-text assertion
// stays true even when the binding is empty.
func TestEdgeWriterRetractEdgesSiblingNonDeltaBatchIgnoresUnmarkedLegacyRows(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		domain         string
		evidenceSource string
	}{
		{"inheritance", reducer.DomainInheritanceEdges, "reducer/inheritance"},
		{"sql_relationships", reducer.DomainSQLRelationships, "reducer/sql-relationships"},
		{"shell_exec", reducer.DomainShellExec, "reducer/shell-exec"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// sqlSequentialRecordingExecutor doubles as the OrphanSweepReader
			// shell exec's retract requires; the other two never read.
			executor := &sqlSequentialRecordingExecutor{readConnected: map[string]bool{}}
			writer := NewEdgeWriter(executor, 0)
			writer.Reader = executor

			rows := []reducer.SharedProjectionIntentRow{
				// The repository that actually asked for a whole-repository
				// retract. Its DELETE must still run.
				wholeScopeRefreshRetractRow("refresh-full", "repo-full"),
				{
					// An unmarked legacy per-edge row: an ordinary edge upsert
					// carrying neither delta_projection nor the refresh
					// intent_type. planRepoWideRetractWork routes exactly this
					// shape into retractRows so it drains instead of deferring
					// forever (reducer/shared_projection_worker_refresh_fence.go).
					IntentID:     "legacy-edge",
					RepositoryID: "repo-legacy",
					Payload: map[string]any{
						"repo_id":     "repo-legacy",
						"action":      "upsert",
						"source_path": "src/other.go",
						"child_path":  "src/other.go",
					},
				},
			}

			if err := writer.RetractEdges(context.Background(), tc.domain, rows, tc.evidenceSource); err != nil {
				t.Fatalf("RetractEdges() error = %v", err)
			}

			// Positive and negative halves in one assertion: repo-full must be
			// bound (the retract still runs) and repo-legacy must not be (the
			// bystander is not swept in).
			assertBoundRepoIDs(t, executor.calls, []string{"repo-full"})
		})
	}
}
