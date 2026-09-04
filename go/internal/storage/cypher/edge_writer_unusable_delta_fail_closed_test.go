// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/inheritance"
	"github.com/eshu-hq/eshu/go/internal/reducer/sqlrelationship"
)

// Fixture identity for the one repository this file describes.
const (
	unusableDeltaScopeID      = "git-repository-scope:repo-1"
	unusableDeltaGenerationID = "gen-1"
	unusableDeltaRepoID       = "repo-1"
	unusableDeltaChangedPath  = "src/a.py"
)

// unusableDeltaRepositoryFacts is the fact set a delta generation produces when
// the reducer cannot qualify any of its changed paths.
//
// repositoryFactEnvelope (collector/gitrepo/git_content_fact_envelopes.go)
// writes delta_generation and both delta path slices unconditionally once the
// snapshot is a delta, but writes local_path only when repositoryidentity
// resolved one, and never writes "path" at all. The reducer's delta-scope
// builders qualify every relative path against that checkout path, so this fact
// yields a delta-generation repository with zero qualified paths. The same
// outcome is reachable with local_path present: on a symlinked repos root in
// git mode, relativePathsForSnapshotTargets resolves each changed target
// through EvalSymlinks while leaving the repo root unresolved, so every target
// relativizes to a "../"-prefixed path that normalizeSnapshotRelativePaths
// drops -- and delta_generation stays true regardless.
func unusableDeltaRepositoryFacts() []facts.Envelope {
	return []facts.Envelope{
		{
			FactKind: "repository",
			ScopeID:  unusableDeltaScopeID,
			Payload: map[string]any{
				"graph_id":                     unusableDeltaRepoID,
				"graph_kind":                   "repository",
				"repo_id":                      unusableDeltaRepoID,
				"name":                         unusableDeltaRepoID,
				"source_run_id":                "run-1",
				"delta_generation":             true,
				"delta_relative_paths":         []any{unusableDeltaChangedPath},
				"delta_deleted_relative_paths": []any{},
			},
		},
	}
}

// staticFactLoader serves one fixed envelope set to a reducer materialization
// handler. It implements only reducer.FactLoader, so the handlers take their
// ListFacts fallback rather than a kind-filtered load path.
type staticFactLoader struct {
	envelopes []facts.Envelope
}

func (l staticFactLoader) ListFacts(_ context.Context, _, _ string) ([]facts.Envelope, error) {
	return l.envelopes, nil
}

// capturingIntentWriter records the shared-projection intents a materialization
// handler emits. Its single method satisfies all four domains' intent-writer
// interfaces, which are structurally identical.
type capturingIntentWriter struct {
	rows []reducer.SharedProjectionIntentRow
}

func (c *capturingIntentWriter) UpsertIntents(_ context.Context, rows []reducer.SharedProjectionIntentRow) error {
	c.rows = append(c.rows, rows...)
	return nil
}

// unusableDeltaCase binds one fenced repo-wide-retract domain's real
// materialization handler to its real retract dispatch.
type unusableDeltaCase struct {
	name                  string
	retractDomain         string
	evidenceSource        string
	materializationDomain reducer.Domain
	materialize           func(
		ctx context.Context,
		loader reducer.FactLoader,
		writer *capturingIntentWriter,
		intent reducer.Intent,
	) error
}

func unusableDeltaCases() []unusableDeltaCase {
	return []unusableDeltaCase{
		{
			name:                  "inheritance",
			retractDomain:         reducer.DomainInheritanceEdges,
			evidenceSource:        "reducer/inheritance",
			materializationDomain: reducer.DomainInheritanceMaterialization,
			materialize: func(
				ctx context.Context,
				loader reducer.FactLoader,
				writer *capturingIntentWriter,
				intent reducer.Intent,
			) error {
				handler := inheritance.MaterializationHandler{FactLoader: loader, IntentWriter: writer}
				_, err := handler.Handle(ctx, intent)
				return err
			},
		},
		{
			name:                  "rationale",
			retractDomain:         reducer.DomainRationaleEdges,
			evidenceSource:        "reducer/rationale",
			materializationDomain: reducer.DomainRationaleMaterialization,
			materialize: func(
				ctx context.Context,
				loader reducer.FactLoader,
				writer *capturingIntentWriter,
				intent reducer.Intent,
			) error {
				handler := reducer.RationaleEdgeMaterializationHandler{FactLoader: loader, IntentWriter: writer}
				_, err := handler.Handle(ctx, intent)
				return err
			},
		},
		{
			name:                  "sql_relationships",
			retractDomain:         reducer.DomainSQLRelationships,
			evidenceSource:        "reducer/sql-relationships",
			materializationDomain: reducer.DomainSQLRelationshipMaterialization,
			materialize: func(
				ctx context.Context,
				loader reducer.FactLoader,
				writer *capturingIntentWriter,
				intent reducer.Intent,
			) error {
				handler := sqlrelationship.SQLRelationshipMaterializationHandler{FactLoader: loader, IntentWriter: writer}
				_, err := handler.Handle(ctx, intent)
				return err
			},
		},
		{
			name:                  "shell_exec",
			retractDomain:         reducer.DomainShellExec,
			evidenceSource:        "reducer/shell-exec",
			materializationDomain: reducer.DomainShellExecMaterialization,
			materialize: func(
				ctx context.Context,
				loader reducer.FactLoader,
				writer *capturingIntentWriter,
				intent reducer.Intent,
			) error {
				handler := reducer.ShellExecMaterializationHandler{FactLoader: loader, IntentWriter: writer}
				_, err := handler.Handle(ctx, intent)
				return err
			},
		},
	}
}

// refreshIntentRows filters captured intents down to the repo-wide refresh rows
// that own each domain's retract.
func refreshIntentRows(rows []reducer.SharedProjectionIntentRow) []reducer.SharedProjectionIntentRow {
	out := make([]reducer.SharedProjectionIntentRow, 0, 1)
	for _, row := range rows {
		if payloadString(row.Payload, "intent_type") == reducer.RepoRefreshIntentType {
			out = append(out, row)
		}
	}
	return out
}

// TestUnusableDeltaRefreshFailsClosedInsteadOfRetractingRepoWide is the #6216
// P0 regression proof, and it spans both halves of the defect with production
// code on each side: the real reducer materialization handler builds the
// refresh intent from a production-shaped delta generation, and the real
// EdgeWriter.RetractEdges dispatches it.
//
// The failure it guards is silent graph data loss. On a delta generation the
// collector replaces the discovered file set with the changed targets alone
// (resolveNativeSnapshotFileSetForTargets, collector/gitrepo/
// git_snapshot_native.go), so the generation carries content-entity facts for
// the CHANGED files only and the per-edge intents re-create only those files'
// edges. If the refresh intent for such a repository degrades to a whole-scope
// refresh, RetractEdges binds it to a repo-wide
// `DELETE ... WHERE <child>.repo_id IN $repo_ids`: every UNCHANGED file's edge
// is deleted and nothing in the generation re-creates it. No error, no dead
// letter, wrong graph.
//
// So the required behavior is fail-closed. collectDeltaFilePaths
// (edge_writer_retract_scope.go) rejects a delta-flagged row with no paths
// BEFORE any statement executes, which retries and then dead-letters. An
// operator can see a dead letter; they cannot see edges that quietly stopped
// existing.
//
// The first assertion is on the executed statements rather than on the intent
// payload, so this test fails for the data loss itself: a repo-wide DELETE
// bound to the delta repository is exactly the deletion that loses the
// unchanged files' edges.
func TestUnusableDeltaRefreshFailsClosedInsteadOfRetractingRepoWide(t *testing.T) {
	t.Parallel()

	for _, tc := range unusableDeltaCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			capture := &capturingIntentWriter{}
			loader := staticFactLoader{envelopes: unusableDeltaRepositoryFacts()}
			intent := reducer.Intent{
				IntentID:     "materialize-1",
				ScopeID:      unusableDeltaScopeID,
				GenerationID: unusableDeltaGenerationID,
				Domain:       tc.materializationDomain,
				EnqueuedAt:   time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC),
			}
			if err := tc.materialize(ctx, loader, capture, intent); err != nil {
				t.Fatalf("materialization handler error = %v", err)
			}

			// Input invariant: the handler must actually have emitted the
			// repo-wide refresh intent that owns this domain's retract, or the
			// dispatch assertions below prove nothing.
			refreshRows := refreshIntentRows(capture.rows)
			if len(refreshRows) != 1 {
				t.Fatalf("materialization emitted %d repo-wide refresh intent(s) from %d intent(s), want 1",
					len(refreshRows), len(capture.rows))
			}
			if got := refreshRows[0].RepositoryID; got != unusableDeltaRepoID {
				t.Fatalf("refresh intent repository = %q, want %q", got, unusableDeltaRepoID)
			}

			// sqlSequentialRecordingExecutor doubles as the OrphanSweepReader
			// shell exec's retract requires; the other three never read.
			executor := &sqlSequentialRecordingExecutor{readConnected: map[string]bool{}}
			writer := NewEdgeWriter(executor, 0)
			writer.Reader = executor

			err := writer.RetractEdges(ctx, tc.retractDomain, refreshRows, tc.evidenceSource)

			for _, stmt := range executor.calls {
				repoIDs, ok := stmt.Parameters["repo_ids"].([]string)
				if !ok {
					continue
				}
				for _, repoID := range repoIDs {
					if repoID != unusableDeltaRepoID {
						continue
					}
					t.Fatalf("a repo-wide retract ran bound to delta-generation repository %q "+
						"(cypher %q). This generation re-creates only %q, so that DELETE removes every "+
						"unchanged file's edge with nothing to restore it -- silently, with no error and "+
						"no dead letter. The retract must fail closed instead.",
						repoID, stmt.Cypher, unusableDeltaChangedPath)
				}
			}

			if err == nil {
				t.Fatalf("RetractEdges() = nil for a delta-generation repository with no qualified paths "+
					"(%d statement(s) executed); the unusable delta must fail closed so an operator sees a "+
					"dead letter instead of a silently narrowed graph", len(executor.calls))
			}
			if !strings.Contains(err.Error(), "delta retract requires delta_file_paths") {
				t.Fatalf("RetractEdges() error = %v, want it to name the empty delta_file_paths rejection", err)
			}
			// The dead letter is the whole point of failing closed, so it has
			// to say which repository an operator should look at.
			if !strings.Contains(err.Error(), unusableDeltaRepoID) {
				t.Fatalf("RetractEdges() error = %v, want it to name repository %q", err, unusableDeltaRepoID)
			}
			if len(executor.calls) != 0 {
				t.Fatalf("executed %d statement(s) before rejecting the payload, want 0", len(executor.calls))
			}
		})
	}
}
