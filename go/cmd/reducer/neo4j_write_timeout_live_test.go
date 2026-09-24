// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestLiveNeo4jCanonicalWriteTimeoutAbortsBlockedWrite proves on a real Neo4j
// server that ESHU_CANONICAL_WRITE_TIMEOUT now bounds reducer graph writes,
// and that a write terminated while it waits on a lock requeues whatever
// terminated it. Each case is driven through newReducerNeo4jExecutor, the
// production retry seam:
//
//   - lock wait: with the reducerTransactionTimeout budget, a holder
//     transaction keeps the write lock on the probe node, so the contender
//     blocks until the server terminates it. Neo4j reports
//     Neo.ClientError.Transaction.LockClientStopped; the write requeues.
//   - long statement: with the same budget, the contender runs a CPU-bound
//     statement that outlives the timeout. Neo4j reports
//     Neo.ClientError.Transaction.TransactionTimedOutClientConfiguration,
//     which stays terminal outside the replay-safe RUNS_ON groups.
//   - operator kill: with no transaction timeout, the contender blocks on
//     the holder's lock until TERMINATE TRANSACTIONS stops it. Neo4j reports
//     the same LockClientStopped status, and the write must requeue rather
//     than dead-letter.
//
// Every case must make exactly one attempt: a terminated write must not be
// replayed locally while its lease is held.
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

	const lockClientStopped = "Neo.ClientError.Transaction.LockClientStopped"
	tests := []struct {
		name          string
		holdLock      bool
		terminate     bool
		txTimeout     time.Duration
		cypher        string
		wantCode      string
		wantRetryable bool
	}{
		{
			name:          "lock wait",
			holdLock:      true,
			txTimeout:     txTimeout,
			cypher:        `MATCH (n:Neo4jWriteTimeoutProbe {probe: $probe}) SET n.value = 'contender'`,
			wantCode:      lockClientStopped,
			wantRetryable: true,
		},
		{
			name:      "long statement",
			txTimeout: txTimeout,
			cypher: `MATCH (n:Neo4jWriteTimeoutProbe {probe: $probe})
UNWIND range(1, 2000000000) AS x
WITH n, sum(x) AS total
SET n.value = 'contender-' + toString(total)`,
			wantCode: "Neo.ClientError.Transaction.TransactionTimedOutClientConfiguration",
		},
		{
			name:          "operator kill",
			holdLock:      true,
			terminate:     true,
			cypher:        `MATCH (n:Neo4jWriteTimeoutProbe {probe: $probe}) SET n.value = 'killed-contender'`,
			wantCode:      lockClientStopped,
			wantRetryable: true,
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

			if tt.holdLock {
				holderSession := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite})
				defer func() { _ = holderSession.Close(context.Background()) }()
				holder, err := holderSession.BeginTransaction(ctx)
				if err != nil {
					t.Fatalf("begin holder transaction: %v", err)
				}
				if _, err := holder.Run(ctx, `MATCH (n:Neo4jWriteTimeoutProbe {probe: $probe}) SET n.value = 'holder'`,
					map[string]any{"probe": probe}); err != nil {
					t.Fatalf("holder takes write lock: %v", err)
				}
			}

			runner := &countingCypherRunner{inner: neo4jSessionRunner{Driver: driver, TxTimeout: tt.txTimeout}}
			executor := newReducerNeo4jExecutor(runner, nil)
			start := time.Now()
			done := make(chan error, 1)
			go func() {
				done <- executor.ExecuteGroup(ctx, []sourcecypher.Statement{{
					Operation:  sourcecypher.OperationCanonicalUpsert,
					Cypher:     tt.cypher,
					Parameters: map[string]any{"probe": probe},
				}})
			}()
			if tt.terminate {
				terminateBlockedContender(ctx, t, driver, "killed-contender")
			}
			writeErr := <-done
			elapsed := time.Since(start)
			t.Logf("contender returned after %s (attempts=%d): %v", elapsed, runner.groups.Load(), writeErr)

			var neo4jErr *neo4jdriver.Neo4jError
			if !errors.As(writeErr, &neo4jErr) {
				t.Fatalf("contender error = %v, want typed Neo4j termination", writeErr)
			}
			if neo4jErr.Code != tt.wantCode {
				t.Fatalf("contender error code = %q, want %q", neo4jErr.Code, tt.wantCode)
			}
			if tt.txTimeout > 0 && (elapsed < configured || elapsed > configured+10*time.Second) {
				t.Fatalf("contender aborted after %s, want about %s", elapsed, configured)
			}
			if attempts := runner.groups.Load(); attempts != 1 {
				t.Fatalf("group attempts = %d, want 1: a terminated write must not retry locally", attempts)
			}
			if got := reducer.IsRetryable(writeErr); got != tt.wantRetryable {
				t.Fatalf("reducer.IsRetryable(%v) = %t, want %t", writeErr, got, tt.wantRetryable)
			}
		})
	}
}

// terminateBlockedContender waits for the contender transaction whose query
// contains marker to appear in SHOW TRANSACTIONS, then terminates it the way
// an operator would.
func terminateBlockedContender(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext, marker string) {
	t.Helper()
	const show = `SHOW TRANSACTIONS YIELD transactionId, currentQuery
WHERE currentQuery CONTAINS $marker AND NOT currentQuery CONTAINS 'SHOW TRANSACTIONS'
RETURN transactionId`
	for ctx.Err() == nil {
		result, err := neo4jdriver.ExecuteQuery(ctx, driver, show, map[string]any{"marker": marker},
			neo4jdriver.EagerResultTransformer)
		if err != nil {
			t.Fatalf("show transactions: %v", err)
		}
		if len(result.Records) == 1 {
			id, _ := result.Records[0].Get("transactionId")
			if _, err := neo4jdriver.ExecuteQuery(ctx, driver, `TERMINATE TRANSACTIONS $id`,
				map[string]any{"id": id}, neo4jdriver.EagerResultTransformer); err != nil {
				t.Fatalf("terminate contender %v: %v", id, err)
			}
			t.Logf("terminated contender transaction %v", id)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("contender transaction never appeared: %v", ctx.Err())
}

// countingCypherRunner counts grouped attempts so the live proof can show the
// retry seam made exactly one attempt.
type countingCypherRunner struct {
	inner  neo4jSessionRunner
	groups atomic.Int32
}

func (r *countingCypherRunner) RunCypher(ctx context.Context, cypher string, params map[string]any) error {
	return r.inner.RunCypher(ctx, cypher, params)
}

func (r *countingCypherRunner) RunCypherGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	r.groups.Add(1)
	return r.inner.RunCypherGroup(ctx, stmts)
}
