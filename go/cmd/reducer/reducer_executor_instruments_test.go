// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestReducerNeo4jExecutorRecordsDeadlockRetryCounter proves the #5048
// Instruments wiring end to end through the reducer's constructor: a
// reducerNeo4jExecutor built with non-nil Instruments increments
// eshu_dp_neo4j_deadlock_retries_total when its persistent RetryingExecutor
// retries a transient graph-write error. The deleted per-call
// executeReducerCypherWithRetry constructed its RetryingExecutor WITHOUT
// Instruments, so the counter was silently never recorded for reducer graph
// writes; this guards against a regression that drops Instruments from
// newReducerNeo4jExecutor / newReducerCypherExecutor again.
func TestReducerNeo4jExecutorRecordsDeadlockRetryCounter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}

	// Attempt 1 returns a retryable transient error; attempt 2 (errs drained)
	// succeeds, so the persistent RetryingExecutor performs exactly one retry.
	session := &fakeNeo4jSession{errs: []error{
		errors.New("Neo4jError: Neo.TransientError.Transaction.DeadlockDetected (deadlock cycle)"),
	}}
	exec := newReducerNeo4jExecutor(session, inst)

	if err := exec.Execute(ctx, sourcecypher.Statement{Cypher: "MERGE (a) RETURN a"}); err != nil {
		t.Fatalf("Execute() = %v, want nil (transient retried to success)", err)
	}
	if got, want := len(session.calls), 2; got != want {
		t.Fatalf("session calls = %d, want %d (attempt 1 transient, attempt 2 success)", got, want)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := deadlockRetryCounterValue(t, rm); got < 1 {
		t.Fatalf("eshu_dp_neo4j_deadlock_retries_total = %d, want >= 1 "+
			"(Instruments must round-trip through the reducer's persistent RetryingExecutor)", got)
	}
}

// deadlockRetryCounterValue sums the eshu_dp_neo4j_deadlock_retries_total counter
// across all attribute sets in rm.
func deadlockRetryCounterValue(t *testing.T, rm metricdata.ResourceMetrics) int64 {
	t.Helper()
	const name = "eshu_dp_neo4j_deadlock_retries_total"
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s is %T, want metricdata.Sum[int64]", name, m.Data)
			}
			var total int64
			for _, point := range sum.DataPoints {
				total += point.Value
			}
			return total
		}
	}
	t.Fatalf("metric %s not found in collected metrics", name)
	return 0
}
