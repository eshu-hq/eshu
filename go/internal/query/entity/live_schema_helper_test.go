// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Shared live-backend plumbing for #6786's scoped-grant proof
// (scoped_grant_live_test.go): applying Eshu's real schema before seeding,
// and resolving which backend/database a run targets from environment
// variables. NornicDB's read-predicate behavior differs materially with the
// schema applied versus a bare container -- see
// docs/internal/evidence/6786-scoped-grant-nornicdb-read-predicates.md -- so
// every live test in this file's sibling MUST apply schema first.
package entity

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// liveGraphBackend resolves which schema dialect and default database a live
// scoped-grant test run targets, from ESHU_LIVE_GRAPH_BACKEND
// (nornicdb|neo4j, default nornicdb -- matching this epic's primary subject)
// and ESHU_LIVE_GRAPH_DATABASE (default "nornic" for nornicdb, "neo4j" for
// neo4j). ESHU_LIVE_GRAPH_DATABASE is the shared #6784/#6786 env contract
// name; keep it in sync with deployment's selectorLiveGraphBackend.
func liveGraphBackend() (graph.SchemaBackend, string) {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND")))
	database := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_DATABASE"))
	if backend == "neo4j" {
		if database == "" {
			database = "neo4j"
		}
		return graph.SchemaBackendNeo4j, database
	}
	if database == "" {
		database = "nornic"
	}
	return graph.SchemaBackendNornicDB, database
}

// liveWriteMaxAttempts bounds retries for a transient write conflict (e.g.
// NornicDB/Neo4j's Neo.TransientError.Transaction.Outdated) a live schema
// apply or seed write can hit. #6784 runs this package's and deployment's
// live tests as separate Go packages in one `go test` invocation, and Go
// runs different packages' tests concurrently by default; two packages
// seeding/cleaning against the SAME live database (they use disjoint id
// prefixes, but NornicDB's own internal write/commit path can still
// serialize and reject a colliding transaction) can hit exactly this
// transient conflict. A sequential run (`go test -p 1`) never retries here:
// it has no concurrent writer to conflict with.
const liveWriteMaxAttempts = 5

// liveWriteRetryBaseDelay is the backoff unit between retries; attempt N
// waits N * this duration before retrying.
const liveWriteRetryBaseDelay = 75 * time.Millisecond

// retryLiveWrite runs op up to liveWriteMaxAttempts times, retrying only
// when op's error is a Neo4j/NornicDB-classified transient error
// (neo4jdriver.IsRetryable -- true for Neo.TransientError.* codes such as
// Transaction.Outdated). Any other error, or exhausting every attempt,
// returns immediately with that error.
func retryLiveWrite(ctx context.Context, op func() error) error {
	var lastErr error
	for attempt := 1; attempt <= liveWriteMaxAttempts; attempt++ {
		lastErr = op()
		if lastErr == nil {
			return nil
		}
		if !neo4jdriver.IsRetryable(lastErr) || attempt == liveWriteMaxAttempts {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * liveWriteRetryBaseDelay):
		}
	}
	return lastErr
}

// liveSchemaExecutor adapts a Bolt driver session to graph.CypherExecutor so
// EnsureSchemaWithBackendStrict can apply the real schema DDL through the
// same driver and database a live test reads and writes through.
type liveSchemaExecutor struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

// ExecuteCypher satisfies graph.CypherExecutor.
func (e liveSchemaExecutor) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	return retryLiveWrite(ctx, func() error {
		session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
			AccessMode:   neo4jdriver.AccessModeWrite,
			DatabaseName: e.database,
		})
		defer func() { _ = session.Close(ctx) }()
		result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
		if err != nil {
			return err
		}
		_, err = result.Consume(ctx)
		return err
	})
}
