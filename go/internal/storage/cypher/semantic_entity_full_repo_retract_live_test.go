// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/semanticentity"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestSemanticEntityWriterLiveNornicDBFullRepoRetractRecreatesSameUID measures
// the retract path #6176's other live proof does not reach.
//
// The committed delta proof retracts by n.path and upserts rows in a DIFFERENT
// file, so the retracted set and the upserted set are disjoint and no uid is
// ever deleted and recreated inside one transaction. The full-repo retract
// (semanticRetractStatements -> WHERE n.repo_id IN $repo_ids ... DETACH DELETE)
// always overlaps: every row the generation keeps is deleted by the retract and
// re-MERGEd by the upsert in the SAME grouped transaction. That is a distinct
// read-your-writes question for a backend, and #4367 is a standing reminder
// that a grouped DETACH DELETE can under-apply here without reporting anything.
//
// The assertions are chosen so the three ways this can go wrong are
// distinguishable rather than collapsing into one count:
//
//   - the retract under-applied: the dropped uid survives;
//   - the re-MERGE did not see the delete: the kept uid is duplicated, or its
//     File containment edge is;
//   - the re-MERGE was swallowed by the delete: the kept uid is missing, or
//     still carries its previous-generation value.
//
// Opt-in: requires a configured live Bolt backend
// (ESHU_SEMANTIC_ENTITY_NORNICDB_LIVE=1, ESHU_GRAPH_BACKEND=nornicdb, and the
// Bolt env vars).
func TestSemanticEntityWriterLiveNornicDBFullRepoRetractRecreatesSameUID(t *testing.T) {
	if !semanticEntityNornicDBLiveEnabled() {
		t.Skipf("set %s=1 (and Bolt env) to run the live full-repo semantic retract proof", semanticEntityNornicDBLiveEnv)
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

	// Guard the premise. Without a group-capable executor WriteSemanticEntities
	// silently takes its per-statement fallback and a green run would say
	// nothing about the grouped retract this test exists to measure.
	if _, ok := any(exec).(cypher.GroupExecutor); !ok {
		t.Fatalf("live executor does not implement cypher.GroupExecutor; the grouped retract would not be exercised")
	}

	runID := secretsIAMLiveTestRunID(t)
	repoID := fmt.Sprintf("repo:test:6176-full-retract:%s", runID)
	keptPath := fmt.Sprintf("/tmp/eshu-6176-full-retract/%s/lib/kept.ex", runID)
	droppedPath := fmt.Sprintf("/tmp/eshu-6176-full-retract/%s/lib/dropped.ex", runID)
	keptUID := fmt.Sprintf("variable:test:%s:kept", runID)
	droppedUID := fmt.Sprintf("variable:test:%s:dropped", runID)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_ = exec.Execute(cleanupCtx, cypher.Statement{
			Cypher:     `MATCH (n) WHERE n.repo_id = $repo_id DETACH DELETE n`,
			Parameters: map[string]any{"repo_id": repoID},
		})
	})

	for _, path := range []string{keptPath, droppedPath} {
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher: `MERGE (f:File {path: $path})
SET f.repo_id = $repo_id`,
			Parameters: map[string]any{"path": path, "repo_id": repoID},
		}); err != nil {
			t.Fatalf("seed file %s: %v", path, err)
		}
	}

	retrying := &cypher.RetryingExecutor{Inner: exec, MaxRetries: 3, BaseDelay: 5 * time.Millisecond}
	writer := cypher.NewSemanticEntityWriterWithCanonicalNodeRows(retrying, 100).WithLabelScopedRetract()

	variableRow := func(uid, path, relative, name, value string) semanticentity.SemanticEntityRow {
		return semanticentity.SemanticEntityRow{
			RepoID:       repoID,
			EntityID:     uid,
			EntityType:   "Variable",
			EntityName:   name,
			FilePath:     path,
			RelativePath: relative,
			Language:     "elixir",
			StartLine:    2,
			EndLine:      2,
			Metadata: map[string]any{
				"attribute_kind": "module_attribute",
				"value":          value,
			},
		}
	}

	// gen1: both Variable nodes for the repo.
	if _, err := writer.WriteSemanticEntities(ctx, semanticentity.SemanticEntityWrite{
		RepoIDs: []string{repoID},
		Rows: []semanticentity.SemanticEntityRow{
			variableRow(keptUID, keptPath, "lib/kept.ex", "@timeout", "gen1"),
			variableRow(droppedUID, droppedPath, "lib/dropped.ex", "@retries", "gen1"),
		},
	}); err != nil {
		t.Fatalf("gen1 write: %v", err)
	}

	gen1Count, err := exec.count(ctx, `MATCH (n:Variable)
WHERE n.repo_id = $repo_id
RETURN count(n)`, map[string]any{"repo_id": repoID})
	if err != nil {
		t.Fatalf("count Variable nodes after gen1: %v", err)
	}
	if gen1Count != 2 {
		t.Fatalf("Variable count after gen1 = %d, want 2", gen1Count)
	}

	// gen2: the full-repo retract path. No DeltaProjection, so the group is
	// the repo-scoped DETACH DELETE followed by the MERGE that recreates the
	// kept uid the retract just removed, in one transaction.
	if _, err := writer.WriteSemanticEntities(ctx, semanticentity.SemanticEntityWrite{
		RepoIDs: []string{repoID},
		Rows: []semanticentity.SemanticEntityRow{
			variableRow(keptUID, keptPath, "lib/kept.ex", "@timeout", "gen2"),
		},
	}); err != nil {
		t.Fatalf("gen2 full-repo retract write: %v", err)
	}

	droppedCount, err := exec.count(ctx, `MATCH (n:Variable {uid: $uid})
RETURN count(n)`, map[string]any{"uid": droppedUID})
	if err != nil {
		t.Fatalf("count dropped Variable after gen2: %v", err)
	}
	if droppedCount != 0 {
		t.Fatalf("dropped Variable count after gen2 = %d, want 0 (the grouped repo-scoped DETACH DELETE under-applied)", droppedCount)
	}

	keptCount, err := exec.count(ctx, `MATCH (n:Variable {uid: $uid})
RETURN count(n)`, map[string]any{"uid": keptUID})
	if err != nil {
		t.Fatalf("count kept Variable after gen2: %v", err)
	}
	if keptCount != 1 {
		t.Fatalf("kept Variable count after gen2 = %d, want 1 (deleted and re-merged in one transaction)", keptCount)
	}

	keptValues, err := exec.values(ctx, `MATCH (n:Variable {uid: $uid})
RETURN n.value`, map[string]any{"uid": keptUID})
	if err != nil {
		t.Fatalf("read kept Variable value after gen2: %v", err)
	}
	if len(keptValues) != 1 || keptValues[0] != "gen2" {
		t.Fatalf("kept Variable value after gen2 = %v, want [gen2] (the re-MERGE did not land)", keptValues)
	}

	containment, err := exec.count(ctx, `MATCH (:File {path: $path})-[r:CONTAINS]->(:Variable {uid: $uid})
RETURN count(r)`, map[string]any{"path": keptPath, "uid": keptUID})
	if err != nil {
		t.Fatalf("count kept Variable containment after gen2: %v", err)
	}
	if containment != 1 {
		t.Fatalf("kept Variable containment after gen2 = %d, want 1", containment)
	}

	files, err := exec.count(ctx, `MATCH (f:File)
WHERE f.repo_id = $repo_id
RETURN count(f)`, map[string]any{"repo_id": repoID})
	if err != nil {
		t.Fatalf("count File nodes after gen2: %v", err)
	}
	if files != 2 {
		t.Fatalf("File count after gen2 = %d, want 2 (the retract must not reach File nodes)", files)
	}
}
