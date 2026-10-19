// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// The fakes below are leaf-local copies of the root cypher package's test
// doubles. Test fakes cannot be imported across the package split, so each
// side carries its own copy; keep the copies behavior-identical.

type recordingExecutor struct {
	calls     []sourcecypher.Statement
	errAtCall error

	// readCalls records every Reader.Run invocation (used by shell-exec
	// orphan-cleanup retract tests, the only production path that reads
	// through EdgeWriter.Reader). readCandidates/readConnected script the S1
	// candidate keys and which of those currently have a relationship (S2);
	// both are nil by default, so a test that never exercises shell-exec
	// cleanup reads sees zero candidates and the production code performs no
	// further read or write.
	readCalls      []sourcecypher.Statement
	readCandidates []string
	readConnected  map[string]bool
	readErr        error
}

func (r *recordingExecutor) Execute(_ context.Context, statement sourcecypher.Statement) error {
	r.calls = append(r.calls, statement)
	if r.errAtCall != nil {
		return r.errAtCall
	}

	return nil
}

// Run implements orphan.OrphanSweepReader so recordingExecutor can double
// as the EdgeWriter.Reader in shell-exec orphan-cleanup retract tests. It
// distinguishes the S1 candidate-keys read (no "keys" parameter) from the S2
// connected-keys read (a "keys" parameter) the same way the production
// statements do.
func (r *recordingExecutor) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	r.readCalls = append(r.readCalls, sourcecypher.Statement{Cypher: cypher, Parameters: params})
	if r.readErr != nil {
		return nil, r.readErr
	}
	if keys, ok := params["keys"].([]string); ok {
		rows := make([]map[string]any, 0, len(keys))
		for _, k := range keys {
			if r.readConnected[k] {
				rows = append(rows, map[string]any{"key": k})
			}
		}
		return rows, nil
	}
	rows := make([]map[string]any, 0, len(r.readCandidates))
	for _, k := range r.readCandidates {
		rows = append(rows, map[string]any{"key": k})
	}
	return rows, nil
}

// recordingGroupExecutor duplicates the recording fake from the root cypher
// package (recording_group_executor_test.go). Test fakes cannot be imported
// across the package split, so each side carries its own copy.

type recordingGroupExecutor struct {
	executeCalls []sourcecypher.Statement
	groupCalls   [][]sourcecypher.Statement
}

func (r *recordingGroupExecutor) Execute(_ context.Context, stmt sourcecypher.Statement) error {
	r.executeCalls = append(r.executeCalls, stmt)
	return nil
}

func (r *recordingGroupExecutor) ExecuteGroup(_ context.Context, stmts []sourcecypher.Statement) error {
	cloned := make([]sourcecypher.Statement, len(stmts))
	copy(cloned, stmts)
	r.groupCalls = append(r.groupCalls, cloned)
	return nil
}

// noopGroupExecutor records nothing and returns nil so the benchmark measures
// statement construction and batching cost, not backend round trips.
type noopGroupExecutor struct{}

func (noopGroupExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (noopGroupExecutor) ExecuteGroup(context.Context, []sourcecypher.Statement) error {
	return nil
}

// dispatchRouteRecorder is a GroupExecutor-capable fake that records which
// dispatch route each statement took: Execute (single, autocommit) versus
// ExecuteGroup (batched, session.ExecuteWrite).
type dispatchRouteRecorder struct {
	executeCyphers []string
	groupCyphers   []string
}

func (r *dispatchRouteRecorder) Execute(_ context.Context, statement sourcecypher.Statement) error {
	r.executeCyphers = append(r.executeCyphers, statement.Cypher)
	return nil
}

func (r *dispatchRouteRecorder) ExecuteGroup(_ context.Context, statements []sourcecypher.Statement) error {
	for _, statement := range statements {
		r.groupCyphers = append(r.groupCyphers, statement.Cypher)
	}
	return nil
}

// echoingPostureExistenceReader is the default PostureExistenceReader test
// double for the posture node writers. With ExistingUIDs left nil it
// answers every candidate uid in the read as existing, so most writer tests
// do not need bespoke read-existence wiring; set ExistingUIDs to restrict
// which uids are reported as existing (used by never-create tests).
type echoingPostureExistenceReader struct {
	ExistingUIDs map[string]bool
	err          error
	calls        []map[string]any
}

func (r *echoingPostureExistenceReader) Run(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
	r.calls = append(r.calls, params)
	if r.err != nil {
		return nil, r.err
	}
	candidates, _ := params["candidate_uids"].([]any)
	rows := make([]map[string]any, 0, len(candidates))
	for _, c := range candidates {
		uid, _ := c.(string)
		if uid == "" {
			continue
		}
		if r.ExistingUIDs != nil && !r.ExistingUIDs[uid] {
			continue
		}
		rows = append(rows, map[string]any{"existing_uid": uid})
	}
	return rows, nil
}

// boltRetractTestRunner wraps a Neo4j driver into a minimal runner for the
// live bolt tests. It is the same dispatch chain the reducer uses: every
// statement's Parameters map goes straight to tx.Run.
type boltRetractTestRunner struct {
	driver       neo4jdriver.DriverWithContext
	databaseName string
}

func (r *boltRetractTestRunner) close(ctx context.Context) {
	_ = r.driver.Close(ctx)
}

// runCypherGroup executes a single statement inside an ExecuteWrite transaction.
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

// runCypherSingle executes a single statement via session.Run (autocommit).
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

// boltTestExecutor adapts the test runner to sourcecypher.Executor for the
// retract writer dispatch path.
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
