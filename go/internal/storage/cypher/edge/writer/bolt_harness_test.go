// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// boltRetractTestRunner, boltTestExecutor, openBoltTestRunner,
// boltWriteStatement and boltCount duplicate the live-backend harness from
// package cypher (code_evidence_bolt_retract_test.go). Test helpers cannot be
// imported across the package split, so the edge leaf carries its own copy.

// boltRetractTestRunner wraps a Neo4j driver into a minimal cypherRunner
// equivalent to cmd/reducer's neo4jSessionRunner.RunCypherGroup path. It is the
// same dispatch chain the reducer uses: every statement's Parameters map goes
// straight to tx.Run.
type boltRetractTestRunner struct {
	driver       neo4jdriver.DriverWithContext
	databaseName string
}

func (r *boltRetractTestRunner) close(ctx context.Context) {
	_ = r.driver.Close(ctx)
}

// runCypherGroup executes a single statement inside an ExecuteWrite transaction.
// Mirrors cmd/reducer neo4jSessionRunner.RunCypherGroup.
func (r *boltRetractTestRunner) runCypherGroup(ctx context.Context, stmt sourcecypher.Statement) error {
	if r.driver == nil {
		return fmt.Errorf("neo4j driver is required")
	}

	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: r.databaseName,
	})
	defer func() { _ = session.Close(ctx) }()

	_, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		result, runErr := tx.Run(ctx, stmt.Cypher, stmt.Parameters)
		if runErr != nil {
			return nil, runErr
		}
		if _, consumeErr := result.Consume(ctx); consumeErr != nil {
			return nil, consumeErr
		}
		return nil, nil
	})
	return err
}

// runCypherSingle executes a single statement via session.Run (autocommit),
// mirroring cmd/reducer neo4jSessionRunner.RunCypher. This is the path
// that the reducer uses for single-statement Execute calls and is the path
// dispatchRetract now routes through.
func (r *boltRetractTestRunner) runCypherSingle(ctx context.Context, stmt sourcecypher.Statement) error {
	if r.driver == nil {
		return fmt.Errorf("neo4j driver is required")
	}

	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: r.databaseName,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

// runCypher runs a read query and returns the collected rows as []map[string]any.
func (r *boltRetractTestRunner) runCypher(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if r.driver == nil {
		return nil, fmt.Errorf("neo4j driver is required")
	}

	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: r.databaseName,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, err
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

// boltTestExecutor adapts the test runner to cypher.Executor for the retract
// writer dispatch path.
type boltTestExecutor struct {
	runner *boltRetractTestRunner
}

func (e *boltTestExecutor) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	return e.runner.runCypherSingle(ctx, stmt)
}

func (e *boltTestExecutor) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	for _, stmt := range stmts {
		if err := e.runner.runCypherGroup(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// openBoltTestRunner connects to the bolt DSN from ESHU_CYPHER_BOLT_DSN and
// returns a runner. Returns nil if the env var is not set.
func openBoltTestRunner(tb testing.TB) *boltRetractTestRunner {
	tb.Helper()

	dsn := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DSN"))
	if dsn == "" {
		tb.Skip("ESHU_CYPHER_BOLT_DSN not set; skipping bolt integration test")
	}

	// Derive database name from ESHU_CYPHER_BOLT_DATABASE or default to "nornic".
	database := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DATABASE"))
	if database == "" {
		database = "nornic"
	}

	driver, err := neo4jdriver.NewDriverWithContext(dsn, neo4jdriver.NoAuth())
	if err != nil {
		tb.Fatalf("open bolt driver %q: %v", dsn, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		tb.Fatalf("verify bolt connectivity %q: %v", dsn, err)
	}

	return &boltRetractTestRunner{driver: driver, databaseName: database}
}

// boltWriteStatement executes a Cypher write statement through ExecuteWrite
// for seed/setup operations.
func boltWriteStatement(ctx context.Context, runner *boltRetractTestRunner, cypher string, params map[string]any) error {
	return runner.runCypherGroup(ctx, sourcecypher.Statement{Cypher: cypher, Parameters: params})
}

// boltCount runs a read query returning a single integer count.
func boltCount(ctx context.Context, runner *boltRetractTestRunner, cypher string, params map[string]any) (int64, error) {
	rows, err := runner.runCypher(ctx, cypher, params)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	switch v := rows[0]["count"].(type) {
	case int64:
		return v, nil
	case float64:
		return int64(v), nil
	}
	return 0, fmt.Errorf("unexpected count type %T", rows[0]["count"])
}

// TestBoltRetractCodeInterprocEvidenceByUIDs_Red reproduces the bug where
// RetractCodeInterprocEvidenceByUIDs passes []string parameters to the bolt
// driver and the edges survive even though the handler reports success.
//
// Gate: ESHU_CYPHER_BOLT_DSN must be set (e.g. neo4j://127.0.0.1:17688).
// When unset the test skips.

// boltOrphanSweepReader duplicates the live-backend OrphanSweepReader from
// package cypher (orphan_sweep_antijoin_live_test.go) for the same reason.
type boltOrphanSweepReader struct {
	runner *boltRetractTestRunner
}

func (r *boltOrphanSweepReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	return r.runner.runCypher(ctx, cypher, params)
}
