// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	reducerNeo4jCloseTimeout             = 10 * time.Second
	defaultNornicDBCanonicalWriteTimeout = 30 * time.Second
	canonicalWriteTimeoutEnv             = "ESHU_CANONICAL_WRITE_TIMEOUT"
	nornicDBCanonicalGroupedWritesEnv    = "ESHU_NORNICDB_CANONICAL_GROUPED_WRITES"
	nornicDBSemanticEntityLabelBatchEnv  = "ESHU_NORNICDB_SEMANTIC_ENTITY_LABEL_BATCH_SIZES"
)

// neo4jSessionRunner wraps a Neo4j driver into the cypherRunner interface,
// opening a write session per call.
type neo4jSessionRunner struct {
	Driver       neo4jdriver.DriverWithContext
	DatabaseName string
	TxTimeout    time.Duration
}

func (r neo4jSessionRunner) RunCypher(ctx context.Context, cypher string, params map[string]any) error {
	if r.Driver == nil {
		return fmt.Errorf("neo4j driver is required")
	}

	session := r.Driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: r.DatabaseName,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, cypher, params, r.transactionConfigurers()...)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

// RunCypherGroup executes multiple Cypher statements inside a single write
// transaction. session.ExecuteWrite automatically retries the entire function
// on transient errors (deadlocks, leader switches), giving us atomic
// retract+upsert with built-in resilience.
func (r neo4jSessionRunner) RunCypherGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	if r.Driver == nil {
		return fmt.Errorf("neo4j driver is required")
	}
	if len(stmts) == 0 {
		return nil
	}

	session := r.Driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: r.DatabaseName,
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
	}, r.transactionConfigurers()...)
	return err
}

func (r neo4jSessionRunner) transactionConfigurers() []func(*neo4jdriver.TransactionConfig) {
	if r.TxTimeout <= 0 {
		return nil
	}
	return []func(*neo4jdriver.TransactionConfig){neo4jdriver.WithTxTimeout(r.TxTimeout)}
}

// QueryCypherExists runs a read-only Cypher query and returns true if at
// least one row is returned. This implements neo4j.CypherReader for the
// CanonicalNodeChecker pre-flight check.
func (r neo4jSessionRunner) QueryCypherExists(ctx context.Context, cypher string, params map[string]any) (bool, error) {
	if r.Driver == nil {
		return false, fmt.Errorf("neo4j driver is required")
	}

	session := r.Driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: r.DatabaseName,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return false, err
	}
	hasNext := result.Next(ctx)
	if err := result.Err(); err != nil {
		return false, err
	}
	return hasNext, nil
}

// Run executes a read-only Cypher query and returns row maps. This implements
// query.GraphQuery for reducer-local graph lookups.
func (r neo4jSessionRunner) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if r.Driver == nil {
		return nil, fmt.Errorf("neo4j driver is required")
	}

	session := r.Driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: r.DatabaseName,
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

// RunSingle executes a read-only Cypher query and returns the first row.
func (r neo4jSessionRunner) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// reducerNeo4jDriverCloser wraps driver close with a timeout.
type reducerNeo4jDriverCloser struct {
	Driver neo4jdriver.DriverWithContext
}

func (c reducerNeo4jDriverCloser) Close() error {
	if c.Driver == nil {
		return nil
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), reducerNeo4jCloseTimeout)
	defer cancel()
	return c.Driver.Close(closeCtx)
}

// openReducerNeo4jAdapters opens a Neo4j driver and returns the executor
// adapters needed by the reducer: one for EdgeWriter (sourcecypher.Executor),
// one for WorkloadMaterializer (reducer.CypherExecutor), and one for
// pre-flight canonical node checks (sourcecypher.CypherReader). Both
// executor adapters hold a persistent *sourcecypher.RetryingExecutor built
// once here (#5048) rather than a fresh one per call (the removed
// executeReducerCypherWithRetry); instruments is wired onto both so
// Neo4jDeadlockRetries is actually recorded -- the per-call construction
// this replaces omitted it entirely.
func openReducerNeo4jAdapters(
	parent context.Context,
	getenv func(string) string,
	instruments *telemetry.Instruments,
) (sourcecypher.Executor, reducer.CypherExecutor, sourcecypher.CypherReader, query.GraphQuery, io.Closer, error) {
	graphBackend, err := runtimecfg.LoadGraphBackend(getenv)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	driver, cfg, err := runtimecfg.OpenNeo4jDriver(parent, getenv)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	runner := neo4jSessionRunner{
		Driver:       driver,
		DatabaseName: cfg.DatabaseName,
		TxTimeout:    reducerTransactionTimeout(graphBackend, getenv),
	}

	return newReducerNeo4jExecutor(runner, instruments),
		newReducerCypherExecutor(runner, instruments),
		runner,
		runner,
		reducerNeo4jDriverCloser{Driver: driver},
		nil
}

func reducerTransactionTimeout(graphBackend runtimecfg.GraphBackend, getenv func(string) string) time.Duration {
	if graphBackend != runtimecfg.GraphBackendNornicDB {
		return 0
	}
	return nornicDBCanonicalWriteTimeout(getenv)
}

func semanticEntityExecutorForGraphBackend(
	rawExecutor sourcecypher.Executor,
	graphBackend runtimecfg.GraphBackend,
	nornicDBTimeout time.Duration,
	nornicDBGroupedWrites bool,
) sourcecypher.Executor {
	if graphBackend == runtimecfg.GraphBackendNornicDB {
		bounded := sourcecypher.TimeoutExecutor{
			Inner:       rawExecutor,
			Timeout:     nornicDBTimeout,
			TimeoutHint: canonicalWriteTimeoutEnv,
		}
		if nornicDBGroupedWrites {
			return bounded
		}
		return sourcecypher.ExecuteOnlyExecutor{Inner: nornicDBSemanticObservedExecutor{inner: bounded}}
	}
	return rawExecutor
}

func semanticEntityWriterForGraphBackend(
	executor sourcecypher.Executor,
	batchSize int,
	graphBackend runtimecfg.GraphBackend,
	getenv func(string) string,
) (*sourcecypher.SemanticEntityWriter, error) {
	writer := sourcecypher.NewSemanticEntityWriter(executor, batchSize)
	if graphBackend == runtimecfg.GraphBackendNornicDB {
		// NornicDB's batch executor is template-sensitive: putting MATCH before
		// MERGE is indexed but misses the generalized UNWIND/MERGE hot path.
		// Use merge-first explicit row templates, but let source-local canonical
		// projection retain ownership of File CONTAINS edges for canonical entity
		// labels. That avoids repeated relationship-existence checks as the graph
		// grows while still preserving Module's semantic-owned uid nodes.
		writer = sourcecypher.NewSemanticEntityWriterWithCanonicalNodeRows(executor, batchSize).
			WithLabelScopedRetract().
			WithSequentialRetract()
		labelBatchSizes, err := nornicDBSemanticEntityLabelBatchSizes(getenv, effectiveNeo4jBatchSize(batchSize))
		if err != nil {
			return nil, err
		}
		for label, size := range labelBatchSizes {
			writer = writer.WithEntityLabelBatchSize(label, size)
		}
	}
	return writer, nil
}

func nornicDBCanonicalWriteTimeout(getenv func(string) string) time.Duration {
	raw := strings.TrimSpace(getenv(canonicalWriteTimeoutEnv))
	if raw == "" {
		return defaultNornicDBCanonicalWriteTimeout
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return defaultNornicDBCanonicalWriteTimeout
	}
	return parsed
}

func nornicDBCanonicalGroupedWrites(getenv func(string) string) (bool, error) {
	raw := strings.TrimSpace(getenv(nornicDBCanonicalGroupedWritesEnv))
	if raw == "" {
		return false, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("parse %s=%q: %w", nornicDBCanonicalGroupedWritesEnv, raw, err)
	}
	return enabled, nil
}

func neo4jBatchSize(getenv func(string) string) int {
	raw := strings.TrimSpace(getenv("ESHU_NEO4J_BATCH_SIZE"))
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func effectiveNeo4jBatchSize(batchSize int) int {
	if batchSize <= 0 {
		return sourcecypher.DefaultBatchSize
	}
	return batchSize
}

func defaultNornicDBSemanticEntityLabelBatchSizes(batchSize int) map[string]int {
	return map[string]int{
		// Full-corpus timing showed small Annotation and TypeAlias batches can
		// consume most of the bounded NornicDB write budget, so keep them below
		// the broader semantic default until the backend path is faster.
		"Annotation": minPositiveInt(batchSize, 5),
		"Function":   minPositiveInt(batchSize, 10),
		"Variable":   minPositiveInt(batchSize, 10),
		// Module rows can carry declaration-merge metadata, and the self-repo
		// dogfood run showed the 45-row statement exceeds NornicDB's bounded
		// semantic write timeout. Keep this family narrow by default.
		"Module": minPositiveInt(batchSize, 10),
		// Rust impl blocks carry trait/receiver context; the self-repo run
		// showed the 103-row family needs the same narrow default.
		"ImplBlock":      minPositiveInt(batchSize, 10),
		"TypeAlias":      minPositiveInt(batchSize, 5),
		"TypeAnnotation": minPositiveInt(batchSize, 50),
	}
}

func nornicDBSemanticEntityLabelBatchSizes(getenv func(string) string, batchSize int) (map[string]int, error) {
	labelBatchSizes := defaultNornicDBSemanticEntityLabelBatchSizes(batchSize)
	raw := strings.TrimSpace(getenv(nornicDBSemanticEntityLabelBatchEnv))
	if raw == "" {
		return labelBatchSizes, nil
	}
	for _, entry := range strings.Split(raw, ",") {
		parts := strings.Split(strings.TrimSpace(entry), "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("parse %s=%q: entries must be Label=size", nornicDBSemanticEntityLabelBatchEnv, raw)
		}
		label := strings.TrimSpace(parts[0])
		if label == "" {
			return nil, fmt.Errorf("parse %s=%q: label must be non-empty", nornicDBSemanticEntityLabelBatchEnv, raw)
		}
		size, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || size <= 0 {
			return nil, fmt.Errorf("parse %s=%q: label %q must have a positive integer size", nornicDBSemanticEntityLabelBatchEnv, raw, label)
		}
		labelBatchSizes[label] = minPositiveInt(batchSize, size)
	}
	return labelBatchSizes, nil
}

func minPositiveInt(left, right int) int {
	if left <= 0 {
		return right
	}
	if right <= 0 {
		return left
	}
	if left < right {
		return left
	}
	return right
}
