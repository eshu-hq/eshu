// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// siblingDeltaDispatchCase names one of the three fenced repo-wide-retract
// domains whose delta branch returned before any whole-scope retract could run,
// plus the Cypher fragment that identifies its file-scoped statement. Rationale
// is deliberately absent: it already carries this branch (#5998 review F6) and
// has its own tests in edge_writer_rationale_mixed_scope_test.go.
type siblingDeltaDispatchCase struct {
	name           string
	domain         string
	evidenceSource string
	deltaFragment  string
}

func siblingDeltaDispatchCases() []siblingDeltaDispatchCase {
	return []siblingDeltaDispatchCase{
		{
			name:           "inheritance",
			domain:         reducer.DomainInheritanceEdges,
			evidenceSource: "reducer/inheritance",
			deltaFragment:  "child.path IN $file_paths",
		},
		{
			name:           "sql_relationships",
			domain:         reducer.DomainSQLRelationships,
			evidenceSource: "reducer/sql-relationships",
			deltaFragment:  "UNWIND $file_paths AS file_path",
		},
		{
			name:           "shell_exec",
			domain:         reducer.DomainShellExec,
			evidenceSource: "reducer/shell-exec",
			deltaFragment:  "UNWIND $file_paths AS file_path",
		},
	}
}

// TestEdgeWriterRetractEdgesSiblingMixedBatchRetractsBothDeltaAndWholeScopeRepos
// is the inheritance / SQL-relationship / shell-exec twin of
// TestEdgeWriterRetractEdgesRationaleMixedBatchRetractsBothDeltaAndWholeScopeRepos
// (#5998 review F6), and it pins a LOST RETRACT rather than an over-delete.
//
// Every repository's whole-scope refresh is emitted under its own partition
// key, but SelectPartitionBatch selects by partition ID -- hashtext(key) modulo
// PartitionCount -- so one batch routinely carries refresh rows for many
// repositories (batchLimit defaults to 100). A repository on a delta generation
// and a repository on a full generation therefore land in the same batch
// whenever their keys hash to the same bucket, which is the ordinary case at
// corpus scale.
//
// Before this change the three domains above returned as soon as
// collectDeltaFilePaths reported hasDeltaScope=true, so the full-generation
// sibling never reached a retract at all: its stale edges survived while only
// the batch's rows were rewritten. Nothing else issues that retract -- the
// refresh intent owns it -- and nothing errors or dead-letters, so the graph
// quietly keeps edges the generation removed.
//
// The assertion is on the bound repo_ids, not on statement presence: the delta
// statements execute either way, and a whole-scope statement built over an
// empty binding reads identically in Cypher to one that retracts a repository.
func TestEdgeWriterRetractEdgesSiblingMixedBatchRetractsBothDeltaAndWholeScopeRepos(t *testing.T) {
	t.Parallel()

	for _, tc := range siblingDeltaDispatchCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// sqlSequentialRecordingExecutor doubles as the OrphanSweepReader
			// shell exec's retract requires; the other two never read.
			executor := &sqlSequentialRecordingExecutor{readConnected: map[string]bool{}}
			writer := NewEdgeWriter(executor, 0)
			writer.Reader = executor

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
				// A full-generation sibling in the SAME batch: the refresh
				// intent_type every refresh builder stamps, and no
				// delta_projection key at all.
				wholeScopeRefreshRetractRow("refresh-full", "repo-full"),
			}

			if err := writer.RetractEdges(context.Background(), tc.domain, rows, tc.evidenceSource); err != nil {
				t.Fatalf("RetractEdges() error = %v", err)
			}

			// The delta repository's own retract must still run: this test has
			// to fail for the lost whole-scope retract, not because the delta
			// branch went inert.
			sawDeltaStatement := false
			for _, stmt := range executor.calls {
				if strings.Contains(stmt.Cypher, tc.deltaFragment) {
					sawDeltaStatement = true
				}
			}
			if !sawDeltaStatement {
				t.Fatalf("repo-delta's file-scoped retract never ran (no statement contained %q)", tc.deltaFragment)
			}

			assertBoundRepoIDs(t, executor.calls, []string{"repo-full"})
		})
	}
}

// TestEdgeWriterRetractEdgesSiblingEmptyDeltaFilePathsFailsThePartition pins
// the dispatch guarantee the #6216 fix depends on: a refresh intent carrying
// delta_projection:true with an empty delta_file_paths fails the partition
// before any statement runs, so it retries and then dead-letters.
//
// That is the intended outcome, not a cost to be engineered away. A repository
// whose delta could not be qualified has no correct retract available here --
// the file-scoped one has nothing to bind, and the repo-wide one deletes every
// unchanged file's edge that this generation's changed-files-only facts cannot
// re-create. See collectDeltaFilePaths (edge_writer_retract_scope.go) and
// TestUnusableDeltaRefreshFailsClosedInsteadOfRetractingRepoWide, which drives
// the real reducer handler into this same dispatch.
func TestEdgeWriterRetractEdgesSiblingEmptyDeltaFilePathsFailsThePartition(t *testing.T) {
	t.Parallel()

	for _, tc := range siblingDeltaDispatchCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			executor := &sqlSequentialRecordingExecutor{readConnected: map[string]bool{}}
			writer := NewEdgeWriter(executor, 0)
			writer.Reader = executor

			rows := []reducer.SharedProjectionIntentRow{
				{
					IntentID:     "refresh-empty-delta",
					RepositoryID: "repo-empty",
					Payload: map[string]any{
						"repo_id":          "repo-empty",
						"intent_type":      reducer.RepoRefreshIntentType,
						"delta_projection": true,
						"delta_file_paths": []string{},
					},
				},
			}

			err := writer.RetractEdges(context.Background(), tc.domain, rows, tc.evidenceSource)
			if err == nil {
				t.Fatal("RetractEdges() returned nil for an empty delta_file_paths payload; " +
					"this test exists to pin that the dispatch rejects it")
			}
			if !strings.Contains(err.Error(), "delta retract requires delta_file_paths") {
				t.Fatalf("RetractEdges() error = %v, want it to name the empty delta_file_paths rejection", err)
			}
			if len(executor.calls) != 0 {
				t.Fatalf("executed %d statement(s) before rejecting the payload, want 0", len(executor.calls))
			}
		})
	}
}
