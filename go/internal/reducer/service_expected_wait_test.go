// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const testExpectedWaitClass = "test_expected_wait"

// expectedWaitTestError is a retryable error that declares itself an expected
// wait, the same shape security_alert_reconciliation's pending-impact defer has.
type expectedWaitTestError struct{}

func (expectedWaitTestError) Error() string        { return "waiting for upstream evidence" }
func (expectedWaitTestError) Retryable() bool      { return true }
func (expectedWaitTestError) ExpectedWait() bool   { return true }
func (expectedWaitTestError) FailureClass() string { return testExpectedWaitClass }

// recordExpectedWaitAttempt records one failed execution at the given attempt
// number and returns the log line and the executions counter points.
func recordExpectedWaitAttempt(t *testing.T, maxAttempts, attempt int, execErr error) recordedExecution {
	t.Helper()
	var logs bytes.Buffer
	reader := metric.NewManualReader()
	instruments, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("reducer-expected-wait"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	service := Service{
		Logger:           slog.New(slog.NewJSONHandler(&logs, nil)),
		Instruments:      instruments,
		RetryMaxAttempts: maxAttempts,
	}
	intent := reducercontract.Intent{
		IntentID: "intent-wait", Domain: DomainSecurityAlertReconciliation,
		EntityKeys: []string{"security-alert:github:acme/api"}, AttemptCount: attempt,
	}
	service.recordReducerResult(context.Background(), intent, reducercontract.Result{}, 0.1, 0.2, "failed", 1, execErr)

	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &line); err != nil {
		t.Fatalf("decode log line %q: %v", logs.String(), err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	attrs := func(status string) map[string]string {
		return map[string]string{"queue": "reducer", "status": status, "domain": string(intent.Domain)}
	}
	got := recordedExecution{
		deferred: executionCount(rm, attrs("deferred")),
		failed:   executionCount(rm, attrs("failed")),
	}
	got.level, _ = line["level"].(string)
	got.msg, _ = line["msg"].(string)
	got.failureClass, _ = line["failure_class"].(string)
	got.status, _ = line["status"].(string)
	return got
}

func TestRecordReducerResultLogsExpectedWaitBelowErrorWithoutSpendingFailed(t *testing.T) {
	t.Parallel()
	// Attempts 1 and 2 of a 3-attempt budget are still retried by the queue.
	for _, attempt := range []int{1, 2} {
		got := recordExpectedWaitAttempt(t, 3, attempt, expectedWaitTestError{})
		if got.level != "WARN" || got.msg != "reducer execution deferred" {
			t.Fatalf("attempt %d: log = %s %q, want WARN \"reducer execution deferred\"", attempt, got.level, got.msg)
		}
		if got.status != "deferred" || got.failureClass != testExpectedWaitClass {
			t.Fatalf("attempt %d: status=%q failure_class=%q, want deferred/%s", attempt, got.status, got.failureClass, testExpectedWaitClass)
		}
		if got.deferred != 1 || got.failed != 0 {
			t.Fatalf("attempt %d: executions deferred=%d failed=%d, want 1/0", attempt, got.deferred, got.failed)
		}
	}
}

// The queue dead-letters a counting retry on its final attempt, so that attempt
// is a real failure and must stay failed/ERROR (the 3 AM "worry" signal).
func TestRecordReducerResultKeepsExhaustedExpectedWaitAsFailed(t *testing.T) {
	t.Parallel()
	got := recordExpectedWaitAttempt(t, 3, 3, expectedWaitTestError{})
	if got.level != "ERROR" || got.msg != "reducer execution failed" {
		t.Fatalf("log = %s %q, want ERROR \"reducer execution failed\"", got.level, got.msg)
	}
	if got.status != "failed" || got.deferred != 0 || got.failed != 1 {
		t.Fatalf("status=%q deferred=%d failed=%d, want failed 0/1", got.status, got.deferred, got.failed)
	}
}

// A zero RetryMaxAttempts mirrors the queue default of 3.
func TestRecordReducerResultExpectedWaitUsesQueueDefaultBudget(t *testing.T) {
	t.Parallel()
	if got := recordExpectedWaitAttempt(t, 0, 2, expectedWaitTestError{}); got.status != "deferred" {
		t.Fatalf("attempt 2 of default budget: status=%q, want deferred", got.status)
	}
	if got := recordExpectedWaitAttempt(t, 0, 3, expectedWaitTestError{}); got.status != "failed" {
		t.Fatalf("attempt 3 of default budget: status=%q, want failed", got.status)
	}
}

// Real handler errors, retryable or not, stay failed at ERROR.
func TestRecordReducerResultKeepsRealErrorsFailed(t *testing.T) {
	t.Parallel()
	retryableOnly := classifiedReducerExecutionError{message: "graph write timed out", failureClass: "graph_write_timeout"}
	for name, execErr := range map[string]error{
		"plain error":                   errors.New("decode facts: boom"),
		"retryable without expectation": retryableNotWaitError{retryableOnly},
	} {
		got := recordExpectedWaitAttempt(t, 3, 1, execErr)
		if got.level != "ERROR" || got.msg != "reducer execution failed" {
			t.Fatalf("%s: log = %s %q, want ERROR \"reducer execution failed\"", name, got.level, got.msg)
		}
		if got.status != "failed" || got.deferred != 0 || got.failed != 1 {
			t.Fatalf("%s: status=%q deferred=%d failed=%d, want failed 0/1", name, got.status, got.deferred, got.failed)
		}
	}
}

// retryableNotWaitError is retryable but does not declare an expected wait.
type retryableNotWaitError struct {
	classifiedReducerExecutionError
}

func (retryableNotWaitError) Retryable() bool { return true }
