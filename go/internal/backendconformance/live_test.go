package backendconformance

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	liveConformanceEnv      = "ESHU_BACKEND_CONFORMANCE_LIVE"
	liveWriteAttempts       = 2
	liveWriteAttemptTimeout = 45 * time.Second
	liveReadTimeout         = 30 * time.Second
	liveTestTimeout         = time.Duration(liveWriteAttempts)*liveWriteAttemptTimeout + liveReadTimeout
)

// TestLiveBackendConformance exercises the shared corpus against a real Bolt
// backend when explicitly enabled by Compose, CI, or a maintainer run.
func TestLiveBackendConformance(t *testing.T) {
	if !liveConformanceEnabled() {
		t.Skipf("set %s=1 to run live backend conformance", liveConformanceEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), liveTestTimeout)
	defer cancel()

	backend, err := runtimecfg.LoadGraphBackend(os.Getenv)
	if err != nil {
		t.Fatalf("load graph backend: %v", err)
	}

	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, os.Getenv)
	if err != nil {
		t.Fatalf("open Bolt driver: %v", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		if err := driver.Close(closeCtx); err != nil {
			t.Fatalf("close Bolt driver: %v", err)
		}
	}()

	executor := liveCypherExecutor{
		driver:   driver,
		database: cfg.DatabaseName,
	}
	if err := cleanupLiveCorpus(ctx, executor); err != nil {
		t.Fatalf("clean live corpus fixture: %v", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := cleanupLiveCorpus(cleanupCtx, executor); err != nil {
			t.Fatalf("cleanup live corpus fixture: %v", err)
		}
	}()

	for attempt := 1; attempt <= liveWriteAttempts; attempt++ {
		attemptCtx, attemptCancel := context.WithTimeout(ctx, liveWriteAttemptTimeout)
		if backend == runtimecfg.GraphBackendNornicDB {
			if _, err := RunPhaseWriteCorpus(attemptCtx, executor, DefaultWriteCorpus()); err != nil {
				attemptCancel()
				t.Fatalf("run %s live write corpus attempt %d: %v", backend, attempt, err)
			}
		} else {
			if _, err := RunWriteCorpus(attemptCtx, executor, DefaultWriteCorpus()); err != nil {
				attemptCancel()
				t.Fatalf("run %s live write corpus attempt %d: %v", backend, attempt, err)
			}
		}
		attemptCancel()
	}

	readCtx, readCancel := context.WithTimeout(ctx, liveReadTimeout)
	defer readCancel()
	if _, err := RunReadCorpus(readCtx, executor, DefaultReadCorpus()); err != nil {
		t.Fatalf("run %s live read corpus: %v", backend, err)
	}
}

// liveConformanceEnabled keeps the live suite opt-in so default package tests
// stay deterministic and do not require Docker or remote graph credentials.
func liveConformanceEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(liveConformanceEnv))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// liveCypherExecutor adapts the shared Bolt driver to the read and write
// conformance ports used by the package corpus.
type liveCypherExecutor struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

// Run executes one read-only Cypher query and returns rows as plain maps.
func (e liveCypherExecutor) Run(
	ctx context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	if e.driver == nil {
		return nil, fmt.Errorf("Bolt driver is required")
	}

	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: e.database,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, fmt.Errorf("run read query: %w", err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("collect read query: %w", err)
	}

	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		row := make(map[string]any, len(record.Keys))
		for i, key := range record.Keys {
			row[key] = record.Values[i]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// RunSingle returns the first row from Run, matching the GraphQuery contract
// used by API readers.
func (e liveCypherExecutor) RunSingle(
	ctx context.Context,
	cypher string,
	params map[string]any,
) (map[string]any, error) {
	rows, err := e.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// Execute runs one write statement and consumes the result so backend errors
// are surfaced before the corpus advances.
func (e liveCypherExecutor) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	if e.driver == nil {
		return fmt.Errorf("Bolt driver is required")
	}

	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.database,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return fmt.Errorf("run write statement: %w", err)
	}
	if _, err := result.Consume(ctx); err != nil {
		return fmt.Errorf("consume write statement: %w", err)
	}
	return nil
}

// ExecuteGroup commits a statement group in one backend transaction for
// adapters that advertise atomic grouped writes.
func (e liveCypherExecutor) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	if e.driver == nil {
		return fmt.Errorf("Bolt driver is required")
	}
	if len(stmts) == 0 {
		return nil
	}

	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.database,
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
	if err != nil {
		return fmt.Errorf("execute write group: %w", err)
	}
	return nil
}

// ExecutePhaseGroup preserves the phase-group conformance surface used by the
// NornicDB canonical write path.
func (e liveCypherExecutor) ExecutePhaseGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	return e.ExecuteGroup(ctx, stmts)
}

// cleanupLiveCorpus removes only nodes created by DefaultWriteCorpus, keeping
// live proofs safe to run against developer Compose databases.
func cleanupLiveCorpus(ctx context.Context, executor liveCypherExecutor) error {
	cleanup := []sourcecypher.Statement{
		{
			Operation: sourcecypher.OperationCanonicalRetract,
			Cypher: `MATCH (caller:Function {uid: $caller_uid})-[rel:CALLS]->(callee:Function {uid: $callee_uid})
DELETE rel`,
			Parameters: map[string]any{
				"caller_uid": "function:backend-conformance:caller",
				"callee_uid": "function:backend-conformance:callee",
			},
		},
		{
			Operation:  sourcecypher.OperationCanonicalRetract,
			Cypher:     `MATCH (n:Function {uid: $entity_uid}) DETACH DELETE n`,
			Parameters: map[string]any{"entity_uid": "function:backend-conformance:file-entity"},
		},
		{
			Operation:  sourcecypher.OperationCanonicalRetract,
			Cypher:     `MATCH (f:File {path: $file_path}) DETACH DELETE f`,
			Parameters: map[string]any{"file_path": "backend-conformance/src/example.go"},
		},
		{
			Operation:  sourcecypher.OperationCanonicalRetract,
			Cypher:     `MATCH (d:Directory {path: $dir_path}) DETACH DELETE d`,
			Parameters: map[string]any{"dir_path": "backend-conformance/src"},
		},
		{
			Operation:  sourcecypher.OperationCanonicalRetract,
			Cypher:     `MATCH (caller:Function {uid: $caller_uid}) DELETE caller`,
			Parameters: map[string]any{"caller_uid": "function:backend-conformance:caller"},
		},
		{
			Operation:  sourcecypher.OperationCanonicalRetract,
			Cypher:     `MATCH (callee:Function {uid: $callee_uid}) DELETE callee`,
			Parameters: map[string]any{"callee_uid": "function:backend-conformance:callee"},
		},
		{
			Operation:  sourcecypher.OperationCanonicalRetract,
			Cypher:     `MATCH (r:Repository {id: $repo_id}) DETACH DELETE r`,
			Parameters: map[string]any{"repo_id": "repo:backend-conformance"},
		},
	}
	return executor.ExecuteGroup(ctx, cleanup)
}
