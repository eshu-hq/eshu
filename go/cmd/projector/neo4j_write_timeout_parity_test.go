// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestProjectorCanonicalExecutorBoundsNeo4jWritesLikeNornicDB pins retry
// parity for a timed-out projector canonical write: on both backends a
// non-RUNS_ON group that outlives ESHU_CANONICAL_WRITE_TIMEOUT must end as a
// retryable graph_write_timeout.
func TestProjectorCanonicalExecutorBoundsNeo4jWritesLikeNornicDB(t *testing.T) {
	t.Parallel()

	for _, backend := range []runtimecfg.GraphBackend{runtimecfg.GraphBackendNeo4j, runtimecfg.GraphBackendNornicDB} {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			getenv := timeoutGetenv("20ms")
			executor := projectorCanonicalExecutorForGraphBackend(
				blockingNeo4jTimeoutExecutor{}, backend, projectorNornicDBConfigForTest(t, getenv), getenv, nil, nil, nil,
			)
			requireRetryableGraphWriteTimeout(t, runBoundedCanonicalGroup(t, executor))
		})
	}
}

// TestProjectorCanonicalExecutorLeavesUnboundedNeo4jUnwrapped pins that an
// unset Neo4j timeout keeps today's executor chain.
func TestProjectorCanonicalExecutorLeavesUnboundedNeo4jUnwrapped(t *testing.T) {
	t.Parallel()

	getenv := timeoutGetenv("")
	executor := projectorCanonicalExecutorForGraphBackend(
		blockingNeo4jTimeoutExecutor{}, runtimecfg.GraphBackendNeo4j, projectorNornicDBConfigForTest(t, getenv), getenv, nil, nil, nil,
	)
	if _, ok := executor.(*sourcecypher.InstrumentedExecutor); !ok {
		t.Fatalf("executor = %T, want the unwrapped *sourcecypher.InstrumentedExecutor", executor)
	}
}

// blockingNeo4jTimeoutExecutor blocks every write until its context ends, the
// way a Neo4j write that waits on another transaction's lock does.
type blockingNeo4jTimeoutExecutor struct{}

func (blockingNeo4jTimeoutExecutor) Execute(ctx context.Context, _ sourcecypher.Statement) error {
	<-ctx.Done()
	return ctx.Err()
}

func (blockingNeo4jTimeoutExecutor) ExecuteGroup(ctx context.Context, _ []sourcecypher.Statement) error {
	<-ctx.Done()
	return ctx.Err()
}

// runBoundedCanonicalGroup drives a non-RUNS_ON canonical group through the
// widest write surface the executor exposes, as the canonical writer does.
func runBoundedCanonicalGroup(t *testing.T, executor sourcecypher.Executor) error {
	t.Helper()
	stmts := []sourcecypher.Statement{{
		Operation:  sourcecypher.OperationCanonicalUpsert,
		Cypher:     "UNWIND $rows AS row MERGE (f:File {path: row.path})",
		Parameters: map[string]any{"rows": []map[string]any{{"path": "a.go"}}},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if pge, ok := executor.(sourcecypher.PhaseGroupExecutor); ok {
		return pge.ExecutePhaseGroup(ctx, stmts)
	}
	ge, ok := executor.(sourcecypher.GroupExecutor)
	if !ok {
		t.Fatalf("executor %T exposes no grouped write surface", executor)
	}
	return ge.ExecuteGroup(ctx, stmts)
}

// requireRetryableGraphWriteTimeout asserts the queue-level outcome NornicDB
// already gets for a write timeout: a retryable graph_write_timeout, not a
// terminal failure.
func requireRetryableGraphWriteTimeout(t *testing.T, err error) {
	t.Helper()
	var timeoutErr sourcecypher.GraphWriteTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("error = %v (%T), want GraphWriteTimeoutError", err, err)
	}
	if !timeoutErr.Retryable() || timeoutErr.FailureClass() != sourcecypher.GraphWriteTimeoutFailureClass {
		t.Fatalf("error = %v, want retryable %s", err, sourcecypher.GraphWriteTimeoutFailureClass)
	}
}

// timeoutGetenv returns ESHU_CANONICAL_WRITE_TIMEOUT=raw and nothing else.
func timeoutGetenv(raw string) func(string) string {
	return func(key string) string {
		if key == "ESHU_CANONICAL_WRITE_TIMEOUT" {
			return raw
		}
		return ""
	}
}
