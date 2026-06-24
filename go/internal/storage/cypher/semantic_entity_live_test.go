// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const semanticEntityNornicDBLiveEnv = "ESHU_SEMANTIC_ENTITY_NORNICDB_LIVE"

func semanticEntityNornicDBLiveEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(semanticEntityNornicDBLiveEnv))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// TestSemanticEntityWriterLiveNornicDBConcurrentDistinctRepos proves that the
// semantic writer converges under cross-scope parallelism without a global
// queue cap. It is opt-in because it requires a configured live Bolt backend.
func TestSemanticEntityWriterLiveNornicDBConcurrentDistinctRepos(t *testing.T) {
	if !semanticEntityNornicDBLiveEnabled() {
		t.Skipf("set %s=1 (and Bolt env) to run live semantic entity concurrency proof", semanticEntityNornicDBLiveEnv)
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
	writes := semanticEntityLiveWrites(runID, 8)
	repoIDs := make([]string, 0, len(writes))
	for _, write := range writes {
		repoIDs = append(repoIDs, write.repoID)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_ = exec.Execute(cleanupCtx, cypher.Statement{
			Cypher:     `MATCH (n) WHERE n.repo_id IN $repo_ids DETACH DELETE n`,
			Parameters: map[string]any{"repo_ids": repoIDs},
		})
	})

	for _, write := range writes {
		seedSemanticEntityLiveRepo(t, ctx, exec, write)
	}

	retrying := &cypher.RetryingExecutor{
		Inner:      exec,
		MaxRetries: 3,
		BaseDelay:  5 * time.Millisecond,
	}
	writer := cypher.NewSemanticEntityWriterWithCanonicalNodeRows(
		cypher.ExecuteOnlyExecutor{Inner: retrying},
		10,
	).WithLabelScopedRetract()

	var wg sync.WaitGroup
	errs := make(chan error, len(writes))
	for _, write := range writes {
		write := write
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := writer.WriteSemanticEntities(ctx, reducer.SemanticEntityWrite{
				RepoIDs: []string{write.repoID},
				Rows: []reducer.SemanticEntityRow{
					write.functionRow(),
					write.moduleRow(),
				},
			}); err != nil {
				errs <- fmt.Errorf("write repo %s: %w", write.repoID, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if t.Failed() {
		return
	}

	for _, write := range writes {
		assertSemanticEntityLiveWrite(t, ctx, exec, write)
	}
}

type semanticEntityLiveWrite struct {
	repoID      string
	filePath    string
	functionID  string
	moduleID    string
	docstring   string
	moduleName  string
	moduleKind  string
	functionEnd int
}

func semanticEntityLiveWrites(runID string, count int) []semanticEntityLiveWrite {
	writes := make([]semanticEntityLiveWrite, 0, count)
	for i := 0; i < count; i++ {
		repoID := fmt.Sprintf("repo:test:semantic-live:%s:%02d", runID, i)
		writes = append(writes, semanticEntityLiveWrite{
			repoID:      repoID,
			filePath:    fmt.Sprintf("/tmp/eshu-semantic-live/%s/%02d/main.go", runID, i),
			functionID:  fmt.Sprintf("function:test:semantic-live:%s:%02d", runID, i),
			moduleID:    fmt.Sprintf("module:test:semantic-live:%s:%02d", runID, i),
			docstring:   fmt.Sprintf("semantic live function %02d", i),
			moduleName:  fmt.Sprintf("semantic_live_%02d", i),
			moduleKind:  "package",
			functionEnd: i + 2,
		})
	}
	return writes
}

func (w semanticEntityLiveWrite) functionRow() reducer.SemanticEntityRow {
	return reducer.SemanticEntityRow{
		RepoID:       w.repoID,
		EntityID:     w.functionID,
		EntityType:   "Function",
		EntityName:   "handleLiveSemantic",
		FilePath:     w.filePath,
		RelativePath: "main.go",
		Language:     "go",
		StartLine:    1,
		EndLine:      w.functionEnd,
		Metadata:     map[string]any{"docstring": w.docstring},
	}
}

func (w semanticEntityLiveWrite) moduleRow() reducer.SemanticEntityRow {
	return reducer.SemanticEntityRow{
		RepoID:       w.repoID,
		EntityID:     w.moduleID,
		EntityType:   "Module",
		EntityName:   w.moduleName,
		FilePath:     w.filePath,
		RelativePath: "main.go",
		Language:     "go",
		StartLine:    1,
		EndLine:      1,
		Metadata:     map[string]any{"module_kind": w.moduleKind},
	}
}

func seedSemanticEntityLiveRepo(
	t *testing.T,
	ctx context.Context,
	exec liveSecretsIAMExecutor,
	write semanticEntityLiveWrite,
) {
	t.Helper()

	if err := exec.Execute(ctx, cypher.Statement{
		Cypher: `MERGE (f:File {path: $path})
SET f.repo_id = $repo_id,
    f.relative_path = "main.go"`,
		Parameters: map[string]any{"path": write.filePath, "repo_id": write.repoID},
	}); err != nil {
		t.Fatalf("seed file %s: %v", write.filePath, err)
	}
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher: `MERGE (fn:Function {uid: $uid})
SET fn.repo_id = $repo_id,
    fn.path = $path,
    fn.evidence_source = "source-local"`,
		Parameters: map[string]any{
			"uid":     write.functionID,
			"repo_id": write.repoID,
			"path":    write.filePath,
		},
	}); err != nil {
		t.Fatalf("seed function %s: %v", write.functionID, err)
	}
}

func assertSemanticEntityLiveWrite(
	t *testing.T,
	ctx context.Context,
	exec liveSecretsIAMExecutor,
	write semanticEntityLiveWrite,
) {
	t.Helper()

	functionCount, err := exec.count(ctx, `MATCH (fn:Function {uid: $uid})
WHERE fn.repo_id = $repo_id
  AND fn.docstring = $docstring
RETURN count(fn)`, map[string]any{
		"uid":       write.functionID,
		"repo_id":   write.repoID,
		"docstring": write.docstring,
	})
	if err != nil {
		t.Fatalf("count function %s: %v", write.functionID, err)
	}
	if functionCount != 1 {
		t.Fatalf("function %s count = %d, want 1", write.functionID, functionCount)
	}

	moduleCount, err := exec.count(ctx, `MATCH (m:Module {uid: $uid})
WHERE m.repo_id = $repo_id
  AND m.module_kind = $module_kind
RETURN count(m)`, map[string]any{
		"uid":         write.moduleID,
		"repo_id":     write.repoID,
		"module_kind": write.moduleKind,
	})
	if err != nil {
		t.Fatalf("count module %s: %v", write.moduleID, err)
	}
	if moduleCount != 1 {
		t.Fatalf("module %s count = %d, want 1", write.moduleID, moduleCount)
	}

	containsCount, err := exec.count(ctx, `MATCH (:File {path: $path})-[r:CONTAINS]->(:Module {uid: $uid})
RETURN count(r)`, map[string]any{"path": write.filePath, "uid": write.moduleID})
	if err != nil {
		t.Fatalf("count module containment %s: %v", write.moduleID, err)
	}
	if containsCount != 1 {
		t.Fatalf("module containment %s count = %d, want 1", write.moduleID, containsCount)
	}
}
