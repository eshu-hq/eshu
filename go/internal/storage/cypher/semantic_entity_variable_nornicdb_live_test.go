// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestSemanticEntityWriterLiveNornicDBMaterializesVariableNodes is the live
// reproduction for issue #5156: on NornicDB, Variable nodes (Elixir module
// attributes, TSX component-type assertions) were never materialized because
// the writer treated Variable as canonical-node-owned (MATCH-only) while
// nothing else ever created the base node. It constructs the writer exactly
// as go/cmd/reducer/neo4j_wiring.go wires it for NornicDB
// (NewSemanticEntityWriterWithCanonicalNodeRows(...).WithLabelScopedRetract()),
// writes one Elixir module-attribute row and one TSX component-type-assertion
// row, and proves both Variable nodes exist with evidence_source and File
// CONTAINS edges. It then proves a full write -> retract cycle: repo-scoped
// retract removes the in-scope Variable nodes while an out-of-scope control
// repo's Variable node survives untouched, directly checking the NornicDB
// v1.1.11 grouped-DELETE-under-application class of bug for this new retract
// path (#5152/#5305 precedent).
//
// Opt-in: requires a configured live Bolt backend
// (ESHU_SEMANTIC_ENTITY_NORNICDB_LIVE=1, ESHU_GRAPH_BACKEND=nornicdb, and the
// Bolt env vars).
func TestSemanticEntityWriterLiveNornicDBMaterializesVariableNodes(t *testing.T) {
	if !semanticEntityNornicDBLiveEnabled() {
		t.Skipf("set %s=1 (and Bolt env) to run live semantic entity Variable materialization proof", semanticEntityNornicDBLiveEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	backend, err := runtimecfg.LoadGraphBackend(os.Getenv)
	if err != nil {
		t.Fatalf("load graph backend: %v", err)
	}
	if backend != runtimecfg.GraphBackendNornicDB {
		t.Fatalf("%s requires ESHU_GRAPH_BACKEND=%s, got %q", semanticEntityNornicDBLiveEnv, runtimecfg.GraphBackendNornicDB, backend)
	}

	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, os.Getenv)
	if err != nil {
		t.Fatalf("open Bolt driver: %v", err)
	}
	registerSecretsIAMLiveDriverClose(t, driver.Close)

	exec := liveSecretsIAMExecutor{driver: driver, database: cfg.DatabaseName}
	runID := secretsIAMLiveTestRunID(t)

	inScopeRepo := fmt.Sprintf("repo:test:variable-live:%s:in-scope", runID)
	controlRepo := fmt.Sprintf("repo:test:variable-live:%s:control", runID)
	elixirPath := fmt.Sprintf("/tmp/eshu-variable-live/%s/lib/worker.ex", runID)
	tsxPath := fmt.Sprintf("/tmp/eshu-variable-live/%s/src/Screen.tsx", runID)
	controlPath := fmt.Sprintf("/tmp/eshu-variable-live/%s/lib/control.ex", runID)

	elixirVariableID := fmt.Sprintf("variable:test:%s:elixir-attr", runID)
	tsxVariableID := fmt.Sprintf("variable:test:%s:tsx-assertion", runID)
	controlVariableID := fmt.Sprintf("variable:test:%s:control-attr", runID)

	allRepoIDs := []string{inScopeRepo, controlRepo}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_ = exec.Execute(cleanupCtx, cypher.Statement{
			Cypher:     `MATCH (n) WHERE n.repo_id IN $repo_ids DETACH DELETE n`,
			Parameters: map[string]any{"repo_ids": allRepoIDs},
		})
	})

	for _, seed := range []struct {
		path   string
		repoID string
	}{
		{path: elixirPath, repoID: inScopeRepo},
		{path: tsxPath, repoID: inScopeRepo},
		{path: controlPath, repoID: controlRepo},
	} {
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher: `MERGE (f:File {path: $path})
SET f.repo_id = $repo_id`,
			Parameters: map[string]any{"path": seed.path, "repo_id": seed.repoID},
		}); err != nil {
			t.Fatalf("seed file %s: %v", seed.path, err)
		}
	}

	retryingExec := &cypher.RetryingExecutor{
		Inner:      exec,
		MaxRetries: 3,
		BaseDelay:  5 * time.Millisecond,
	}
	writer := cypher.NewSemanticEntityWriterWithCanonicalNodeRows(retryingExec, 100).WithLabelScopedRetract()

	// 1. Write the control repo's Variable row first so a later in-scope
	// retract has an out-of-scope survivor to prove scoping against.
	if _, err := writer.WriteSemanticEntities(ctx, reducer.SemanticEntityWrite{
		RepoIDs: []string{controlRepo},
		Rows: []reducer.SemanticEntityRow{
			{
				RepoID:       controlRepo,
				EntityID:     controlVariableID,
				EntityType:   "Variable",
				EntityName:   "@control_timeout",
				FilePath:     controlPath,
				RelativePath: "lib/control.ex",
				Language:     "elixir",
				StartLine:    2,
				EndLine:      2,
				Metadata: map[string]any{
					"attribute_kind": "module_attribute",
					"value":          "1_000",
				},
			},
		},
	}); err != nil {
		t.Fatalf("write control repo variable: %v", err)
	}

	// 2. Write the in-scope repo's Elixir module-attribute Variable row and
	// TSX component-type-assertion Variable row in one call, matching the
	// NornicDB reducer wiring exactly.
	if _, err := writer.WriteSemanticEntities(ctx, reducer.SemanticEntityWrite{
		RepoIDs: []string{inScopeRepo},
		Rows: []reducer.SemanticEntityRow{
			{
				RepoID:       inScopeRepo,
				EntityID:     elixirVariableID,
				EntityType:   "Variable",
				EntityName:   "@timeout",
				FilePath:     elixirPath,
				RelativePath: "lib/worker.ex",
				Language:     "elixir",
				StartLine:    2,
				EndLine:      2,
				Metadata: map[string]any{
					"attribute_kind": "module_attribute",
					"value":          "5_000",
				},
			},
			{
				RepoID:       inScopeRepo,
				EntityID:     tsxVariableID,
				EntityType:   "Variable",
				EntityName:   "Screen",
				FilePath:     tsxPath,
				RelativePath: "src/Screen.tsx",
				Language:     "tsx",
				StartLine:    6,
				EndLine:      6,
				Metadata: map[string]any{
					"component_type_assertion": "ComponentType",
				},
			},
		},
	}); err != nil {
		t.Fatalf("write in-scope repo variables: %v", err)
	}

	inScopeCount, err := exec.count(ctx, `MATCH (n:Variable)
WHERE n.repo_id = $repo_id AND n.evidence_source = $evidence_source
RETURN count(n)`, map[string]any{
		"repo_id":         inScopeRepo,
		"evidence_source": "parser/semantic-entities",
	})
	if err != nil {
		t.Fatalf("count in-scope Variable nodes after write: %v", err)
	}
	if inScopeCount != 2 {
		t.Fatalf("in-scope Variable count after write = %d, want 2 (issue #5156 reproduces as 0)", inScopeCount)
	}

	elixirContains, err := exec.count(ctx, `MATCH (:File {path: $path})-[r:CONTAINS]->(:Variable {uid: $uid})
RETURN count(r)`, map[string]any{"path": elixirPath, "uid": elixirVariableID})
	if err != nil {
		t.Fatalf("count elixir variable containment: %v", err)
	}
	if elixirContains != 1 {
		t.Fatalf("elixir variable containment count = %d, want 1", elixirContains)
	}

	tsxContains, err := exec.count(ctx, `MATCH (:File {path: $path})-[r:CONTAINS]->(:Variable {uid: $uid})
RETURN count(r)`, map[string]any{"path": tsxPath, "uid": tsxVariableID})
	if err != nil {
		t.Fatalf("count tsx variable containment: %v", err)
	}
	if tsxContains != 1 {
		t.Fatalf("tsx variable containment count = %d, want 1", tsxContains)
	}

	controlCountBeforeRetract, err := exec.count(ctx, `MATCH (n:Variable {uid: $uid})
WHERE n.repo_id = $repo_id
RETURN count(n)`, map[string]any{"uid": controlVariableID, "repo_id": controlRepo})
	if err != nil {
		t.Fatalf("count control Variable node before retract: %v", err)
	}
	if controlCountBeforeRetract != 1 {
		t.Fatalf("control Variable count before retract = %d, want 1", controlCountBeforeRetract)
	}

	// 3. Retract-cycle proof: rewrite the in-scope repo with zero rows so the
	// writer's normal retract-then-upsert statement group removes every stale
	// Variable node for that repo. This is the exact NornicDB grouped-DELETE
	// path the #5152 fix targeted for other writers; assert it actually
	// removes the rows rather than silently under-applying.
	if _, err := writer.WriteSemanticEntities(ctx, reducer.SemanticEntityWrite{
		RepoIDs: []string{inScopeRepo},
		Rows:    nil,
	}); err != nil {
		t.Fatalf("retract in-scope repo variables: %v", err)
	}

	inScopeCountAfterRetract, err := exec.count(ctx, `MATCH (n:Variable)
WHERE n.repo_id = $repo_id
RETURN count(n)`, map[string]any{"repo_id": inScopeRepo})
	if err != nil {
		t.Fatalf("count in-scope Variable nodes after retract: %v", err)
	}
	if inScopeCountAfterRetract != 0 {
		t.Fatalf("in-scope Variable count after retract = %d, want 0 (grouped DETACH DELETE under-applied)", inScopeCountAfterRetract)
	}

	controlCountAfterRetract, err := exec.count(ctx, `MATCH (n:Variable {uid: $uid})
WHERE n.repo_id = $repo_id
RETURN count(n)`, map[string]any{"uid": controlVariableID, "repo_id": controlRepo})
	if err != nil {
		t.Fatalf("count control Variable node after retract: %v", err)
	}
	if controlCountAfterRetract != 1 {
		t.Fatalf("control Variable count after retract = %d, want 1 (out-of-scope control must survive)", controlCountAfterRetract)
	}
}
