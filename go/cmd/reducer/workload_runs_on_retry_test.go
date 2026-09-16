// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// runsOnConflictRunner injects a failure at the graph transaction boundary,
// below the production reducer adapter and its persistent retry executor.
// Its first group attempt rolls back; the replay commits one canonical edge.
type runsOnConflictRunner struct {
	conflict      error
	groupAttempts int
	canonical     map[string]any
	legacyPresent bool
}

func (r *runsOnConflictRunner) RunCypher(_ context.Context, query string, _ map[string]any) error {
	if !strings.Contains(query, "MERGE (p:Platform") {
		return errors.New("unexpected ungrouped materializer statement")
	}
	return nil
}

func (r *runsOnConflictRunner) RunCypherGroup(_ context.Context, statements []cypher.Statement) error {
	r.groupAttempts++
	if len(statements) != 3 ||
		!strings.Contains(statements[0].Cypher, "DELETE rel") ||
		!strings.Contains(statements[1].Cypher, "MERGE (i)-[rel:RUNS_ON {identity_key: 'canonical'}]->(p)") ||
		!strings.Contains(statements[2].Cypher, "SET rel.confidence") {
		return errors.New("unexpected RUNS_ON atomic group shape")
	}
	if r.groupAttempts == 1 {
		return r.conflict
	}
	rows, ok := statements[0].Parameters["rows"].([]map[string]any)
	if !ok || len(rows) != 1 {
		return errors.New("unexpected RUNS_ON group rows")
	}
	row := rows[0]
	r.legacyPresent = false
	r.canonical = map[string]any{
		"identity_key":    "canonical",
		"confidence":      row["platform_confidence"],
		"evidence_source": row["evidence_source"],
		"source_tool":     nil,
	}
	return nil
}

func TestWorkloadRunsOnAtomicGroupReplaysCommitConflicts(t *testing.T) {
	const platformID = "platform:kubernetes:none:production:production:none"
	conflicts := []struct {
		name string
		err  error
	}{
		{
			name: "commit unique conflict",
			err: &neo4jdriver.Neo4jError{
				Code: "Neo.ClientError.Transaction.TransactionCommitFailed",
				Msg: "commit failed: constraint violation: Constraint violation (UNIQUE on Platform.[id]): " +
					"Node with id=" + platformID + " already exists",
			},
		},
		{
			name: "relationship snapshot conflict",
			err: &neo4jdriver.Neo4jError{
				Code: "Neo.ClientError.Statement.SyntaxError",
				Msg:  "UNWIND MERGE chain relationship update failed: not found",
			},
		},
	}
	for _, test := range conflicts {
		t.Run(test.name, func(t *testing.T) {
			runner := &runsOnConflictRunner{conflict: test.err, legacyPresent: true}
			materializer := reducer.NewWorkloadMaterializer(newReducerCypherExecutor(runner, nil))
			projection := &reducer.ProjectionResult{RuntimePlatformRows: []reducer.RuntimePlatformRow{{
				Environment:  "production",
				Confidence:   0.9,
				InstanceID:   "workload-instance:my-api:production",
				PlatformID:   platformID,
				PlatformKind: "kubernetes",
				PlatformName: "production",
			}}}
			result, err := materializer.Materialize(context.Background(), projection)
			if err != nil {
				t.Fatalf("Materialize() error = %v, want replayed atomic group", err)
			}
			if result.RuntimePlatformsWritten != 1 {
				t.Fatalf("RuntimePlatformsWritten = %d, want 1", result.RuntimePlatformsWritten)
			}
			if runner.groupAttempts != 2 {
				t.Fatalf("group attempts = %d, want one failed commit and one replay", runner.groupAttempts)
			}
			if runner.legacyPresent || runner.canonical == nil {
				t.Fatalf("final RUNS_ON state = legacy %t, canonical %v", runner.legacyPresent, runner.canonical)
			}
			if runner.canonical["identity_key"] != "canonical" ||
				runner.canonical["evidence_source"] != reducer.EvidenceSourceWorkloads ||
				runner.canonical["confidence"] != 0.9 ||
				runner.canonical["source_tool"] != nil {
				t.Fatalf("final canonical RUNS_ON tuple = %v", runner.canonical)
			}
		})
	}
}
