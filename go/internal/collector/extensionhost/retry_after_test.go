// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const providerThrottleMetric = "eshu_dp_workflow_claim_provider_throttle_total"

// retryableResultWithDelays builds a retryable SDK result whose failure
// statuses carry the given retry_after_seconds values in order.
func retryableResultWithDelays(item workflow.WorkItem, delays ...int) sdkcollector.Result {
	result := baseResult(item, sdkcollector.ResultRetryable)
	for _, seconds := range delays {
		result.Statuses = append(result.Statuses, sdkcollector.Status{
			Class:             sdkcollector.StatusFailure,
			FailureClass:      "rate_limited",
			RetryAfterSeconds: seconds,
		})
	}
	return result
}

// TestNextClaimedRetryableCarriesLargestRetryAfter proves the classified
// failure for a retryable SDK result exposes the largest retry_after_seconds
// across its statuses through the RetryAfterDelay accessor ClaimedService
// reads, not the first or last value.
func TestNextClaimedRetryableCarriesLargestRetryAfter(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	source := mustNewSource(t, &recordingRunner{result: retryableResultWithDelays(item, 30, 120, 45)}, nil)

	_, _, err := source.NextClaimed(context.Background(), item)
	assertFailure(t, err, "rate_limited", false)
	var delayed interface{ RetryAfterDelay() time.Duration }
	if !errors.As(err, &delayed) {
		t.Fatalf("error %v does not expose RetryAfterDelay", err)
	}
	if got, want := delayed.RetryAfterDelay(), 120*time.Second; got != want {
		t.Fatalf("RetryAfterDelay() = %s, want %s", got, want)
	}
}

// TestClaimedServiceHonorsExtensionRetryAfter drives a retryable result with
// retry_after_seconds=120 through the real ClaimedService under a fixed fake
// clock: the work item must not become visible again for at least 120 seconds,
// and the provider-throttle counter must record the honored Retry-After.
func TestClaimedServiceHonorsExtensionRetryAfter(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	// Live grant: the service re-stamps the dispatch deadline from its
	// heartbeat, so the fixture must carry the granted lease for the
	// canned satellite echo to validate (eshu-hq/eshu#9205).
	item.LeaseExpiresAt = testObservedAt().Add(time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &serviceClaimStore{item: item, claim: testWorkflowClaim(item), cancel: cancel}
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"),
	)
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	source := mustNewSource(t, &recordingRunner{result: retryableResultWithDelays(item, 30, 120, 45)}, nil)
	service := collector.ClaimedService{
		ControlStore:        store,
		Source:              source,
		Committer:           &serviceCommitter{},
		CollectorKind:       item.CollectorKind,
		CollectorInstanceID: item.CollectorInstanceID,
		OwnerID:             item.CurrentOwnerID,
		ClaimIDFunc:         func() string { return item.CurrentClaimID },
		PollInterval:        time.Millisecond,
		ClaimLeaseTTL:       time.Minute,
		HeartbeatInterval:   30 * time.Second,
		Clock:               testObservedAt,
		Instruments:         instruments,
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := store.retryableCalls; got != 1 {
		t.Fatalf("FailClaimRetryable calls = %d, want 1", got)
	}
	if got, want := store.retryVisibleAt.Sub(testObservedAt()), 120*time.Second; got < want {
		t.Fatalf("retry visible after %s, want at least %s", got, want)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if got := sumWithAttr(rm, providerThrottleMetric, "outcome", "retry_after_honored"); got != 1 {
		t.Fatalf("%s{outcome=retry_after_honored} = %d, want 1", providerThrottleMetric, got)
	}
}

// sumWithAttr sums an Int64 counter's points whose attribute key equals value.
func sumWithAttr(rm metricdata.ResourceMetrics, name, key, value string) int64 {
	var total int64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, record := range scopeMetrics.Metrics {
			if record.Name != name {
				continue
			}
			sum, ok := record.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				if got, ok := point.Attributes.Value(attribute.Key(key)); ok && got.AsString() == value {
					total += point.Value
				}
			}
		}
	}
	return total
}

// TestNextClaimedRetryAfterIsClamped proves an oversized retry_after_seconds
// cannot hide a work item beyond MaxRetryAfterDelay or overflow
// time.Duration, while a value at the cap is kept exactly.
func TestNextClaimedRetryAfterIsClamped(t *testing.T) {
	t.Parallel()

	capSeconds := int(MaxRetryAfterDelay / time.Second)
	tests := []struct {
		name    string
		seconds int
		want    time.Duration
	}{
		{name: "at cap", seconds: capSeconds, want: MaxRetryAfterDelay},
		{name: "one over cap", seconds: capSeconds + 1, want: MaxRetryAfterDelay},
		{name: "would overflow duration", seconds: math.MaxInt, want: MaxRetryAfterDelay},
		{name: "one second", seconds: 1, want: time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			item := testWorkItem()
			source := mustNewSource(t, &recordingRunner{result: retryableResultWithDelays(item, tc.seconds)}, nil)
			_, _, err := source.NextClaimed(context.Background(), item)
			var delayed interface{ RetryAfterDelay() time.Duration }
			if !errors.As(err, &delayed) {
				t.Fatalf("error %v does not expose RetryAfterDelay", err)
			}
			if got := delayed.RetryAfterDelay(); got != tc.want {
				t.Fatalf("RetryAfterDelay() = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestNextClaimedTerminalCarriesNoRetryAfter proves only a retryable result
// requests pacing: a terminal result never schedules a retry.
func TestNextClaimedTerminalCarriesNoRetryAfter(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	result := baseResult(item, sdkcollector.ResultTerminal)
	result.Statuses = []sdkcollector.Status{{
		Class:             sdkcollector.StatusFailure,
		FailureClass:      "invalid_config",
		RetryAfterSeconds: 90,
	}}
	source := mustNewSource(t, &recordingRunner{result: result}, nil)
	_, _, err := source.NextClaimed(context.Background(), item)
	assertFailure(t, err, "invalid_config", true)
	var delayed interface{ RetryAfterDelay() time.Duration }
	if errors.As(err, &delayed) && delayed.RetryAfterDelay() != 0 {
		t.Fatalf("terminal RetryAfterDelay() = %s, want 0", delayed.RetryAfterDelay())
	}
}

// TestRetryAfterFromStatusesIgnoresZeroAndNegative proves a zero or negative
// retry_after_seconds never yields a delay or wins the maximum, so pacing
// falls back to the poll interval. The SDK validator already rejects a
// negative value; this guards the helper on its own.
func TestRetryAfterFromStatusesIgnoresZeroAndNegative(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	tests := []struct {
		name   string
		delays []int
		want   time.Duration
	}{
		{name: "no statuses", want: 0},
		{name: "zero", delays: []int{0}, want: 0},
		{name: "negative", delays: []int{-5}, want: 0},
		{name: "negative and positive", delays: []int{-5, 7, 0}, want: 7 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := retryAfterFromStatuses(retryableResultWithDelays(item, tc.delays...)); got != tc.want {
				t.Fatalf("retryAfterFromStatuses(%v) = %s, want %s", tc.delays, got, tc.want)
			}
		})
	}
}

// TestNextClaimedNegativeRetryAfterIsInvalidResult proves a negative
// retry_after_seconds fails the result closed as a terminal invalid result
// instead of scheduling a retry.
func TestNextClaimedNegativeRetryAfterIsInvalidResult(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	source := mustNewSource(t, &recordingRunner{result: retryableResultWithDelays(item, -1)}, nil)
	_, _, err := source.NextClaimed(context.Background(), item)
	assertFailure(t, err, FailureClassInvalidResult, true)
}
