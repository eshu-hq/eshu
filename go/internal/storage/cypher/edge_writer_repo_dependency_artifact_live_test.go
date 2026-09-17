// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// trueGroupLiveExecutor mirrors the production reducer executor shape for
// repo_dependency claims: ExecuteGroup runs every statement sequentially
// inside ONE managed-transaction ExecuteWrite (cmd/reducer
// neo4jSessionRunner.RunCypherGroup), while Execute is single-statement
// autocommit. The package's boltTestExecutor does NOT reproduce this: it
// runs one ExecuteWrite per statement, which hides in-transaction
// write-visibility defects on NornicDB.
type trueGroupLiveExecutor struct {
	runner *boltRetractTestRunner
}

func (e *trueGroupLiveExecutor) Execute(ctx context.Context, stmt Statement) error {
	return e.runner.runCypherSingle(ctx, stmt)
}

func (e *trueGroupLiveExecutor) ExecuteGroup(ctx context.Context, stmts []Statement) error {
	if e.runner.driver == nil {
		return fmt.Errorf("neo4j driver is required")
	}
	session := e.runner.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.runner.databaseName,
	})
	defer func() { _ = session.Close(ctx) }()
	_, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		for _, stmt := range stmts {
			result, runErr := tx.Run(ctx, stmt.Cypher, stmt.Parameters)
			if runErr != nil {
				return nil, runErr
			}
			if _, consumeErr := result.Consume(ctx); consumeErr != nil {
				return nil, consumeErr
			}
		}
		return nil, nil
	})
	return err
}

// keeperRepoDependencyRow builds a DEPLOYS_FROM intent row carrying one
// deployment evidence artifact, the production shape
// buildResolvedEdgeIntentRow emits for ARGOCD_APPLICATION_SOURCE evidence.
// IDs are unique per test run so the endpoints are always cold: the defect
// only bites when the artifact MATCH must see nodes nothing committed yet.
func keeperRepoDependencyRow(tag string, i int) reducer.SharedProjectionIntentRow {
	repo := fmt.Sprintf("keeper/repo-%s-%d", tag, i)
	target := fmt.Sprintf("keeper/target-%s-%d", tag, i)
	return reducer.SharedProjectionIntentRow{
		IntentID:     fmt.Sprintf("keeper-intent-%s-%d", tag, i),
		RepositoryID: repo,
		ScopeID:      "keeper-scope",
		GenerationID: "keeper-generation",
		Payload: map[string]any{
			"repo_id":                 repo,
			"target_repo_id":          target,
			"relationship_type":       "DEPLOYS_FROM",
			"resolved_id":             fmt.Sprintf("keeper-resolved-%s-%d", tag, i),
			"generation_id":           "keeper-generation",
			"confidence":              0.9,
			"evidence_count":          1,
			"evidence_kinds":          []any{"ARGOCD_APPLICATION_SOURCE"},
			"resolution_source":       "inferred",
			"rationale":               "keeper",
			"source_tool":             "keeper",
			"source_revision":         "",
			"first_party_ref_version": "",
			"evidence_artifacts": []any{
				map[string]any{
					"evidence_kind":   "ARGOCD_APPLICATION_SOURCE",
					"artifact_family": "deployment",
					"path":            "application.yaml",
					"extractor":       "keeper",
					"environment":     "",
					"matched_alias":   "keeper",
					"matched_value":   "keeper",
					"confidence":      0.9,
				},
			},
		},
	}
}

func cleanupKeeperFamily(t *testing.T, ctx context.Context, runner *boltRetractTestRunner, tag string) {
	t.Helper()
	// Artifact node ids are content-derived (no keeper prefix), but every
	// row the writer commits stamps generation_id, so sweep by that plus
	// the keeper repo-id prefixes. This MUST run through a write session:
	// runCypher is read-only and a DELETE through it fails (silently
	// polluting every backend the test touches — caught when keeper nodes
	// showed up in a live gate verdict). Fail loudly if anything remains.
	stmt := Statement{
		Cypher:     `MATCH (n) WHERE n.generation_id = 'keeper-generation' OR n.id STARTS WITH $prefix OR n.repo_id STARTS WITH $prefix DETACH DELETE n`,
		Parameters: map[string]any{"prefix": "keeper/repo-" + tag},
	}
	if err := runner.runCypherSingle(ctx, stmt); err != nil {
		t.Errorf("cleanup keeper nodes: %v", err)
		return
	}
	left, err := boltCount(ctx, runner,
		`MATCH (n) WHERE n.generation_id = 'keeper-generation' OR n.id STARTS WITH $prefix OR n.repo_id STARTS WITH $prefix RETURN count(n) AS count`,
		map[string]any{"prefix": "keeper/repo-" + tag},
	)
	if err != nil {
		t.Errorf("verify keeper cleanup: %v", err)
		return
	}
	if left != 0 {
		t.Errorf("keeper cleanup left %d nodes behind", left)
	}
}

// TestBoltWriteEdgesRepoDependencyArtifactColdEndpointsPersist is the #6184
// run15 regression: a repo_dependency claim whose Repository endpoints exist
// only as the main batch's in-transaction MERGEs must still persist its
// EvidenceArtifact family. On NornicDB v1.3.3 a MATCH in a later statement of
// the same managed transaction does not see those nodes, so the artifact
// batch silently writes nothing while the call succeeds and the intents
// complete (#5410/#4367 family). The writer must not co-locate the artifact
// statements with the endpoint-creating statements in one managed txn.
//
// Gate: ESHU_CYPHER_BOLT_DSN must point at a NornicDB backend. When unset
// the test skips.
func TestBoltWriteEdgesRepoDependencyArtifactColdEndpointsPersist(t *testing.T) {
	runner := openBoltTestRunner(t)
	ctx := context.Background()
	tag := fmt.Sprintf("%d", time.Now().UnixNano())
	// LIFO: the driver must outlive the node sweep, so register its close
	// first (a bare defer would run before these cleanups and leave the
	// sweep dialing a closed driver).
	t.Cleanup(func() { runner.close(context.Background()) })
	t.Cleanup(func() { cleanupKeeperFamily(t, context.Background(), runner, tag) })

	writer := NewEdgeWriter(&trueGroupLiveExecutor{runner: runner}, 0)

	const n = 12
	rows := make([]reducer.SharedProjectionIntentRow, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, keeperRepoDependencyRow(tag, i))
	}
	if _, err := writer.WriteEdges(ctx, reducer.DomainRepoDependency, rows, "resolver/cross-repo"); err != nil {
		t.Fatalf("WriteEdges errored (not the silent-loss defect): %v", err)
	}

	missing := 0
	for i := 0; i < n; i++ {
		repo := fmt.Sprintf("keeper/repo-%s-%d", tag, i)
		target := fmt.Sprintf("keeper/target-%s-%d", tag, i)
		main, err := boltCount(ctx, runner,
			`MATCH (:Repository {id: $s})-[r:DEPLOYS_FROM]->(:Repository {id: $t}) RETURN count(r) AS count`,
			map[string]any{"s": repo, "t": target},
		)
		if err != nil {
			t.Fatalf("count main edge %d: %v", i, err)
		}
		art, err := boltCount(ctx, runner,
			`MATCH (:Repository {id: $s})-[r:HAS_DEPLOYMENT_EVIDENCE]->(:EvidenceArtifact) RETURN count(r) AS count`,
			map[string]any{"s": repo},
		)
		if err != nil {
			t.Fatalf("count artifact edge %d: %v", i, err)
		}
		if main == 0 || art == 0 {
			missing++
			t.Logf("row %d: main=%d artifactEdges=%d (silent loss on commit-success)", i, main, art)
		}
	}
	if missing > 0 {
		t.Fatalf("writer lost %d/%d artifact families on commit-success", missing, n)
	}
}
