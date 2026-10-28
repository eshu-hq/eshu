// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestClaimedServiceDoesNotObserveWhenUnchangedResolutionFails pins the #9420
// guard on the unchanged path: when resolvedCompletionMutation rejects the
// generation (terraform_state identity mismatch), the claim fails terminal
// and the post-commit observer never fires.
func TestClaimedServiceDoesNotObserveWhenUnchangedResolutionFails(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.June, 1, 12, 20, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.CollectorKind = scope.CollectorTerraformState
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
	mismatched := testGeneration(now)
	mismatched.ScopeID = "a-different-scope"
	source := &stubClaimedSourceWithObserver{
		stubClaimedSource: &stubClaimedSource{
			collected: CollectedGeneration{Scope: testScope(), Generation: mismatched, Unchanged: true},
			ok:        true,
		},
	}
	service := testClaimedService(now, claim, scope.CollectorTerraformState, store, source, &stubClaimedCommitter{})

	if err := service.Run(ctx); err == nil {
		t.Fatal("Run() error = nil, want the identity mismatch")
	}
	if got, want := store.terminalFailCalls, 1; got != want {
		t.Fatalf("terminal fail calls = %d, want %d", got, want)
	}
	if got := source.observeCalls; got != 0 {
		t.Fatalf("observe calls = %d, want 0 when unchanged resolution fails", got)
	}
	if got := store.completeCalls; got != 0 {
		t.Fatalf("complete calls = %d, want 0", got)
	}
}

// TestClaimedServiceObserverErrorIsNonFatalAndCounted proves, for both the
// committed and the unchanged path (#9420), that the observer fires exactly
// once, that its error never fails the claim, and that the error is visible
// to an operator as the claimed-run span event and the
// eshu_dp_workflow_claim_commit_observer_failures_total counter.
func TestClaimedServiceObserverErrorIsNonFatalAndCounted(t *testing.T) {
	t.Parallel()

	for _, unchanged := range []bool{false, true} {
		name := "committed"
		if unchanged {
			name = "unchanged"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			now := time.Date(2026, time.June, 1, 12, 25, 0, 0, time.UTC)
			item := testClaimedWorkItem(now)
			claim := testWorkflowClaim(item.WorkItemID, now)
			store := &stubClaimStore{
				item:  item,
				claim: claim,
				found: true,
				heartbeat: func(context.Context, workflow.ClaimMutation) error {
					cancel()
					return nil
				},
			}
			collected := FactsFromSlice(testScope(), testGeneration(now), testFacts(now))
			if unchanged {
				collected = CollectedGeneration{Scope: testScope(), Generation: testGeneration(now), Unchanged: true}
			}
			source := &stubClaimedSourceWithObserver{
				stubClaimedSource: &stubClaimedSource{collected: collected, ok: true},
				observeErr:        errors.New("progress store unavailable"),
			}
			service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})
			spans := tracetest.NewSpanRecorder()
			service.Tracer = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("test")
			reader := sdkmetric.NewManualReader()
			inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments() error = %v", err)
			}
			service.Instruments = inst

			if err := service.Run(ctx); err != nil {
				t.Fatalf("Run() error = %v, want nil: an observer error is non-fatal", err)
			}
			if got, want := source.observeCalls, 1; got != want {
				t.Fatalf("observe calls = %d, want %d", got, want)
			}
			if got, want := store.completeCalls, 1; got != want {
				t.Fatalf("complete calls = %d, want %d", got, want)
			}
			if store.retryableFailCalls+store.terminalFailCalls != 0 {
				t.Fatalf("claim failed (retryable=%d terminal=%d), want completed", store.retryableFailCalls, store.terminalFailCalls)
			}
			if !hasSpanEvent(spans.Ended(), "claimed_generation_commit_observer_failed") {
				t.Fatal("no claimed_generation_commit_observer_failed span event")
			}
			if got := observerFailureCount(t, reader, item.SourceSystem); got != 1 {
				t.Fatalf("eshu_dp_workflow_claim_commit_observer_failures_total = %d, want 1", got)
			}
		})
	}
}

func hasSpanEvent(spans []sdktrace.ReadOnlySpan, name string) bool {
	for _, span := range spans {
		for _, event := range span.Events() {
			if event.Name == name {
				return true
			}
		}
	}
	return false
}

func observerFailureCount(t *testing.T, reader *sdkmetric.ManualReader, sourceSystem string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	want := attribute.NewSet(
		telemetry.AttrCollectorKind(string(scope.CollectorGit)),
		telemetry.AttrSourceSystem(sourceSystem),
	)
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if m.Name != "eshu_dp_workflow_claim_commit_observer_failures_total" || !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				if point.Attributes.Equals(&want) {
					total += point.Value
				}
			}
		}
	}
	return total
}
