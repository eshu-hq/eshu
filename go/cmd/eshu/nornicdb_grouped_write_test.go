package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/projector"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestNornicDBGroupedWriteSafetyProbe(t *testing.T) {
	withNornicDBSyntaxDriver(t, func(ctx context.Context, driver neo4jdriver.DriverWithContext) {
		executor := nornicDBConformanceExecutor{
			driver:       driver,
			databaseName: localNornicDBDefaultDatabase,
			txTimeout:    15 * time.Second,
		}

		t.Run("canonical writer grouped transaction commits Eshu node shape", func(t *testing.T) {
			writer := sourcecypher.NewCanonicalNodeWriter(executor, 500, nil)
			if err := writer.Write(ctx, groupedWriteMaterialization("eshu-nornicdb-grouped-commit")); err != nil {
				t.Fatalf("Write() error = %v, want nil", err)
			}

			count, err := nornicDBReadCount(ctx, driver, `
MATCH (:Repository {id: $repo_id})-[:REPO_CONTAINS]->(:File {path: $file_path})-[:CONTAINS]->(:Function {uid: $entity_id})
RETURN count(*) AS count`, map[string]any{
				"repo_id":   "eshu-nornicdb-grouped-commit",
				"file_path": "/tmp/eshu-nornicdb-grouped-commit/src/main.go",
				"entity_id": "entity:eshu-nornicdb-grouped-commit:main",
			})
			if err != nil {
				t.Fatalf("count committed canonical shape error = %v, want nil", err)
			}
			if count != 1 {
				t.Fatalf("committed canonical shape count = %d, want 1", count)
			}
		})

		t.Run("canonical writer preserves distinct entity ids for multiple functions", func(t *testing.T) {
			writer := sourcecypher.NewCanonicalNodeWriter(executor, 500, nil)
			if err := writer.Write(ctx, groupedWriteMaterializationWithTwoFunctions("eshu-nornicdb-multi-entity")); err != nil {
				t.Fatalf("Write() error = %v, want nil", err)
			}

			count, err := nornicDBReadCount(ctx, driver, `
MATCH (f:Function)
WHERE f.repo_id = $repo_id
RETURN count(f) AS count`, map[string]any{
				"repo_id": "eshu-nornicdb-multi-entity",
			})
			if err != nil {
				t.Fatalf("count distinct canonical function nodes error = %v, want nil", err)
			}
			if count != 2 {
				t.Fatalf("distinct canonical function node count = %d, want 2", count)
			}

			count, err = nornicDBReadCount(ctx, driver, `
MATCH (:Repository {id: $repo_id})-[:REPO_CONTAINS]->(:File {path: $file_path})-[:CONTAINS]->(f:Function)
RETURN count(f) AS count`, map[string]any{
				"repo_id":   "eshu-nornicdb-multi-entity",
				"file_path": "/tmp/eshu-nornicdb-multi-entity/src/main.go",
			})
			if err != nil {
				t.Fatalf("count canonical containment edges error = %v, want nil", err)
			}
			if count != 2 {
				t.Fatalf("canonical containment count = %d, want 2", count)
			}
		})

		t.Run("failed grouped transaction rollback status is observable", func(t *testing.T) {
			// This guards Eshu's default of keeping NornicDB canonical writes
			// sequential until rollback semantics pass the promotion gate below.
			nodeID := "eshu-nornicdb-grouped-rollback-gap"
			err := executeRollbackProbe(ctx, executor, nodeID)
			if err == nil {
				t.Fatal("ExecuteGroup() error = nil, want syntax error")
			}

			count, err := countRollbackProbeNodes(ctx, driver, nodeID)
			if err != nil {
				t.Fatalf("count rollback marker error = %v, want nil", err)
			}
			if count != 0 {
				t.Fatalf("rollback marker count = %d, want 0", count)
			}
			t.Logf("failed grouped transaction rollback marker count = %d", count)
		})

		t.Run("clean explicit transaction rollback status is observable", func(t *testing.T) {
			nodeID := "eshu-nornicdb-clean-explicit-rollback"
			if err := executeCleanExplicitRollbackProbe(ctx, driver, nodeID); err != nil {
				t.Fatalf("executeCleanExplicitRollbackProbe() error = %v, want nil", err)
			}

			count, err := countRollbackProbeNodes(ctx, driver, nodeID)
			if err != nil {
				t.Fatalf("count clean rollback marker error = %v, want nil", err)
			}
			if count != 0 {
				t.Fatalf("clean rollback marker count = %d, want 0", count)
			}
			t.Logf("clean explicit rollback marker count = %d", count)
		})

		t.Run("explicit transaction after statement failure rollback status is observable", func(t *testing.T) {
			nodeID := "eshu-nornicdb-failed-explicit-rollback"
			err := executeExplicitRollbackProbe(ctx, driver, nodeID)
			if err == nil {
				t.Fatal("executeExplicitRollbackProbe() error = nil, want syntax error")
			}

			count, err := countRollbackProbeNodes(ctx, driver, nodeID)
			if err != nil {
				t.Fatalf("count explicit rollback marker error = %v, want nil", err)
			}
			if count != 0 {
				t.Fatalf("explicit rollback marker count = %d, want 0", count)
			}
			t.Logf("explicit rollback after failed statement marker count = %d", count)
		})

		t.Run("grouped transaction honors caller timeout without partial write", func(t *testing.T) {
			nodeID := "eshu-nornicdb-grouped-timeout"
			timeoutExecutor := sourcecypher.TimeoutExecutor{
				Inner:   executor,
				Timeout: time.Nanosecond,
			}
			err := timeoutExecutor.ExecuteGroup(ctx, []sourcecypher.Statement{{
				Operation: sourcecypher.OperationCanonicalUpsert,
				Cypher:    "MERGE (n:EshuConformanceTimeout {id: $id}) SET n.value = $value",
				Parameters: map[string]any{
					"id":    nodeID,
					"value": "must-not-partially-commit",
				},
			}})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("ExecuteGroup() error = %v, want context deadline exceeded", err)
			}

			count, err := nornicDBReadCount(ctx, driver, `
MATCH (n:EshuConformanceTimeout {id: $id})
RETURN count(n) AS count`, map[string]any{"id": nodeID})
			if err != nil {
				t.Fatalf("count timeout marker error = %v, want nil", err)
			}
			if count != 0 {
				t.Fatalf("timeout marker count = %d, want 0", count)
			}
		})
	})
}

func TestNornicDBGroupedWriteRollbackConformance(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_NORNICDB_REQUIRE_GROUPED_ROLLBACK")) != "true" {
		t.Skip("set ESHU_NORNICDB_REQUIRE_GROUPED_ROLLBACK=true to require NornicDB grouped-write rollback conformance")
	}

	withNornicDBSyntaxDriver(t, func(ctx context.Context, driver neo4jdriver.DriverWithContext) {
		executor := nornicDBConformanceExecutor{
			driver:       driver,
			databaseName: localNornicDBDefaultDatabase,
			txTimeout:    15 * time.Second,
		}
		nodeID := "eshu-nornicdb-grouped-rollback-conformance"
		err := executeRollbackProbe(ctx, executor, nodeID)
		if err == nil {
			t.Fatal("ExecuteGroup() error = nil, want syntax error")
		}

		count, err := countRollbackProbeNodes(ctx, driver, nodeID)
		if err != nil {
			t.Fatalf("count rollback marker error = %v, want nil", err)
		}
		if count != 0 {
			t.Fatalf("rollback marker count = %d, want 0", count)
		}

		cleanNodeID := "eshu-nornicdb-clean-explicit-rollback-conformance"
		if err := executeCleanExplicitRollbackProbe(ctx, driver, cleanNodeID); err != nil {
			t.Fatalf("executeCleanExplicitRollbackProbe() error = %v, want nil", err)
		}
		count, err = countRollbackProbeNodes(ctx, driver, cleanNodeID)
		if err != nil {
			t.Fatalf("count clean rollback marker error = %v, want nil", err)
		}
		if count != 0 {
			t.Fatalf("clean rollback marker count = %d, want 0", count)
		}

		failedNodeID := "eshu-nornicdb-failed-explicit-rollback-conformance"
		err = executeExplicitRollbackProbe(ctx, driver, failedNodeID)
		if err == nil {
			t.Fatal("executeExplicitRollbackProbe() error = nil, want syntax error")
		}
		count, err = countRollbackProbeNodes(ctx, driver, failedNodeID)
		if err != nil {
			t.Fatalf("count explicit rollback marker error = %v, want nil", err)
		}
		if count != 0 {
			t.Fatalf("explicit rollback marker count = %d, want 0", count)
		}
	})
}

type nornicDBConformanceExecutor struct {
	driver       neo4jdriver.DriverWithContext
	databaseName string
	txTimeout    time.Duration
}

func (e nornicDBConformanceExecutor) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	if e.driver == nil {
		return fmt.Errorf("neo4j driver is required")
	}
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.databaseName,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters, e.transactionConfigurers()...)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

func (e nornicDBConformanceExecutor) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	if e.driver == nil {
		return fmt.Errorf("neo4j driver is required")
	}
	if len(stmts) == 0 {
		return nil
	}
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.databaseName,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

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
	}, e.transactionConfigurers()...)
	return err
}

func (e nornicDBConformanceExecutor) transactionConfigurers() []func(*neo4jdriver.TransactionConfig) {
	if e.txTimeout <= 0 {
		return nil
	}
	return []func(*neo4jdriver.TransactionConfig){neo4jdriver.WithTxTimeout(e.txTimeout)}
}

func groupedWriteMaterialization(repoID string) projector.CanonicalMaterialization {
	repoPath := "/tmp/" + repoID
	srcPath := repoPath + "/src"
	filePath := srcPath + "/main.go"
	return projector.CanonicalMaterialization{
		ScopeID:      "scope:" + repoID,
		GenerationID: "generation:" + repoID,
		RepoID:       repoID,
		RepoPath:     repoPath,
		Repository: &projector.RepositoryRow{
			RepoID:    repoID,
			Name:      repoID,
			Path:      repoPath,
			LocalPath: repoPath,
			RemoteURL: "",
			RepoSlug:  "",
			HasRemote: false,
		},
		Directories: []projector.DirectoryRow{
			{
				Path:       srcPath,
				Name:       "src",
				ParentPath: repoPath,
				RepoID:     repoID,
				Depth:      0,
			},
		},
		Files: []projector.FileRow{
			{
				Path:         filePath,
				RelativePath: "src/main.go",
				Name:         "main.go",
				Language:     "go",
				RepoID:       repoID,
				DirPath:      srcPath,
			},
		},
		Entities: []projector.EntityRow{
			{
				EntityID:     "entity:" + repoID + ":main",
				Label:        "Function",
				EntityName:   "main",
				FilePath:     filePath,
				RelativePath: "src/main.go",
				StartLine:    1,
				EndLine:      5,
				Language:     "go",
				RepoID:       repoID,
			},
		},
	}
}

func groupedWriteMaterializationWithTwoFunctions(repoID string) projector.CanonicalMaterialization {
	mat := groupedWriteMaterialization(repoID)
	filePath := "/tmp/" + repoID + "/src/main.go"
	mat.Entities = []projector.EntityRow{
		{
			EntityID:     "entity:" + repoID + ":main",
			Label:        "Function",
			EntityName:   "main",
			FilePath:     filePath,
			RelativePath: "src/main.go",
			StartLine:    1,
			EndLine:      5,
			Language:     "go",
			RepoID:       repoID,
		},
		{
			EntityID:     "entity:" + repoID + ":helper",
			Label:        "Function",
			EntityName:   "helper",
			FilePath:     filePath,
			RelativePath: "src/main.go",
			StartLine:    7,
			EndLine:      11,
			Language:     "go",
			RepoID:       repoID,
		},
	}
	return mat
}

func executeRollbackProbe(ctx context.Context, executor nornicDBConformanceExecutor, nodeID string) error {
	return executor.ExecuteGroup(ctx, []sourcecypher.Statement{
		{
			Operation: sourcecypher.OperationCanonicalUpsert,
			Cypher:    "MERGE (n:EshuConformanceRollback {id: $id}) SET n.value = $value",
			Parameters: map[string]any{
				"id":    nodeID,
				"value": "must-rollback",
			},
		},
		{
			Operation:  sourcecypher.OperationCanonicalUpsert,
			Cypher:     "THIS IS NOT CYPHER",
			Parameters: map[string]any{},
		},
	})
}

func executeExplicitRollbackProbe(
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	nodeID string,
) error {
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: localNornicDBDefaultDatabase,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	tx, err := session.BeginTransaction(ctx)
	if err != nil {
		return err
	}
	result, err := tx.Run(ctx,
		"MERGE (n:EshuConformanceRollback {id: $id}) SET n.value = $value",
		map[string]any{"id": nodeID, "value": "must-rollback"},
	)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := result.Consume(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	result, err = tx.Run(ctx, "THIS IS NOT CYPHER", map[string]any{})
	if err == nil {
		_, err = result.Consume(ctx)
	}
	rollbackErr := tx.Rollback(ctx)
	if err != nil {
		return err
	}
	return rollbackErr
}

func executeCleanExplicitRollbackProbe(
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	nodeID string,
) error {
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: localNornicDBDefaultDatabase,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	tx, err := session.BeginTransaction(ctx)
	if err != nil {
		return err
	}
	result, err := tx.Run(ctx,
		"MERGE (n:EshuConformanceRollback {id: $id}) SET n.value = $value",
		map[string]any{"id": nodeID, "value": "must-rollback"},
	)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if _, err := result.Consume(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Rollback(ctx)
}

func countRollbackProbeNodes(
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	nodeID string,
) (int64, error) {
	return nornicDBReadCount(ctx, driver, `
MATCH (n:EshuConformanceRollback {id: $id})
RETURN count(n) AS count`, map[string]any{"id": nodeID})
}

func nornicDBReadCount(
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	cypher string,
	params map[string]any,
) (int64, error) {
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: localNornicDBDefaultDatabase,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	result, err := session.Run(ctx, strings.TrimSpace(cypher), params)
	if err != nil {
		return 0, err
	}
	record, err := result.Single(ctx)
	if err != nil {
		return 0, err
	}
	value, ok := record.Get("count")
	if !ok {
		return 0, fmt.Errorf("count column missing")
	}
	count, ok := value.(int64)
	if !ok {
		return 0, fmt.Errorf("count column type %T, want int64", value)
	}
	return count, nil
}
