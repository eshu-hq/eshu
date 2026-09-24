// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

	"github.com/eshu-hq/eshu/go/internal/reducer"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestLiveNeo4jCanonicalWriteTimeoutAbortsBlockedWrite proves on a real Neo4j
// server that ESHU_CANONICAL_WRITE_TIMEOUT now bounds reducer graph writes.
// The runner is built from reducerTransactionTimeout for the Neo4j backend and
// driven through newReducerNeo4jExecutor, the production retry seam. Two
// shapes are covered:
//
//   - lock wait: a holder transaction keeps the write lock on the probe node,
//     so the contender blocks until the server terminates it. Neo4j reports
//     Neo.ClientError.Transaction.LockClientStopped for this shape.
//   - long statement: the contender runs a CPU-bound statement that outlives
//     the timeout. Neo4j reports
//     Neo.ClientError.Transaction.TransactionTimedOutClientConfiguration.
//
// Each shape must abort near the configured timeout, make exactly one attempt
// (a timed-out write must not be replayed locally for another full timeout
// while its lease is held), stay terminal outside the replay-safe RUNS_ON
// groups, and leave no committed contender write behind.
//
// Run with a disposable Neo4j (auth disabled):
//
//	ESHU_NEO4J_WRITE_TIMEOUT_LIVE=1 ESHU_NEO4J_URI=bolt://127.0.0.1:<port> \
//	  go test ./cmd/reducer -run TestLiveNeo4jCanonicalWriteTimeoutAbortsBlockedWrite -count=1 -v
func TestLiveNeo4jCanonicalWriteTimeoutAbortsBlockedWrite(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_NEO4J_WRITE_TIMEOUT_LIVE")) == "" {
		t.Skip("set ESHU_NEO4J_WRITE_TIMEOUT_LIVE=1 to run the Neo4j write-timeout proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()

	const configured = 2 * time.Second
	getenv := func(key string) string {
		if key == canonicalWriteTimeoutEnv {
			return configured.String()
		}
		return ""
	}
	txTimeout := reducerTransactionTimeout(runtimecfg.GraphBackendNeo4j, getenv)
	if txTimeout != configured {
		t.Fatalf("reducerTransactionTimeout(neo4j) = %s, want %s", txTimeout, configured)
	}

	tests := []struct {
		name     string
		holdLock bool
		cypher   string
		wantCode string
	}{
		{
			name:     "lock wait",
			holdLock: true,
			cypher:   `MATCH (n:Neo4jWriteTimeoutProbe {probe: $probe}) SET n.value = 'contender'`,
			wantCode: "Neo.ClientError.Transaction.LockClientStopped",
		},
		{
			name: "long statement",
			cypher: `MATCH (n:Neo4jWriteTimeoutProbe {probe: $probe})
UNWIND range(1, 2000000000) AS x
WITH n, sum(x) AS total
SET n.value = 'contender-' + toString(total)`,
			wantCode: "Neo.ClientError.Transaction.TransactionTimedOutClientConfiguration",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			probe := fmt.Sprintf("neo4j-write-timeout-%d", time.Now().UnixNano())
			setup := neo4jSessionRunner{Driver: driver}
			if err := setup.RunCypher(ctx, `CREATE (:Neo4jWriteTimeoutProbe {probe: $probe, value: 'seed'})`,
				map[string]any{"probe": probe}); err != nil {
				t.Fatalf("seed probe: %v", err)
			}
			t.Cleanup(func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cleanupCancel()
				_ = setup.RunCypher(cleanupCtx, `MATCH (n:Neo4jWriteTimeoutProbe {probe: $probe}) DETACH DELETE n`,
					map[string]any{"probe": probe})
			})

			wantValue := "seed"
			var holder neo4jdriver.ExplicitTransaction
			if tt.holdLock {
				holderSession := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite})
				defer func() { _ = holderSession.Close(context.Background()) }()
				holder, err = holderSession.BeginTransaction(ctx)
				if err != nil {
					t.Fatalf("begin holder transaction: %v", err)
				}
				if _, err := holder.Run(ctx, `MATCH (n:Neo4jWriteTimeoutProbe {probe: $probe}) SET n.value = 'holder'`,
					map[string]any{"probe": probe}); err != nil {
					t.Fatalf("holder takes write lock: %v", err)
				}
				wantValue = "holder"
			}

			runner := &countingCypherRunner{inner: neo4jSessionRunner{Driver: driver, TxTimeout: txTimeout}}
			executor := newReducerNeo4jExecutor(runner, nil)
			start := time.Now()
			writeErr := executor.ExecuteGroup(ctx, []sourcecypher.Statement{{
				Operation:  sourcecypher.OperationCanonicalUpsert,
				Cypher:     tt.cypher,
				Parameters: map[string]any{"probe": probe},
			}})
			elapsed := time.Since(start)
			t.Logf("contender returned after %s (attempts=%d): %v", elapsed, runner.groups, writeErr)

			var neo4jErr *neo4jdriver.Neo4jError
			if !errors.As(writeErr, &neo4jErr) {
				t.Fatalf("contender error = %v, want typed Neo4j termination", writeErr)
			}
			if neo4jErr.Code != tt.wantCode {
				t.Fatalf("contender error code = %q, want %q", neo4jErr.Code, tt.wantCode)
			}
			if elapsed < configured || elapsed > configured+10*time.Second {
				t.Fatalf("contender aborted after %s, want about %s", elapsed, configured)
			}
			if runner.groups != 1 {
				t.Fatalf("group attempts = %d, want 1: a timed-out write must not retry locally", runner.groups)
			}
			if reducer.IsRetryable(writeErr) {
				t.Fatalf("contender error = %v, want terminal outside replay-safe RUNS_ON groups", writeErr)
			}

			if holder != nil {
				if err := holder.Commit(ctx); err != nil {
					t.Fatalf("commit holder: %v", err)
				}
			}
			readSession := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead})
			defer func() { _ = readSession.Close(context.Background()) }()
			result, err := readSession.Run(ctx, `MATCH (n:Neo4jWriteTimeoutProbe {probe: $probe}) RETURN n.value AS value`,
				map[string]any{"probe": probe})
			if err != nil {
				t.Fatalf("read probe: %v", err)
			}
			record, err := result.Single(ctx)
			if err != nil {
				t.Fatalf("read probe record: %v", err)
			}
			if value, _ := record.Get("value"); value != wantValue {
				t.Fatalf("probe value = %v, want %s: the timed-out write must roll back", value, wantValue)
			}
		})
	}
}

// countingCypherRunner counts grouped attempts so the live proof can show the
// retry seam made exactly one attempt.
type countingCypherRunner struct {
	inner  neo4jSessionRunner
	groups int
}

func (r *countingCypherRunner) RunCypher(ctx context.Context, cypher string, params map[string]any) error {
	return r.inner.RunCypher(ctx, cypher, params)
}

func (r *countingCypherRunner) RunCypherGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	r.groups++
	return r.inner.RunCypherGroup(ctx, stmts)
}
