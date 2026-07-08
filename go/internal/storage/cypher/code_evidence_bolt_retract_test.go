// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

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
func (r *boltRetractTestRunner) runCypherGroup(ctx context.Context, stmt Statement) error {
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
func (r *boltRetractTestRunner) runCypherSingle(ctx context.Context, stmt Statement) error {
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

func (e *boltTestExecutor) Execute(ctx context.Context, stmt Statement) error {
	return e.runner.runCypherSingle(ctx, stmt)
}

func (e *boltTestExecutor) ExecuteGroup(ctx context.Context, stmts []Statement) error {
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
	return runner.runCypherGroup(ctx, Statement{Cypher: cypher, Parameters: params})
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
func TestBoltRetractCodeInterprocEvidenceByUIDs_Red(t *testing.T) {
	t.Parallel()

	runner := openBoltTestRunner(t)
	defer runner.close(context.Background())

	ctx := context.Background()

	const (
		testSourceUID   = "bolt-retract-test-src"
		testSinkUID     = "bolt-retract-test-sink"
		testScopeID     = "bolt-retract-test-scope"
		testEvidenceSrc = "reducer/code-interproc"
		testEdgeUID     = "bolt-retract-test-edge"
	)

	// 1. Seed Function nodes (these must exist; the writer MATCHes, not MERGEs).
	for _, uid := range []string{testSourceUID, testSinkUID} {
		if err := boltWriteStatement(
			ctx, runner,
			`MERGE (:Function {uid: $uid})`,
			map[string]any{"uid": uid},
		); err != nil {
			t.Fatalf("seed Function %q: %v", uid, err)
		}
	}

	// 2. Seed a TAINT_FLOWS_TO edge with the same properties the writer uses.
	if err := boltWriteStatement(
		ctx, runner,
		strings.Join([]string{
			`MATCH (s:Function {uid: $source_uid})`,
			`MATCH (t:Function {uid: $sink_uid})`,
			`MERGE (s)-[rel:TAINT_FLOWS_TO {evidence_uid: $evidence_uid}]->(t)`,
			`SET rel.scope_id = $scope_id,`,
			`rel.evidence_source = $evidence_source,`,
			`rel.generation_id = $generation_id`,
		}, " "),
		map[string]any{
			"source_uid":      testSourceUID,
			"sink_uid":        testSinkUID,
			"evidence_uid":    testEdgeUID,
			"scope_id":        testScopeID,
			"evidence_source": testEvidenceSrc,
			"generation_id":   "gen-1",
		},
	); err != nil {
		t.Fatalf("seed edge: %v", err)
	}

	// 3. Prove the edge exists before retract.
	g0, err := boltCount(
		ctx, runner,
		`MATCH (:Function {uid: $uid})-[rel:TAINT_FLOWS_TO]->(:Function) RETURN count(rel) AS count`,
		map[string]any{"uid": testSourceUID},
	)
	if err != nil {
		t.Fatalf("pre-retract count: %v", err)
	}
	if g0 == 0 {
		t.Fatalf("seed failed: 0 edges before retract")
	}
	t.Logf("pre-retract edge count: %d", g0)

	// 4. Retract through the REAL writer via the GroupExecutor dispatch path.
	//    This is the EXACT path the reducer uses.
	executor := &boltTestExecutor{runner: runner}
	writer := NewCodeInterprocEvidenceWriter(executor, 0)
	if err := writer.RetractCodeInterprocEvidenceByUIDs(
		ctx,
		[]string{testSourceUID},
		[]string{testScopeID},
		testEvidenceSrc,
	); err != nil {
		t.Fatalf("RetractCodeInterprocEvidenceByUIDs returned error: %v", err)
	}

	// 5. Prove the edge count after retract. THIS IS THE ASSERTION THAT MUST FAIL
	//    on the current buggy code.
	g1, err := boltCount(
		ctx, runner,
		`MATCH (:Function {uid: $uid})-[rel:TAINT_FLOWS_TO]->(:Function) RETURN count(rel) AS count`,
		map[string]any{"uid": testSourceUID},
	)
	if err != nil {
		t.Fatalf("post-retract count: %v", err)
	}
	if g1 != 0 {
		t.Fatalf("post-retract edge count = %d, want 0 — retract bug reproduced: []string params not correctly handled by bolt driver", g1)
	}
	t.Logf("post-retract edge count: %d (expected 0)", g1)
}

// TestBoltRetractCodeTaintEvidenceByUIDs_Red reproduces the same bug for the
// taint evidence writer's anchored retract.
func TestBoltRetractCodeTaintEvidenceByUIDs_Red(t *testing.T) {
	t.Parallel()

	runner := openBoltTestRunner(t)
	defer runner.close(context.Background())

	ctx := context.Background()

	const (
		testFuncUID     = "bolt-taint-test-func"
		testNodeUID     = "bolt-taint-test-node"
		testScopeID     = "bolt-taint-test-scope"
		testEvidenceSrc = "reducer/code-taint"
	)

	// 1. Seed Function node.
	if err := boltWriteStatement(
		ctx, runner,
		`MERGE (:Function {uid: $uid})`,
		map[string]any{"uid": testFuncUID},
	); err != nil {
		t.Fatalf("seed Function: %v", err)
	}

	// 2. Seed a CodeTaintEvidence node attached to the Function.
	if err := boltWriteStatement(
		ctx, runner,
		strings.Join([]string{
			`MATCH (f:Function {uid: $func_uid})`,
			`MERGE (ev:CodeTaintEvidence {uid: $node_uid})`,
			`SET ev.scope_id = $scope_id,`,
			`ev.evidence_source = $evidence_source,`,
			`ev.generation_id = $generation_id`,
			`MERGE (f)-[:HAS_TAINT_EVIDENCE]->(ev)`,
		}, " "),
		map[string]any{
			"func_uid":        testFuncUID,
			"node_uid":        testNodeUID,
			"scope_id":        testScopeID,
			"evidence_source": testEvidenceSrc,
			"generation_id":   "gen-1",
		},
	); err != nil {
		t.Fatalf("seed taint node: %v", err)
	}

	// 3. Prove node exists.
	g0, err := boltCount(
		ctx, runner,
		`MATCH (n:CodeTaintEvidence {uid: $uid}) RETURN count(n) AS count`,
		map[string]any{"uid": testNodeUID},
	)
	if err != nil {
		t.Fatalf("pre-retract count: %v", err)
	}
	if g0 == 0 {
		t.Fatalf("seed failed: 0 nodes before retract")
	}
	t.Logf("pre-retract node count: %d", g0)

	// 4. Retract through the REAL writer via GroupExecutor dispatch path.
	executor := &boltTestExecutor{runner: runner}
	writer := NewCodeTaintEvidenceWriter(executor, 0)
	if err := writer.RetractCodeTaintEvidenceByUIDs(
		ctx,
		[]string{testNodeUID},
		[]string{testScopeID},
		testEvidenceSrc,
	); err != nil {
		t.Fatalf("RetractCodeTaintEvidenceByUIDs returned error: %v", err)
	}

	// 5. Prove the node count after retract. MUST FAIL on current buggy code.
	g1, err := boltCount(
		ctx, runner,
		`MATCH (n:CodeTaintEvidence {uid: $uid}) RETURN count(n) AS count`,
		map[string]any{"uid": testNodeUID},
	)
	if err != nil {
		t.Fatalf("post-retract count: %v", err)
	}
	if g1 != 0 {
		t.Fatalf("post-retract node count = %d, want 0 — retract bug reproduced: []string params not correctly handled by bolt driver", g1)
	}
	t.Logf("post-retract node count: %d (expected 0)", g1)
}

// TestBoltUNWINDStringSliceIsolation isolates whether UNWIND with a []string
// parameter works over bolt. If UNWIND $uids AS uid where $uids is []string
// returns zero rows, the root cause is the parameter type.
func TestBoltUNWINDStringSliceIsolation(t *testing.T) {
	t.Parallel()

	runner := openBoltTestRunner(t)
	defer runner.close(context.Background())

	ctx := context.Background()

	// Try UNWIND with []string parameter.
	countStringSlice, err := boltCount(
		ctx, runner,
		`UNWIND $uids AS uid RETURN count(uid) AS count`,
		map[string]any{"uids": []string{"a", "b", "c"}},
	)
	if err != nil {
		t.Fatalf("UNWIND []string: %v", err)
	}
	t.Logf("UNWIND []string count = %d (expect 3)", countStringSlice)

	// Try UNWIND with []any parameter.
	countAnySlice, err := boltCount(
		ctx, runner,
		`UNWIND $uids AS uid RETURN count(uid) AS count`,
		map[string]any{"uids": []any{"a", "b", "c"}},
	)
	if err != nil {
		t.Fatalf("UNWIND []any: %v", err)
	}
	t.Logf("UNWIND []any count = %d (expect 3)", countAnySlice)

	// If []string returns 0 and []any returns 3, []string is the root cause.
	if countStringSlice == 0 && countAnySlice == 3 {
		t.Logf("ROOT CAUSE CONFIRMED: UNWIND with []string returns 0 rows over bolt; []any works correctly")
	}
}
