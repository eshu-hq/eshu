// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// runsOnEdgeConflictExecutor fails the first atomic group at commit and
// records the final edge only after the production writer retries the group.
type runsOnEdgeConflictExecutor struct {
	conflict      error
	groupAttempts int
	canonical     map[string]any
	legacyPresent bool
}

func (e *runsOnEdgeConflictExecutor) Execute(context.Context, Statement) error {
	return errors.New("RUNS_ON edge write must use an atomic group")
}

func (e *runsOnEdgeConflictExecutor) ExecuteGroup(_ context.Context, statements []Statement) error {
	e.groupAttempts++
	if len(statements) != 2 ||
		!strings.Contains(statements[0].Cypher, "WHERE rel.identity_key IS NULL") ||
		!strings.Contains(statements[0].Cypher, "DELETE rel") ||
		!strings.Contains(statements[1].Cypher, "MERGE (i)-[rel:RUNS_ON {identity_key: 'canonical'}]->(p)") ||
		!strings.Contains(statements[1].Cypher, "rel.evidence_source = row.evidence_source") ||
		!strings.Contains(statements[1].Cypher, "rel.source_tool = row.source_tool") {
		return errors.New("unexpected cross-repo RUNS_ON group shape")
	}
	if e.groupAttempts == 1 {
		return e.conflict
	}
	rows, ok := statements[1].Parameters["rows"].([]map[string]any)
	if !ok || len(rows) != 1 {
		return errors.New("unexpected cross-repo RUNS_ON rows")
	}
	e.legacyPresent = false
	e.canonical = map[string]any{
		"identity_key":    "canonical",
		"confidence":      0.97,
		"evidence_source": rows[0]["evidence_source"],
		"source_tool":     rows[0]["source_tool"],
	}
	return nil
}

func TestCrossRepoRunsOnAtomicGroupReplaysCommitConflicts(t *testing.T) {
	const platformID = "platform:eks:aws:cluster-1:prod:us-east-1"
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
			inner := &runsOnEdgeConflictExecutor{conflict: test.err, legacyPresent: true}
			writer := NewEdgeWriter(&RetryingExecutor{
				Inner:      inner,
				MaxRetries: 1,
				BaseDelay:  time.Millisecond,
			}, 0)
			rows := []reducer.SharedProjectionIntentRow{{
				IntentID:     "cross-repo-runs-on-retry",
				RepositoryID: "service-repo",
				Payload: map[string]any{
					"repo_id":           "service-repo",
					"platform_id":       platformID,
					"relationship_type": "RUNS_ON",
					"source_tool":       "argocd",
				},
			}}
			_, err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "resolver/cross-repo")
			if err != nil {
				t.Fatalf("WriteEdges() error = %v, want replayed atomic group", err)
			}
			if inner.groupAttempts != 2 {
				t.Fatalf("group attempts = %d, want one failed commit and one replay", inner.groupAttempts)
			}
			if inner.legacyPresent || inner.canonical == nil {
				t.Fatalf("final RUNS_ON state = legacy %t, canonical %v", inner.legacyPresent, inner.canonical)
			}
			if inner.canonical["identity_key"] != "canonical" ||
				inner.canonical["confidence"] != 0.97 ||
				inner.canonical["evidence_source"] != "resolver/cross-repo" ||
				inner.canonical["source_tool"] != "argocd" {
				t.Fatalf("final canonical RUNS_ON tuple = %v", inner.canonical)
			}
		})
	}
}
