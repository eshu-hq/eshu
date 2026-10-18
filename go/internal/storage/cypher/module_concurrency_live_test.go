// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

// moduleConcurrencyKeys is how many Module keys every worker in one iteration
// writes. All workers share all of them, which is the hot-key shape of real
// imports such as Go `context` and `fmt` that nearly every repository names.
const moduleConcurrencyKeys = 4

// moduleConcurrencyDefaultIterations is the per-worker-count iteration floor
// the #8168 ruling requires. ESHU_MODULE_CONCURRENCY_ITERATIONS raises it.
const moduleConcurrencyDefaultIterations = 100

// moduleConcurrencyTotals accumulates one worker count's outcome across every
// iteration, so a failure reports the duplicate and error counts, not only the
// first bad iteration.
type moduleConcurrencyTotals struct {
	duplicateIterations int
	extraNodes          int64
	edgeMismatches      int
	txErrors            int
	deadlocks           int
	firstError          string
}

// TestLiveCanonicalModuleConcurrentWritersKeepOneNodePerKey proves issue
// #8168: concurrent canonical writers that import the same modules create
// exactly one Module node per import key and one IMPORTS edge per (file,
// module), with no transaction error, on the pinned Neo4j.
//
// Each iteration starts N workers at a barrier. Every worker owns one File and
// imports the same moduleConcurrencyKeys modules; odd workers receive the rows
// in reversed order, as two repositories whose facts arrive in different order
// do. Each worker runs the production statements -- buildModuleStatements and
// buildStructuralEdgeStatements -- in ONE explicit transaction, which is how
// bootstrap-index commits a repository materialization
// (bootstrapNeo4jExecutor.ExecuteGroup). The transaction is committed with no
// driver retry, so a DeadlockDetected that a managed ExecuteWrite would absorb
// with backoff is counted here as the failure it is.
//
// N runs at 2 and at 8, the projectionWorkerCount ceiling. Without a schema
// constraint on the MERGE key, MERGE only guarantees the pattern exists, not
// that it is unique, so each transaction creates its own node for a key the
// other has not committed yet.
//
// Gate: ESHU_CYPHER_BOLT_DSN must point at a Neo4j backend; the test skips on
// any other kernel because the locking it proves is Neo4j's.
func TestLiveCanonicalModuleConcurrentWritersKeepOneNodePerKey(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()
	requireNeo4jKernel(ctx, t, runner)

	if err := graph.EnsureSchemaWithBackendStrict(ctx, boltSchemaExecutor{runner: runner}, nil, graph.SchemaBackendNeo4j); err != nil {
		t.Fatalf("apply production Neo4j schema: %v", err)
	}

	iterations := moduleConcurrencyDefaultIterations
	if raw := strings.TrimSpace(os.Getenv("ESHU_MODULE_CONCURRENCY_ITERATIONS")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < moduleConcurrencyDefaultIterations {
			t.Fatalf("ESHU_MODULE_CONCURRENCY_ITERATIONS=%q: want an integer >= %d", raw, moduleConcurrencyDefaultIterations)
		}
		iterations = parsed
	}
	runID := strconv.FormatInt(time.Now().UnixNano(), 36)
	writer := NewCanonicalNodeWriter(&boltTestExecutor{runner: runner}, DefaultBatchSize, nil)

	for _, workers := range []int{2, 8} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			prefix := fmt.Sprintf("modconc-8168-%s-w%d-", runID, workers)
			t.Cleanup(func() { cleanupModuleConcurrency(ctx, runner, prefix) })

			var totals moduleConcurrencyTotals
			for it := 0; it < iterations; it++ {
				runModuleConcurrencyIteration(ctx, t, runner, writer, fmt.Sprintf("%si%d-", prefix, it), workers, &totals)
			}
			t.Logf("workers=%d iterations=%d duplicate_iterations=%d extra_nodes=%d edge_mismatch_iterations=%d tx_errors=%d deadlocks=%d",
				workers, iterations, totals.duplicateIterations, totals.extraNodes, totals.edgeMismatches, totals.txErrors, totals.deadlocks)
			if totals.duplicateIterations != 0 || totals.edgeMismatches != 0 || totals.txErrors != 0 {
				t.Fatalf("workers=%d: %d of %d iterations created duplicate Module nodes (%d extra), %d had the wrong IMPORTS count, "+
					"%d transactions failed (%d DeadlockDetected); want all zero. First error: %s",
					workers, totals.duplicateIterations, iterations, totals.extraNodes, totals.edgeMismatches,
					totals.txErrors, totals.deadlocks, totals.firstError)
			}
		})
	}
}

// runModuleConcurrencyIteration seeds one File per worker, releases every
// worker at once, and checks the graph each iteration leaves behind.
func runModuleConcurrencyIteration(
	ctx context.Context,
	t *testing.T,
	runner *boltRetractTestRunner,
	writer *CanonicalNodeWriter,
	prefix string,
	workers int,
	totals *moduleConcurrencyTotals,
) {
	t.Helper()
	paths := make([]string, workers)
	for w := range paths {
		paths[w] = fmt.Sprintf("/eshu-test/%sf%d.go", prefix, w)
	}
	if err := boltWriteStatement(ctx, runner,
		`UNWIND $paths AS p CREATE (:File {path: p, language: 'go', lang: 'go', evidence_source: 'projector/canonical'})`,
		map[string]any{"paths": paths}); err != nil {
		t.Fatalf("seed files: %v", err)
	}

	statements := make([][]Statement, workers)
	for w := 0; w < workers; w++ {
		statements[w] = SanitizeStatements(moduleConcurrencyStatements(writer, prefix, paths[w], w%2 == 1))
	}

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		start = make(chan struct{})
	)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(stmts []Statement) {
			defer wg.Done()
			<-start
			if err := runExplicitTransaction(ctx, runner, stmts); err != nil {
				mu.Lock()
				defer mu.Unlock()
				totals.txErrors++
				if strings.Contains(err.Error(), "DeadlockDetected") {
					totals.deadlocks++
				}
				if totals.firstError == "" {
					totals.firstError = err.Error()
				}
			}
		}(statements[w])
	}
	close(start)
	wg.Wait()

	rows, err := runner.runCypher(ctx,
		`MATCH (m:Module) WHERE m.name STARTS WITH $prefix RETURN m.name AS name, count(m) AS n`,
		map[string]any{"prefix": prefix})
	if err != nil {
		t.Fatalf("count module nodes: %v", err)
	}
	duplicated := false
	for _, row := range rows {
		if n, _ := row["n"].(int64); n > 1 {
			duplicated = true
			totals.extraNodes += n - 1
		}
	}
	if duplicated {
		totals.duplicateIterations++
	}
	edges, err := boltCount(ctx, runner,
		`MATCH (f:File)-[r:IMPORTS]->(:Module) WHERE f.path IN $paths RETURN count(r) AS count`,
		map[string]any{"paths": paths})
	if err != nil {
		t.Fatalf("count imports edges: %v", err)
	}
	if edges != int64(workers*moduleConcurrencyKeys) {
		totals.edgeMismatches++
	}
}

// moduleConcurrencyStatements builds one worker's production Module and
// structural-edge statements for the shared keys under prefix.
func moduleConcurrencyStatements(writer *CanonicalNodeWriter, prefix, filePath string, reversed bool) []Statement {
	mat := canonical.CanonicalMaterialization{GenerationID: "gen-modconc", ScopeID: "scope-modconc"}
	for k := 0; k < moduleConcurrencyKeys; k++ {
		index := k
		if reversed {
			index = moduleConcurrencyKeys - 1 - k
		}
		name := fmt.Sprintf("%sk%d", prefix, index)
		mat.Modules = append(mat.Modules, canonical.ModuleRow{Name: name, Language: "go"})
		mat.Imports = append(mat.Imports, canonical.ImportRow{
			FilePath: filePath, ModuleName: name, ModuleLanguage: "go", LineNumber: k + 1,
		})
	}
	return append(writer.buildModuleStatements(mat), writer.buildStructuralEdgeStatements(mat)...)
}

// runExplicitTransaction runs stmts in one explicit transaction and commits it
// with no driver retry, so a transient error reaches the caller.
func runExplicitTransaction(ctx context.Context, runner *boltRetractTestRunner, stmts []Statement) error {
	session := runner.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: runner.databaseName,
	})
	defer func() { _ = session.Close(ctx) }()
	tx, err := session.BeginTransaction(ctx)
	if err != nil {
		return err
	}
	for _, stmt := range stmts {
		result, runErr := tx.Run(ctx, stmt.Cypher, stmt.Parameters)
		if runErr == nil {
			_, runErr = result.Consume(ctx)
		}
		if runErr != nil {
			_ = tx.Rollback(ctx)
			return runErr
		}
	}
	return tx.Commit(ctx)
}

// requireNeo4jKernel skips the test unless the backend reports the Neo4j
// kernel.
func requireNeo4jKernel(ctx context.Context, t *testing.T, runner *boltRetractTestRunner) {
	t.Helper()
	rows, err := runner.runCypher(ctx, `CALL dbms.components() YIELD name RETURN name`, nil)
	if err != nil || len(rows) == 0 {
		t.Skipf("backend did not answer dbms.components (%v); this test proves Neo4j locking", err)
	}
	if name, _ := rows[0]["name"].(string); name != "Neo4j Kernel" {
		t.Skipf("backend kernel %q is not Neo4j; this test proves Neo4j locking", name)
	}
}

// cleanupModuleConcurrency removes every node one worker-count run created.
func cleanupModuleConcurrency(ctx context.Context, runner *boltRetractTestRunner, prefix string) {
	_ = boltWriteStatement(ctx, runner,
		`MATCH (m:Module) WHERE m.name STARTS WITH $prefix DETACH DELETE m`,
		map[string]any{"prefix": prefix})
	_ = boltWriteStatement(ctx, runner,
		`MATCH (f:File) WHERE f.path STARTS WITH $prefix DETACH DELETE f`,
		map[string]any{"prefix": "/eshu-test/" + prefix})
}
