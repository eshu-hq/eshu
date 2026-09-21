// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel/sdk/metric"
)

// TestRetryingExecutorRetriesShortFormPlatformCommitUniqueConflict is the
// #6922 regression: the differential oracle captured a concurrent Platform
// MERGE loser as SyntaxError + "commit failed: constraint violation: UNIQUE
// on Platform.[id]" with no "already exists" tail. The classifier must retry
// it on MERGE-shaped Cypher because re-execution converges on the winner.
// Contention mechanics (two writers, one create, one conflict, both green)
// are proven by
// TestRetryingExecutorConvergesConcurrentTypedPlatformCommitUniqueConflict;
// this test pins the short-form classification plus the retry reason the
// operator sees on the contended Platform conflict domain.
func TestRetryingExecutorRetriesShortFormPlatformCommitUniqueConflict(t *testing.T) {
	t.Parallel()

	inner := &typedPlatformConflictExecutor{err: &neo4jdriver.Neo4jError{
		Code: nornicDBStatementSyntaxErrorCode,
		Msg:  "commit failed: constraint violation: UNIQUE on Platform.[id]",
	}}
	reader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	retrying := &RetryingExecutor{
		Inner:       inner,
		MaxRetries:  1,
		BaseDelay:   time.Millisecond,
		Instruments: instruments,
	}

	err = retrying.Execute(context.Background(), Statement{
		Operation: OperationCanonicalUpsert,
		Cypher: `UNWIND $rows AS row
MERGE (p:Platform {id: row.platform_id})
SET p.name = row.platform_name`,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil after retry", err)
	}
	if got, want := inner.calls.Load(), int32(2); got != want {
		t.Fatalf("Execute() calls = %d, want %d", got, want)
	}
	if got := collectRetryCounter(t, reader); got != 1 {
		t.Fatalf("Neo4jDeadlockRetries counter = %d, want 1", got)
	}
	attrs := retryCounterAttributes(t, reader)
	if got, want := attrs[telemetry.MetricDimensionReason], graphWriteRetryReasonUniqueConflict; got != want {
		t.Fatalf("retry reason = %q, want %q", got, want)
	}
}
