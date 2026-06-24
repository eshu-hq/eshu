// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestClaimedServiceFailsTerminalWhenAttemptBudgetExhausted is the safety net
// for issue #612: a retryable collector failure that recurs for the same work
// item must not loop forever on workflow_claims.failed_retryable. Once the
// item's prior AttemptCount has reached MaxAttempts, the service routes the
// failure through FailClaimTerminal with class "attempt_budget_exhausted"
// instead of re-enqueueing the work for another stale retry.
func TestClaimedServiceFailsTerminalWhenAttemptBudgetExhausted(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.AttemptCount = 5
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		terminalFail: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	source := &stubClaimedSource{err: errors.New("still throttled")}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})
	service.MaxAttempts = 3

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil after attempt budget routes to terminal", err)
	}
	if got, want := store.terminalFailCalls, 1; got != want {
		t.Fatalf("terminal fail calls = %d, want %d", got, want)
	}
	if got, want := store.retryableFailCalls, 0; got != want {
		t.Fatalf("retryable fail calls = %d, want %d", got, want)
	}
	if got, want := store.lastTerminalFail.FailureClass, "attempt_budget_exhausted"; got != want {
		t.Fatalf("FailureClass = %q, want %q", got, want)
	}
	if got := store.lastTerminalFail.FailureMessage; !strings.Contains(got, "attempt 5") || !strings.Contains(got, "budget 3") || !strings.Contains(got, "exhausted") {
		t.Fatalf("FailureMessage = %q, want attempt/budget/exhausted detail", got)
	}
}

// TestClaimedServiceRetriesWithinAttemptBudget proves the budget guard does
// not regress the retryable path for in-budget transient failures. The
// service still calls FailClaimRetryable so transient collector failures
// keep their existing recovery shape.
func TestClaimedServiceRetriesWithinAttemptBudget(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.May, 25, 12, 30, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.AttemptCount = 2
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		retryableFail: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	source := &stubClaimedSource{err: errors.New("temporary throttle")}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})
	service.MaxAttempts = 5

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil after retryable path", err)
	}
	if got, want := store.retryableFailCalls, 1; got != want {
		t.Fatalf("retryable fail calls = %d, want %d", got, want)
	}
	if got, want := store.terminalFailCalls, 0; got != want {
		t.Fatalf("terminal fail calls = %d, want %d", got, want)
	}
	if got, want := store.lastRetryableFail.FailureClass, "collect_failure"; got != want {
		t.Fatalf("FailureClass = %q, want %q", got, want)
	}
}

// TestClaimedServiceAttemptBudgetExhaustionIncrementsCounter proves the
// runtime publishes
// eshu_dp_workflow_claim_attempt_budget_exhausted_total when the bounded
// retry guard escalates a claim. An operator paged at 3 AM uses this counter
// to attribute the runaway-loop block in issue #612 to the right
// (collector_kind, source_system) pair.
func TestClaimedServiceAttemptBudgetExhaustionIncrementsCounter(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.May, 25, 14, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.AttemptCount = 4
	item.SourceSystem = "aws"
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		terminalFail: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() err = %v", err)
	}

	source := &stubClaimedSource{err: errors.New("still throttled")}
	service := testClaimedService(now, claim, scope.CollectorAWS, store, source, &stubClaimedCommitter{})
	service.MaxAttempts = 3
	service.Instruments = instruments

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() err = %v", err)
	}
	got := claimMetricValue(t, rm, "eshu_dp_workflow_claim_attempt_budget_exhausted_total", map[string]string{
		telemetry.MetricDimensionCollectorKind: string(scope.CollectorAWS),
		telemetry.MetricDimensionSourceSystem:  "aws",
	})
	if got != 1 {
		t.Fatalf("attempt budget exhausted counter = %d, want 1", got)
	}
}

func claimMetricValue(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
	wantAttrs map[string]string,
) int64 {
	t.Helper()
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, record := range scopeMetrics.Metrics {
			if record.Name != metricName {
				continue
			}
			sum, ok := record.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Sum[int64]", metricName, record.Data)
			}
			for _, dp := range sum.DataPoints {
				if claimMetricAttrsMatch(dp.Attributes.ToSlice(), wantAttrs) {
					return dp.Value
				}
			}
		}
	}
	t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
	return 0
}

func claimMetricAttrsMatch(actual []attribute.KeyValue, want map[string]string) bool {
	if len(actual) != len(want) {
		return false
	}
	for _, attr := range actual {
		if want[string(attr.Key)] != attr.Value.AsString() {
			return false
		}
	}
	return true
}

// TestClaimedServiceAttemptBudgetIgnoredWhenUnset confirms the default
// behavior is unchanged when MaxAttempts is zero: any retryable error keeps
// going through FailClaimRetryable. This preserves backwards compatibility
// for collectors that haven't yet been wired to a bounded retry policy.
func TestClaimedServiceAttemptBudgetIgnoredWhenUnset(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.May, 25, 13, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.AttemptCount = 999
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		retryableFail: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	source := &stubClaimedSource{err: errors.New("still throttled")}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := store.retryableFailCalls, 1; got != want {
		t.Fatalf("retryable fail calls = %d, want %d", got, want)
	}
	if got, want := store.terminalFailCalls, 0; got != want {
		t.Fatalf("terminal fail calls = %d, want %d", got, want)
	}
}
