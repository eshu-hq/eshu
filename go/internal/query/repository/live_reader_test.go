// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Shared live-backend plumbing for this package's live answer-truth tests:
// the database-name default, a test-only GraphQuery + graph.CypherExecutor
// over the Bolt driver, and a bounded retry for transient seed-write
// conflicts.
package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	eshugraph "github.com/eshu-hq/eshu/go/internal/graph"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// liveGraphDatabaseName returns ESHU_LIVE_GRAPH_DATABASE when set, or the
// pinned image's default database name for backend ("nornic" for NornicDB,
// "neo4j" for Neo4j).
func liveGraphDatabaseName(backend string) string {
	if db := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_DATABASE")); db != "" {
		return db
	}
	if backend == "nornicdb" {
		return "nornic"
	}
	return "neo4j"
}

// repositoryLiveReader is the test-only live GraphQuery + graph.CypherExecutor
// for this file. The package cannot import root query's Neo4jReader without a
// cycle (same rationale as entity's entityLiveReader).
type repositoryLiveReader struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (r repositoryLiveReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: r.database})
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

func (r repositoryLiveReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// ExecuteCypher implements graph.CypherExecutor so this reader can also drive
// graph.EnsureSchemaWithBackend.
func (r repositoryLiveReader) ExecuteCypher(ctx context.Context, stmt eshugraph.CypherStatement) error {
	return runLiveWriteWithRetry(ctx, func(ctx context.Context) error {
		session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: r.database})
		defer func() { _ = session.Close(ctx) }()
		result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
		if err != nil {
			return err
		}
		_, err = result.Consume(ctx)
		return err
	})
}

func (r repositoryLiveReader) write(ctx context.Context, t *testing.T, cypher string) {
	t.Helper()
	err := runLiveWriteWithRetry(ctx, func(ctx context.Context) error {
		session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: r.database})
		defer func() { _ = session.Close(ctx) }()
		result, err := session.Run(ctx, cypher, nil)
		if err != nil {
			return err
		}
		_, err = result.Consume(ctx)
		return err
	})
	if err != nil {
		t.Fatalf("write %q: %v", cypher, err)
	}
}

// liveWriteMaxAttempts bounds the retry count for a transient graph-write
// conflict (see isLiveWriteTransientError). Go runs different test packages
// concurrently by default even with no t.Parallel(), so this package's live
// test and a sibling package's live test (e.g. codequery's) can race writes
// against the same shared NornicDB/Neo4j instance in CI (#6784).
const liveWriteMaxAttempts = 5

// liveWriteRetryDelay is a small linear backoff for a racing test-seed/
// cleanup/schema write, not a production retry policy: these are one-shot
// DDL/seed statements contending with a sibling test package, not a
// production hot path.
func liveWriteRetryDelay(attempt int) time.Duration {
	return time.Duration(attempt) * 100 * time.Millisecond
}

// isLiveWriteTransientError reports whether err is a transient, safe-to-retry
// write conflict -- observed live as
// "Neo.TransientError.Transaction.Outdated ... Please retry" when two test
// packages' live writes race the same shared NornicDB/Neo4j instance.
func isLiveWriteTransientError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "TransientError")
}

// runLiveWriteWithRetry runs run, retrying up to liveWriteMaxAttempts times
// with a small backoff on a transient write conflict, and returning
// immediately on success or a non-transient error.
func runLiveWriteWithRetry(ctx context.Context, run func(context.Context) error) error {
	var lastErr error
	for attempt := 1; attempt <= liveWriteMaxAttempts; attempt++ {
		lastErr = run(ctx)
		if lastErr == nil {
			return nil
		}
		if !isLiveWriteTransientError(lastErr) || attempt == liveWriteMaxAttempts {
			return lastErr
		}
		time.Sleep(liveWriteRetryDelay(attempt))
	}
	return lastErr
}
