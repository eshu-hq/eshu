// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestRetryingExecutorExecuteGroupDoesNotRetryDriverBudgetExhaustion(t *testing.T) {
	t.Parallel()

	driverErr := &neo4jdriver.TransactionExecutionLimit{
		Cause: "timeout (exceeded max retry time: 30s)",
		Errors: []error{&neo4jdriver.Neo4jError{
			Code: "Neo.TransientError.Transaction.DeadlockDetected",
			Msg:  "deadlock cycle",
		}},
	}
	inner := &driverBudgetExhaustedGroupExecutor{err: driverErr}
	retrying := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 3,
		BaseDelay:  time.Millisecond,
	}
	statements := []Statement{{
		Operation: OperationCanonicalUpsert,
		Cypher:    "UNWIND $rows AS row MERGE (r:Repository {id: row.id})",
	}}

	err := retrying.ExecuteGroup(context.Background(), statements)
	if err == nil {
		t.Fatal("ExecuteGroup() error = nil, want exhausted driver budget")
	}
	if got, want := inner.calls.Load(), int32(1); got != want {
		t.Fatalf("group attempts = %d, want %d; outer retry repeated the exhausted driver budget", got, want)
	}
	var gotDriverErr *neo4jdriver.TransactionExecutionLimit
	if !errors.As(err, &gotDriverErr) {
		t.Fatalf("ExecuteGroup() error = %T %v, want TransactionExecutionLimit preserved", err, err)
	}
}

func TestRetryingExecutorExecuteGroupDoesNotRetryCommitFailedDeadConnectivityError(t *testing.T) {
	t.Parallel()

	// CommitFailedDeadError is intentionally private to the Neo4j driver. Its
	// public error contract is a ConnectivityError whose inner text starts with
	// "Connection lost during commit:". The driver itself classifies that
	// specific connectivity wrapper as non-retryable because the transaction's
	// commit outcome is unknown; replaying it can double-apply non-idempotent
	// effects.
	driverErr := &neo4jdriver.ConnectivityError{
		Inner: errors.New("Connection lost during commit: EOF"),
	}
	inner := &driverBudgetExhaustedGroupExecutor{err: driverErr}
	retrying := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 3,
		BaseDelay:  time.Millisecond,
	}
	statements := []Statement{{
		Operation: OperationCanonicalUpsert,
		Cypher:    "UNWIND $rows AS row MERGE (r:Repository {id: row.id})",
	}}

	err := retrying.ExecuteGroup(context.Background(), statements)
	if err == nil {
		t.Fatal("ExecuteGroup() error = nil, want unknown commit outcome")
	}
	if got, want := inner.calls.Load(), int32(1); got != want {
		t.Fatalf("group attempts = %d, want %d; outer retry replayed an unknown commit outcome", got, want)
	}
	var gotConnectivityErr *neo4jdriver.ConnectivityError
	if !errors.As(err, &gotConnectivityErr) {
		t.Fatalf("ExecuteGroup() error = %T %v, want ConnectivityError preserved", err, err)
	}
}

func TestRetryingExecutorExecuteGroupDoesNotRetryConnectivityErrorWithoutCause(t *testing.T) {
	t.Parallel()

	inner := &driverBudgetExhaustedGroupExecutor{
		err: &neo4jdriver.ConnectivityError{},
	}
	retrying := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 3,
		BaseDelay:  time.Millisecond,
	}
	statements := []Statement{{
		Operation: OperationCanonicalUpsert,
		Cypher:    "UNWIND $rows AS row MERGE (r:Repository {id: row.id})",
	}}

	err := retrying.ExecuteGroup(context.Background(), statements)
	if err == nil {
		t.Fatal("ExecuteGroup() error = nil, want malformed connectivity error")
	}
	if got, want := inner.calls.Load(), int32(1); got != want {
		t.Fatalf("group attempts = %d, want %d; malformed connectivity error must fail closed", got, want)
	}
	if got, want := err.Error(), malformedNeo4jConnectivityErrorMessage; got != want {
		t.Fatalf("ExecuteGroup() error = %q, want safe terminal error %q", got, want)
	}
	if reducer.IsRetryable(err) {
		t.Fatal("ExecuteGroup() malformed connectivity error is retryable, want terminal error")
	}
}

func TestRetryingExecutorRetryMetricUsesBoundedReason(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("retry-reason-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	inner := &failingGroupExecutor{
		failFor: 1,
		errMsg: "Neo4jError: Neo.TransientError.Transaction.DeadlockDetected " +
			"(repository=high-cardinality-repo-123, node=high-cardinality-node-456)",
	}
	retrying := &RetryingExecutor{
		Inner:       inner,
		MaxRetries:  1,
		BaseDelay:   time.Millisecond,
		Instruments: instruments,
	}
	statements := []Statement{{
		Operation: OperationCanonicalUpsert,
		Cypher:    "UNWIND $rows AS row MERGE (r:Repository {id: row.id})",
	}}

	if err := retrying.ExecuteGroup(context.Background(), statements); err != nil {
		t.Fatalf("ExecuteGroup() error = %v, want nil after retry", err)
	}

	attrs := retryCounterAttributes(t, reader)
	if got, want := attrs[telemetry.MetricDimensionReason], "transient_error"; got != want {
		t.Fatalf("retry reason = %q, want bounded %q", got, want)
	}
	if got, want := attrs[telemetry.MetricDimensionWritePhase], string(OperationCanonicalUpsert); got != want {
		t.Fatalf("write phase = %q, want %q", got, want)
	}
	for key, value := range attrs {
		if strings.Contains(value, "high-cardinality-repo-123") ||
			strings.Contains(value, "high-cardinality-node-456") {
			t.Fatalf("metric attribute %q leaked high-cardinality error data %q", key, value)
		}
	}
}

func retryCounterAttributes(t *testing.T, reader *metric.ManualReader) map[string]string {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("reader.Collect() error = %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_neo4j_deadlock_retries_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok || len(sum.DataPoints) != 1 {
				t.Fatalf("retry metric data = %T with points, want one int64 sum point", m.Data)
			}
			attrs := make(map[string]string)
			for _, attr := range sum.DataPoints[0].Attributes.ToSlice() {
				attrs[string(attr.Key)] = attr.Value.AsString()
			}
			return attrs
		}
	}
	t.Fatal("eshu_dp_neo4j_deadlock_retries_total not recorded")
	return nil
}

type driverBudgetExhaustedGroupExecutor struct {
	calls atomic.Int32
	err   error
}

func (e *driverBudgetExhaustedGroupExecutor) Execute(context.Context, Statement) error {
	return nil
}

func (e *driverBudgetExhaustedGroupExecutor) ExecuteGroup(context.Context, []Statement) error {
	e.calls.Add(1)
	return e.err
}
