// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestResultRecordRetryAfterIsTheRequestNotTheAppliedDelay pins the operator
// contract for ResultRecord.RetryAfter and the telemetry built from it
// (#8908): it is the extension's retry request after the one-hour cap,
// recorded before ClaimedService decides what to do with it. ClaimedService
// applies max(PollInterval, request) while attempts remain, and none of it
// once the attempt budget is spent (the claim then fails terminally, keeps the
// extension's own failure class, and counts on the attempt-budget-exhausted
// counter). The record must carry the request in all three cases, and the
// durable outcome must be the one the docs describe.
func TestResultRecordRetryAfterIsTheRequestNotTheAppliedDelay(t *testing.T) {
	t.Parallel()

	const requested = 120 * time.Second
	tests := []struct {
		name         string
		pollInterval time.Duration
		maxAttempts  int
		attemptCount int
		wantRetry    int
		wantTerminal int
		wantClass    string
		wantVisible  time.Duration
		// wantBudget is the expected attempt-budget-exhausted counter value.
		wantBudget int64
	}{
		{
			name:         "poll interval above the request wins",
			pollInterval: 10 * time.Minute,
			wantRetry:    1,
			wantClass:    "rate_limited",
			wantVisible:  10 * time.Minute,
		},
		{
			name:         "request above the poll interval is applied",
			pollInterval: time.Millisecond,
			wantRetry:    1,
			wantClass:    "rate_limited",
			wantVisible:  requested,
		},
		{
			name:         "spent attempt budget applies no delay",
			pollInterval: time.Millisecond,
			maxAttempts:  3,
			attemptCount: 3,
			wantTerminal: 1,
			wantClass:    "rate_limited",
			wantBudget:   1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			item := testWorkItem()
			// Live grant: the service re-stamps the dispatch deadline from its
			// heartbeat, so the fixture must carry the granted lease for the
			// canned satellite echo to validate (eshu-hq/eshu#9205).
			item.LeaseExpiresAt = testObservedAt().Add(time.Minute)
			if tc.attemptCount > 0 {
				item.AttemptCount = tc.attemptCount
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &serviceClaimStore{item: item, claim: testWorkflowClaim(item), cancel: cancel}
			recorder := &recordingResultRecorder{}
			reader := sdkmetric.NewManualReader()
			instruments, err := telemetry.NewInstruments(
				sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"),
			)
			if err != nil {
				t.Fatalf("NewInstruments() error = %v, want nil", err)
			}
			source := mustNewSource(t, &recordingRunner{result: retryableResultWithDelays(item, int(requested/time.Second))}, recorder)
			service := collector.ClaimedService{
				ControlStore:        store,
				Source:              source,
				Committer:           &serviceCommitter{},
				CollectorKind:       item.CollectorKind,
				CollectorInstanceID: item.CollectorInstanceID,
				OwnerID:             item.CurrentOwnerID,
				ClaimIDFunc:         func() string { return item.CurrentClaimID },
				PollInterval:        tc.pollInterval,
				ClaimLeaseTTL:       time.Minute,
				HeartbeatInterval:   30 * time.Second,
				MaxAttempts:         tc.maxAttempts,
				Clock:               testObservedAt,
				Instruments:         instruments,
			}

			if err := service.Run(ctx); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			if len(recorder.results) != 1 {
				t.Fatalf("ResultRecords = %d, want 1", len(recorder.results))
			}
			got := recorder.results[0]
			if got.State != sdkcollector.ResultRetryable {
				t.Fatalf("recorded state = %q, want the extension's result state %q", got.State, sdkcollector.ResultRetryable)
			}
			if got.RetryAfter != requested {
				t.Fatalf("recorded RetryAfter = %s, want the capped request %s", got.RetryAfter, requested)
			}
			if store.retryableCalls != tc.wantRetry || store.terminalCalls != tc.wantTerminal {
				t.Fatalf("retryable/terminal calls = %d/%d, want %d/%d",
					store.retryableCalls, store.terminalCalls, tc.wantRetry, tc.wantTerminal)
			}
			if store.failureClass != tc.wantClass {
				t.Fatalf("durable failure class = %q, want %q", store.failureClass, tc.wantClass)
			}
			var rm metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &rm); err != nil {
				t.Fatalf("Collect() error = %v, want nil", err)
			}
			if got := sumWithAttr(rm, "eshu_dp_workflow_claim_attempt_budget_exhausted_total", "collector_kind", "repo"); got != tc.wantBudget {
				t.Fatalf("attempt_budget_exhausted_total = %d, want %d", got, tc.wantBudget)
			}
			if tc.wantRetry == 1 {
				if applied := store.retryVisibleAt.Sub(testObservedAt()); applied != tc.wantVisible {
					t.Fatalf("applied delay = %s, want %s", applied, tc.wantVisible)
				}
			}
		})
	}
}
