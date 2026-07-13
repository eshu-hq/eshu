// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/projector"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	storagenornicdb "github.com/eshu-hq/eshu/go/internal/storage/nornicdb"
)

func TestLoadProjectorNornicDBConfigUsesProductionWriterDefaults(t *testing.T) {
	t.Parallel()

	config, err := loadProjectorNornicDBConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadProjectorNornicDBConfig() error = %v, want nil", err)
	}
	if got, want := config.FileBatchSize, storagenornicdb.DefaultFileBatchSize; got != want {
		t.Fatalf("file batch size = %d, want %d", got, want)
	}
	if got, want := config.EntityBatchSize, storagenornicdb.DefaultEntityBatchSize; got != want {
		t.Fatalf("entity batch size = %d, want %d", got, want)
	}
	if got, want := config.EntityLabelBatchSizes["Function"], storagenornicdb.DefaultFunctionEntityBatchSize; got != want {
		t.Fatalf("Function batch size = %d, want %d", got, want)
	}
	if got, want := config.BatchedEntityContainment, storagenornicdb.DefaultBatchedEntityContainment; got != want {
		t.Fatalf("batched entity containment = %v, want %v", got, want)
	}
}

func TestLoadProjectorNornicDBConfigRejectsInvalidEnv(t *testing.T) {
	t.Parallel()

	_, err := loadProjectorNornicDBConfig(func(name string) string {
		if name == projectorNornicDBFileBatchSizeEnv {
			return "invalid"
		}
		return ""
	})
	if err == nil {
		t.Fatal("loadProjectorNornicDBConfig() error = nil, want invalid env error")
	}
	if !strings.Contains(err.Error(), projectorNornicDBFileBatchSizeEnv) {
		t.Fatalf("error = %q, want env name", err)
	}
}

func TestLoadProjectorNornicDBConfigRejectsRetractBatchAboveMaximum(t *testing.T) {
	t.Parallel()

	_, err := loadProjectorNornicDBConfig(func(name string) string {
		if name == projectorNornicDBCanonicalRetractBatchEnv {
			return "10001"
		}
		return ""
	})
	if err == nil {
		t.Fatal("loadProjectorNornicDBConfig() error = nil, want out-of-range error")
	}
	if !strings.Contains(err.Error(), projectorNornicDBCanonicalRetractBatchEnv) ||
		!strings.Contains(err.Error(), "1..10000") {
		t.Fatalf("error = %q, want env name and valid range", err)
	}
}

func TestLoadProjectorNornicDBConfigAcceptsRetractBatchBoundaries(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"1", "10000"} {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			config, err := loadProjectorNornicDBConfig(func(name string) string {
				if name == projectorNornicDBCanonicalRetractBatchEnv {
					return value
				}
				return ""
			})
			if err != nil {
				t.Fatalf("loadProjectorNornicDBConfig() error = %v, want nil", err)
			}
			if got := config.CanonicalRetractBatchSize; strconv.Itoa(got) != value {
				t.Fatalf("retract batch size = %d, want %s", got, value)
			}
		})
	}
}

func TestConfigureProjectorCanonicalWriterUsesNornicDBBatchedContainment(t *testing.T) {
	t.Parallel()

	executor := &recordingProjectorPhaseExecutor{}
	config, err := loadProjectorNornicDBConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadProjectorNornicDBConfig() error = %v", err)
	}
	writer := sourcecypher.NewCanonicalNodeWriter(executor, 500, nil)
	writer = configureProjectorCanonicalWriter(writer, runtimecfg.GraphBackendNornicDB, config)

	if err := writer.Write(context.Background(), projectorContainmentMaterialization()); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	batchedEntities := 0
	for _, statement := range executor.statements {
		phase, _ := statement.Parameters[sourcecypher.StatementMetadataPhaseKey].(string)
		if phase == sourcecypher.CanonicalPhaseEntityContainment {
			t.Fatalf("separate entity_containment statement emitted: %s", statement.Cypher)
		}
		if phase == sourcecypher.CanonicalPhaseEntities &&
			strings.Contains(statement.Cypher, "MATCH (f:File {path: row.file_path})") &&
			strings.Contains(statement.Cypher, "MERGE (f)-[rel:CONTAINS]->(n)") {
			batchedEntities++
		}
	}
	if got, want := batchedEntities, 1; got != want {
		t.Fatalf("batched entity statements = %d, want %d", got, want)
	}
}

func TestProjectorCanonicalTransactionTimeoutOnlyAppliesToNornicDB(t *testing.T) {
	t.Parallel()

	getenv := func(name string) string {
		if name == canonicalWriteTimeoutEnv {
			return "4s"
		}
		return ""
	}
	if got := projectorCanonicalTransactionTimeout(runtimecfg.GraphBackendNeo4j, getenv); got != 0 {
		t.Fatalf("Neo4j transaction timeout = %s, want 0", got)
	}
	if got, want := projectorCanonicalTransactionTimeout(runtimecfg.GraphBackendNornicDB, getenv), 4*time.Second; got != want {
		t.Fatalf("NornicDB transaction timeout = %s, want %s", got, want)
	}
}

func TestProjectorNeo4jExecutorTransactionConfigurersSetTimeout(t *testing.T) {
	t.Parallel()

	executor := projectorNeo4jExecutor{TxTimeout: 4 * time.Second}
	configurers := executor.transactionConfigurers()
	if got, want := len(configurers), 1; got != want {
		t.Fatalf("transaction configurer count = %d, want %d", got, want)
	}
	var config neo4jdriver.TransactionConfig
	configurers[0](&config)
	if got, want := config.Timeout, 4*time.Second; got != want {
		t.Fatalf("transaction timeout = %s, want %s", got, want)
	}
}

func TestProjectorNornicDBDrainUsesPerIterationClientTimeout(t *testing.T) {
	t.Parallel()

	getenv := func(name string) string {
		if name == canonicalWriteTimeoutEnv {
			return "10ms"
		}
		return ""
	}
	raw := blockingProjectorDrainExecutor{}
	executor := projectorCanonicalExecutorForGraphBackend(
		raw,
		runtimecfg.GraphBackendNornicDB,
		projectorNornicDBConfigForTest(t, getenv),
		getenv,
		nil,
		nil,
	)
	phase, ok := executor.(storagenornicdb.PhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want nornicdb.PhaseGroupExecutor", executor)
	}
	if phase.DrainReader == nil {
		t.Fatal("NornicDB phase executor has no drain reader")
	}
	outerCtx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := phase.DrainReader.RunWrite(outerCtx, "RETURN 0 AS __drained", nil)
	if err == nil || !strings.Contains(err.Error(), "drain timed out after 10ms") {
		t.Fatalf("RunWrite() error = %v, want per-iteration client-timeout error", err)
	}
	var timeoutErr sourcecypher.GraphWriteTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("RunWrite() error = %T, want GraphWriteTimeoutError", err)
	}
	if !projector.IsRetryable(err) {
		t.Fatalf("projector.IsRetryable(%v) = false, want true", err)
	}
	if elapsed := time.Since(started); elapsed >= 80*time.Millisecond {
		t.Fatalf("RunWrite() elapsed = %s, want client timeout before outer deadline", elapsed)
	}
}

func TestProjectorCanonicalWriterDrainTimeoutRemainsQueueRetryable(t *testing.T) {
	t.Parallel()

	getenv := func(name string) string {
		if name == canonicalWriteTimeoutEnv {
			return "10ms"
		}
		return ""
	}
	executor := projectorCanonicalExecutorForGraphBackend(
		blockingProjectorDrainExecutor{},
		runtimecfg.GraphBackendNornicDB,
		projectorNornicDBConfigForTest(t, getenv),
		getenv,
		nil,
		nil,
	)
	writer := sourcecypher.NewCanonicalNodeWriter(executor, sourcecypher.DefaultBatchSize, nil)
	err := writer.Write(context.Background(), projector.CanonicalMaterialization{
		ScopeID:      "scope-drain-timeout",
		GenerationID: "generation-2",
		RepoID:       "repo-drain-timeout",
		Repository: &projector.RepositoryRow{
			RepoID: "repo-drain-timeout",
			Name:   "drain-timeout",
			Path:   "/repos/drain-timeout",
		},
	})
	if err == nil {
		t.Fatal("Write() error = nil, want drain timeout")
	}
	var timeoutErr sourcecypher.GraphWriteTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("Write() error = %T, want GraphWriteTimeoutError", err)
	}
	if !projector.IsRetryable(err) {
		t.Fatalf("projector.IsRetryable(%v) = false, want queue retry", err)
	}
}

type recordingProjectorPhaseExecutor struct {
	statements []sourcecypher.Statement
}

type blockingProjectorDrainExecutor struct{}

func (blockingProjectorDrainExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (blockingProjectorDrainExecutor) ExecuteGroup(context.Context, []sourcecypher.Statement) error {
	return nil
}

func (blockingProjectorDrainExecutor) RunWrite(
	ctx context.Context,
	_ string,
	_ map[string]any,
) (storagenornicdb.DrainWriteResult, error) {
	<-ctx.Done()
	return storagenornicdb.DrainWriteResult{}, ctx.Err()
}

func (e *recordingProjectorPhaseExecutor) Execute(_ context.Context, statement sourcecypher.Statement) error {
	e.statements = append(e.statements, statement)
	return nil
}

func (e *recordingProjectorPhaseExecutor) ExecutePhaseGroup(
	_ context.Context,
	statements []sourcecypher.Statement,
) error {
	e.statements = append(e.statements, statements...)
	return nil
}

func projectorContainmentMaterialization() projector.CanonicalMaterialization {
	return projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		RepoID:       "repository-1",
		RepoPath:     "/repos/example",
		Repository: &projector.RepositoryRow{
			RepoID: "repository-1",
			Name:   "example",
			Path:   "/repos/example",
		},
		Directories: []projector.DirectoryRow{{
			Path:       "/repos/example/src",
			Name:       "src",
			ParentPath: "/repos/example",
			RepoID:     "repository-1",
			Depth:      0,
		}},
		Files: []projector.FileRow{{
			Path:         "/repos/example/src/main.go",
			RelativePath: "src/main.go",
			Name:         "main.go",
			Language:     "go",
			RepoID:       "repository-1",
			DirPath:      "/repos/example/src",
		}},
		Entities: []projector.EntityRow{{
			EntityID:     "function-1",
			Label:        "Function",
			EntityName:   "main",
			FilePath:     "/repos/example/src/main.go",
			RelativePath: "src/main.go",
			StartLine:    1,
			EndLine:      5,
			Language:     "go",
			RepoID:       "repository-1",
		}},
	}
}
