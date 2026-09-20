// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// mixedRunsOnRetryExecutor records the complete main group on each attempt.
// A first-attempt commit failure represents an atomic rollback, so the
// writer must replay every main route, not only the RUNS_ON cleanup or
// upsert. Evidence-artifact statements run sequentially after the main group
// commits (#6184 run15: same-transaction MATCHes miss the main batch's
// in-transaction MERGEs on NornicDB), so single writes are expected exactly
// for the artifact batch.
type mixedRunsOnRetryExecutor struct {
	groups       [][]sourcecypher.Statement
	snapshots    [][]byte
	singleWrites []sourcecypher.Statement
}

func (e *mixedRunsOnRetryExecutor) Execute(_ context.Context, stmt sourcecypher.Statement) error {
	e.singleWrites = append(e.singleWrites, stmt)
	return nil
}

func (e *mixedRunsOnRetryExecutor) ExecuteGroup(_ context.Context, statements []sourcecypher.Statement) error {
	snapshot, err := json.Marshal(statements)
	if err != nil {
		return err
	}
	e.snapshots = append(e.snapshots, snapshot)
	e.groups = append(e.groups, append([]sourcecypher.Statement(nil), statements...))
	if len(e.groups) == 1 {
		return &neo4jdriver.Neo4jError{
			Code: "Neo.ClientError.Transaction.TransactionCommitFailed",
			Msg: "commit failed: constraint violation: Constraint violation (UNIQUE on Repository.[id]): " +
				"Node with id=service-repo already exists",
		}
	}
	return nil
}

func TestEdgeWriterMixedRepoDependencyRunsOnGroupReplaysWholeCommit(t *testing.T) {
	t.Parallel()

	inner := &mixedRunsOnRetryExecutor{}
	writer := NewEdgeWriter(&sourcecypher.RetryingExecutor{
		Inner:      inner,
		MaxRetries: 1,
		BaseDelay:  time.Millisecond,
	}, 0)
	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "runs-on",
			RepositoryID: "service-repo",
			Payload: map[string]any{
				"repo_id":           "service-repo",
				"platform_id":       "platform-prod",
				"relationship_type": "RUNS_ON",
				"source_tool":       "argocd",
			},
		},
		{
			IntentID:     "depends-on",
			RepositoryID: "service-repo",
			GenerationID: "gen-1",
			Payload: map[string]any{
				"repo_id":           "service-repo",
				"target_repo_id":    "dependency-repo",
				"relationship_type": "DEPENDS_ON",
				"resolved_id":       "resolved-dependency-1",
				"generation_id":     "gen-1",
				"evidence_artifacts": []map[string]any{{
					"evidence_kind": "DOCKER_COMPOSE_DEPENDS_ON",
					"path":          "compose.yaml",
					"matched_value": "dependency-repo",
				}},
			},
		},
		{
			IntentID:     "deploys-from",
			RepositoryID: "service-repo",
			Payload: map[string]any{
				"repo_id":           "service-repo",
				"target_repo_id":    "deployment-repo",
				"relationship_type": "DEPLOYS_FROM",
			},
		},
	}

	report, err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "resolver/cross-repo")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v, want whole-group retry", err)
	}
	if len(report.UnroutableRows) != 0 {
		t.Fatalf("unroutable rows = %#v, want none", report.UnroutableRows)
	}
	if len(inner.singleWrites) != 1 || len(inner.groups) != 2 {
		t.Fatalf("dispatch = %d single writes and %d group attempts, want 1 and 2", len(inner.singleWrites), len(inner.groups))
	}
	if !bytes.Equal(inner.snapshots[0], inner.snapshots[1]) {
		t.Fatal("replayed group differs from the failed atomic attempt")
	}

	group := inner.groups[1]
	if sourcecypher.AllStatementsAreReplaySafe(group) {
		t.Fatal("mixed RUNS_ON group unexpectedly passed the generic replay classifier")
	}
	// The main group replays wholly without the evidence-artifact batch:
	// artifacts follow sequentially after the main group commits, so the
	// group holds exactly the four main routes in order.
	wantQueries := []string{
		sourcecypher.BatchCanonicalRunsOnLegacyIdentityCleanupCypher,
		sourcecypher.BatchCanonicalRunsOnUpsertCypher,
		sourcecypher.BatchCanonicalRepoDependencyUpsertCypher,
		sourcecypher.BatchCanonicalDeploysFromRepoRelationshipUpsertCypher,
	}
	if len(group) != len(wantQueries) {
		t.Fatalf("group statements = %d, want %d", len(group), len(wantQueries))
	}
	for index, want := range wantQueries {
		if group[index].Cypher != want || group[index].Operation != sourcecypher.OperationCanonicalUpsert {
			t.Fatalf("group[%d] = operation %q, query %q; want canonical upsert with exact route query %q",
				index, group[index].Operation, group[index].Cypher, want)
		}
	}
	if !reflect.DeepEqual(group[0].Parameters["rows"], group[1].Parameters["rows"]) {
		t.Fatal("RUNS_ON cleanup and upsert have different pair rows")
	}
	assertMixedRunsOnRouteRow(t, group[0], "service-repo", "platform_id", "platform-prod")
	assertMixedRunsOnRouteRow(t, group[2], "service-repo", "target_repo_id", "dependency-repo")
	assertMixedRunsOnRouteRow(t, group[3], "service-repo", "target_repo_id", "deployment-repo")

	// The artifact batch follows as one sequential statement carrying the
	// dependency evidence identity.
	single := inner.singleWrites[0]
	if single.Cypher != sourcecypher.BatchCanonicalRepoEvidenceArtifactUpsertCypher || single.Operation != sourcecypher.OperationCanonicalUpsert {
		t.Fatalf("artifact single = operation %q, query %q; want the artifact upsert", single.Operation, single.Cypher)
	}
	assertMixedRunsOnRouteRow(t, single, "service-repo", "target_repo_id", "dependency-repo")
	artifactRows := single.Parameters["rows"].([]map[string]any)
	if artifactRows[0]["artifact_id"] == "" || artifactRows[0]["resolved_id"] != "resolved-dependency-1" {
		t.Fatalf("artifact row = %#v, want the dependency evidence identity", artifactRows[0])
	}
}

func assertMixedRunsOnRouteRow(t *testing.T, statement sourcecypher.Statement, repoID, targetKey, targetID string) {
	t.Helper()
	rows, ok := statement.Parameters["rows"].([]map[string]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("route rows = %#v, want one", statement.Parameters["rows"])
	}
	if rows[0]["repo_id"] != repoID || rows[0][targetKey] != targetID ||
		rows[0]["evidence_source"] != "resolver/cross-repo" {
		t.Fatalf("route row = %#v, want repo %q, %s %q, cross-repo evidence", rows[0], repoID, targetKey, targetID)
	}
}
