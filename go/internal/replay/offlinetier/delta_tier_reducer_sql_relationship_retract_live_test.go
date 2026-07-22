// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Reducer-materialized SQL relationship retract coverage for #5116.
//
// NornicDB v1.1.11 acknowledges a managed transaction containing these
// per-source-label DELETE statements but applies none of them. The same statements
// run as separate auto-commit transactions delete the intended edges. This test
// drives the production EdgeWriter write and retract paths for both repository
// and delta-file scopes and protects scope, evidence, and endpoint-node truth.

package offlinetier_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	sqlRetractInRepoID      = "replay-sql-retract:inscope"
	sqlRetractOutRepoID     = "replay-sql-retract:outscope"
	sqlRetractInPath        = "sql-retract/in/schema.sql"
	sqlRetractOutPath       = "sql-retract/out/schema.sql"
	sqlRetractEvidence      = "reducer/sql-relationships"
	sqlRetractOtherEvidence = "reducer/sql-relationships-other"
)

type sqlRetractFixture struct {
	name        string
	sourceLabel string
	sourceUID   string
	targetLabel string
	targetUID   string
	relType     string
	repoID      string
	path        string
	evidence    string
}

var sqlRetractInScopeFixtures = []sqlRetractFixture{
	{"queries-table", "Function", "sql-retract:fn", "SqlTable", "sql-retract:qt", "QUERIES_TABLE", sqlRetractInRepoID, sqlRetractInPath, sqlRetractEvidence},
	{"references-table", "SqlTable", "sql-retract:fk-table", "SqlTable", "sql-retract:referenced-table", "REFERENCES_TABLE", sqlRetractInRepoID, sqlRetractInPath, sqlRetractEvidence},
	{"view-reads-from-table", "SqlView", "sql-retract:view", "SqlTable", "sql-retract:vrt", "READS_FROM", sqlRetractInRepoID, sqlRetractInPath, sqlRetractEvidence},
	{"function-reads-from-table", "SqlFunction", "sql-retract:sql-fn", "SqlTable", "sql-retract:frt", "READS_FROM", sqlRetractInRepoID, sqlRetractInPath, sqlRetractEvidence},
	{"function-writes-to-table", "SqlFunction", "sql-retract:writer-fn", "SqlTable", "sql-retract:written-table", "WRITES_TO", sqlRetractInRepoID, sqlRetractInPath, sqlRetractEvidence},
	{"has-column", "SqlTable", "sql-retract:table", "SqlColumn", "sql-retract:column", "HAS_COLUMN", sqlRetractInRepoID, sqlRetractInPath, sqlRetractEvidence},
	{"triggers", "SqlTrigger", "sql-retract:trigger-table", "SqlTable", "sql-retract:triggered-table", "TRIGGERS", sqlRetractInRepoID, sqlRetractInPath, sqlRetractEvidence},
	{"executes", "SqlTrigger", "sql-retract:trigger-fn", "SqlFunction", "sql-retract:executed-fn", "EXECUTES", sqlRetractInRepoID, sqlRetractInPath, sqlRetractEvidence},
	{"indexes", "SqlIndex", "sql-retract:index", "SqlTable", "sql-retract:indexed-table", "INDEXES", sqlRetractInRepoID, sqlRetractInPath, sqlRetractEvidence},
	{"migrates", "SqlMigration", "sql-retract:migration", "SqlTable", "sql-retract:migrated-table", "MIGRATES", sqlRetractInRepoID, sqlRetractInPath, sqlRetractEvidence},
}

// TestReducerSQLRelationshipRetractGraphTruth is the failing-then-green live
// regression for the managed multi-DELETE bug on NornicDB v1.1.11.
func TestReducerSQLRelationshipRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 to run the SQL relationship retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	exec, _ := openDeltaLiveBackend(ctx, t)

	cases := []struct {
		name           string
		controlRepoID  string
		controlPath    string
		retractPayload map[string]any
	}{
		{
			name:          "repository scope",
			controlRepoID: sqlRetractOutRepoID,
			controlPath:   sqlRetractOutPath,
		},
		{
			name:          "delta file scope",
			controlRepoID: sqlRetractInRepoID,
			controlPath:   sqlRetractOutPath,
			retractPayload: map[string]any{
				"delta_projection": true,
				"delta_file_paths": []string{sqlRetractInPath},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cleanupSQLRetractScope(ctx, t, exec)
			control := sqlRetractFixture{
				"scope-control", "Function", "sql-retract:control-fn", "SqlTable", "sql-retract:control-table",
				"QUERIES_TABLE", tc.controlRepoID, tc.controlPath, sqlRetractEvidence,
			}
			wrongEvidence := sqlRetractFixture{
				"evidence-control", "Function", "sql-retract:evidence-fn", "SqlTable", "sql-retract:evidence-table",
				"QUERIES_TABLE", sqlRetractInRepoID, sqlRetractInPath, sqlRetractOtherEvidence,
			}
			fixtures := append(append([]sqlRetractFixture{}, sqlRetractInScopeFixtures...), control, wrongEvidence)
			seedSQLRetractNodes(ctx, t, exec, fixtures)

			writer := cypher.NewEdgeWriter(exec, 0)
			writer.SQLRelationshipSequentialWrites = true
			writeSQLRetractFixtures(ctx, t, writer, fixtures)
			for _, fixture := range fixtures {
				assertSQLRetractFixtureCount(ctx, t, exec, fixture, 1, "write")
			}
			writeSQLRetractFixtures(ctx, t, writer, fixtures)
			for _, fixture := range fixtures {
				assertSQLRetractFixtureCount(ctx, t, exec, fixture, 1, "idempotent duplicate write")
			}

			rows := []reducer.SharedProjectionIntentRow{{
				IntentID: "sql-retract", RepositoryID: sqlRetractInRepoID, Payload: tc.retractPayload,
			}}
			if err := writer.RetractEdges(ctx, reducer.DomainSQLRelationships, rows, sqlRetractEvidence); err != nil {
				t.Fatalf("RetractEdges: %v", err)
			}

			for _, fixture := range sqlRetractInScopeFixtures {
				assertSQLRetractFixtureCount(ctx, t, exec, fixture, 0, "retract")
			}
			assertSQLRetractFixtureCount(ctx, t, exec, control, 1, "scope control")
			assertSQLRetractFixtureCount(ctx, t, exec, wrongEvidence, 1, "evidence control")
			for _, fixture := range fixtures {
				for _, uid := range []string{fixture.sourceUID, fixture.targetUID} {
					assertEdgeCount(ctx, t, exec, "MATCH (n {uid: $u}) RETURN count(n)", map[string]any{"u": uid}, 1, "node survives: "+uid)
				}
			}

			if err := writer.RetractEdges(ctx, reducer.DomainSQLRelationships, rows, sqlRetractEvidence); err != nil {
				t.Fatalf("idempotent RetractEdges: %v", err)
			}
			for _, fixture := range sqlRetractInScopeFixtures {
				assertSQLRetractFixtureCount(ctx, t, exec, fixture, 0, "idempotent retract")
			}
			assertSQLRetractFixtureCount(ctx, t, exec, control, 1, "idempotent scope control")
			assertSQLRetractFixtureCount(ctx, t, exec, wrongEvidence, 1, "idempotent evidence control")
		})
	}

	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupSQLRetractScope(cleanCtx, t, exec)
	})
}

func seedSQLRetractNodes(ctx context.Context, t *testing.T, exec liveExecutor, fixtures []sqlRetractFixture) {
	t.Helper()
	for _, fixture := range fixtures {
		stmt := cypher.Statement{
			Cypher:     fmt.Sprintf("CREATE (:%s {uid: $source, repo_id: $repo, path: $path}), (:%s {uid: $target, repo_id: $repo, path: $path})", fixture.sourceLabel, fixture.targetLabel),
			Parameters: map[string]any{"source": fixture.sourceUID, "target": fixture.targetUID, "repo": fixture.repoID, "path": fixture.path},
		}
		if err := exec.Execute(ctx, stmt); err != nil {
			t.Fatalf("seed %s nodes: %v", fixture.name, err)
		}
	}
}

func writeSQLRetractFixtures(ctx context.Context, t *testing.T, writer *cypher.EdgeWriter, fixtures []sqlRetractFixture) {
	t.Helper()
	for _, fixture := range fixtures {
		row := reducer.SharedProjectionIntentRow{
			IntentID: fixture.name, RepositoryID: fixture.repoID,
			Payload: map[string]any{
				"relationship_type": fixture.relType,
				"source_entity_id":  fixture.sourceUID, "source_entity_type": fixture.sourceLabel,
				"target_entity_id": fixture.targetUID, "target_entity_type": fixture.targetLabel,
			},
		}
		if err := writer.WriteEdges(ctx, reducer.DomainSQLRelationships, []reducer.SharedProjectionIntentRow{row}, fixture.evidence); err != nil {
			t.Fatalf("write %s edge: %v", fixture.name, err)
		}
	}
}

func assertSQLRetractFixtureCount(ctx context.Context, t *testing.T, exec liveExecutor, fixture sqlRetractFixture, want int64, phase string) {
	t.Helper()
	query := fmt.Sprintf("MATCH (:%s {uid: $s})-[r:%s]->(:%s {uid: $t}) RETURN count(r)", fixture.sourceLabel, fixture.relType, fixture.targetLabel)
	assertEdgeCount(ctx, t, exec, query, map[string]any{"s": fixture.sourceUID, "t": fixture.targetUID}, want, phase+": "+fixture.name)
}

func cleanupSQLRetractScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	for _, label := range []string{"Function", "SqlColumn", "SqlFunction", "SqlIndex", "SqlMigration", "SqlTable", "SqlTrigger", "SqlView"} {
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher:     fmt.Sprintf("MATCH (n:%s) WHERE n.repo_id IN [$in, $out] DETACH DELETE n", label),
			Parameters: map[string]any{"in": sqlRetractInRepoID, "out": sqlRetractOutRepoID},
		}); err != nil {
			t.Fatalf("cleanup SQL retract %s scope: %v", label, err)
		}
	}
}
