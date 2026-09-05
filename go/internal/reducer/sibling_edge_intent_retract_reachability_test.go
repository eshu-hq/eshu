// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/inheritance"
	"github.com/eshu-hq/eshu/go/internal/reducer/sqlrelationship"
)

// wholeScopeRetractReachabilityCase describes one FENCED repo-wide-retract
// domain's production emitter, so the three siblings of the rationale
// reachability proof can share one body (#6166).
type wholeScopeRetractReachabilityCase struct {
	name      string
	domain    string
	edgeRow   func(repoID string) map[string]any
	buildRows func(edges []map[string]any, repoIDs []string, ctxByRepo map[string]ProjectionContext, at time.Time) []SharedProjectionIntentRow
}

func wholeScopeRetractReachabilityCases() []wholeScopeRetractReachabilityCase {
	return []wholeScopeRetractReachabilityCase{
		{
			name:   "inheritance",
			domain: DomainInheritanceEdges,
			edgeRow: func(repoID string) map[string]any {
				return map[string]any{
					"repo_id": repoID, "child_path": "src/child.go",
					"child_entity_id": "c:" + repoID, "parent_entity_id": "p:" + repoID,
					"relationship_type": "INHERITS",
				}
			},
			buildRows: func(edges []map[string]any, repoIDs []string, ctxByRepo map[string]ProjectionContext, at time.Time) []SharedProjectionIntentRow {
				return inheritance.BuildSharedIntentRows(edges, inheritance.DeltaScope{}, repoIDs, ctxByRepo, at)
			},
		},
		{
			name:   "sql_relationships",
			domain: DomainSQLRelationships,
			edgeRow: func(repoID string) map[string]any {
				return map[string]any{
					"repo_id": repoID, "source_path": "db/schema.sql",
					"source_entity_id": "s:" + repoID, "target_entity_id": "t:" + repoID,
					"relationship_type": "QUERIES_TABLE",
				}
			},
			buildRows: func(edges []map[string]any, repoIDs []string, ctxByRepo map[string]ProjectionContext, at time.Time) []SharedProjectionIntentRow {
				return sqlrelationship.BuildSharedIntentRows(edges, sqlrelationship.DeltaScope{}, repoIDs, ctxByRepo, at)
			},
		},
		{
			name:   "shell_exec",
			domain: DomainShellExec,
			edgeRow: func(repoID string) map[string]any {
				return map[string]any{
					"repo_id": repoID, "source_path": "scripts/run.sh",
					"source_entity_id": "s:" + repoID, "target_entity_id": "shell-command:" + repoID,
				}
			},
			buildRows: func(edges []map[string]any, repoIDs []string, ctxByRepo map[string]ProjectionContext, at time.Time) []SharedProjectionIntentRow {
				return buildShellExecSharedIntentRows(edges, sqlrelationship.DeltaScope{}, repoIDs, ctxByRepo, at)
			},
		},
	}
}

// TestSiblingProductionIntentsNeverReachRetractAsUnmarkedRows is the
// reachability counterweight for the three domains that mirror rationale's
// non-delta narrowing (#6166): inheritance, SQL relationships, shell exec.
//
// Each now binds collectWholeScopeRefreshRepoIDs on its non-delta branch
// (storage/cypher/edge_writer_retract.go), so a retract row without the refresh
// intent_type contributes nothing. That narrowing is only safe while no
// production emitter can produce such a row. It cannot: each domain's
// buildXSharedIntentRows stamps retract_via_refresh on every per-edge intent,
// and planRepoWideRetractWork routes a marked per-edge row to writeRows, never
// to retractRows -- so only the refresh rows, which all carry intent_type,
// retract.
//
// The durable store is the other half, and it needs a different argument.
// This test proves what CURRENT emitters do; it cannot prove the queue holds no
// pre-fence row written before the marker existed. The basis for that is that
// each of these domains was promoted onto the shared-intent path AFTER the
// refresh fence (#2924) landed, so no generation of these rows has ever been
// written by an emitter that did not stamp the marker.
//
// Do NOT restate this as "the commit that created the file is the only commit
// that touched retractViaRefreshKey". That reading is wrong and was corrected
// on #6233: these files' --diff-filter=A commits are recent cmd/eshu
// extractions (rationale_edge_intents.go was created by 5836de3aae on
// 2026-08-18, "extract the repository-selector matcher from cmd/eshu"), so file
// creation says nothing about when the rows were emitted.
//
// Because that argument rests on history rather than on something a test can
// re-derive, the residual risk is covered at runtime instead: if a batch ever
// does arrive with no marked row, EdgeWriter.logWholeScopeRetractSkipped warns
// with the domain and the row count rather than skipping the DELETE silently.
//
// This is the test that turns each mirror from a guess into a proof. If an
// emitter stops stamping the marker, or the marker stops surviving the durable
// round trip, this goes red and that domain's over-delete becomes live.
func TestSiblingProductionIntentsNeverReachRetractAsUnmarkedRows(t *testing.T) {
	t.Parallel()

	for _, tc := range wholeScopeRetractReachabilityCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			const repoA, repoB = "repo-a", "repo-b"
			ctxByRepo := map[string]ProjectionContext{
				repoA: {ScopeID: "scope-a", SourceRunID: "run-1", GenerationID: "gen-1"},
				repoB: {ScopeID: "scope-b", SourceRunID: "run-1", GenerationID: "gen-1"},
			}
			edges := []map[string]any{tc.edgeRow(repoA), tc.edgeRow(repoB)}
			rows := tc.buildRows(edges, []string{repoA, repoB}, ctxByRepo,
				time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC))
			if len(rows) != 4 {
				t.Fatalf("emitted intents = %d, want 4 (2 refresh + 2 per-edge)", len(rows))
			}

			plan, err := planRepoWideRetractWork(
				context.Background(),
				tc.domain,
				roundTripPayloads(t, rows),
				rejectingFenceLookup{t: t},
				nil,
				nil,
			)
			if err != nil {
				t.Fatalf("planRepoWideRetractWork: %v", err)
			}

			// Positive half first: the refresh rows must still retract, or a
			// wholly inert plan would satisfy the negative half vacuously.
			if len(plan.retractRows) != 2 {
				t.Fatalf("retractRows = %d, want 2 (one refresh per repository)", len(plan.retractRows))
			}
			if len(plan.writeRows) != 2 {
				t.Fatalf("writeRows = %d, want 2 (both per-edge rows write this cycle)", len(plan.writeRows))
			}

			// Negative half: nothing reaching the retract may lack the
			// intent_type the narrowed dispatch now requires.
			for _, row := range plan.retractRows {
				if payloadStr(row.Payload, "intent_type") != RepoRefreshIntentType {
					t.Fatalf("intent %s reached retractRows without intent_type=%q (payload %#v); "+
						"the %s whole-scope retract would bind it into a repo-wide DELETE",
						row.IntentID, RepoRefreshIntentType, row.Payload, tc.domain)
				}
			}
		})
	}
}

// TestSiblingPerEdgeIntentsCarryRefreshFenceMarkerAfterRoundTrip is the
// narrower pin under the reachability test above: the marker itself, read back
// exactly as the worker reads it, after the durable JSON round trip.
func TestSiblingPerEdgeIntentsCarryRefreshFenceMarkerAfterRoundTrip(t *testing.T) {
	t.Parallel()

	for _, tc := range wholeScopeRetractReachabilityCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			const repoID = "repo-marker"
			rows := tc.buildRows(
				[]map[string]any{tc.edgeRow(repoID)},
				[]string{repoID},
				map[string]ProjectionContext{repoID: {ScopeID: "scope-x", SourceRunID: "run-1", GenerationID: "gen-1"}},
				time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC),
			)

			perEdge := 0
			for _, row := range roundTripPayloads(t, rows) {
				if isRepoRefreshRow(row) {
					continue
				}
				perEdge++
				if !rowUsesRefreshFence(row) {
					t.Fatalf("per-edge intent %s lost its %s marker across the durable round trip (payload %#v)",
						row.IntentID, retractViaRefreshKey, row.Payload)
				}
			}
			if perEdge != 1 {
				t.Fatalf("per-edge intents = %d, want 1", perEdge)
			}
		})
	}
}
