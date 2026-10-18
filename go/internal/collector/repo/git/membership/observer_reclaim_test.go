// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Every cycle writes the selector liveness marker, including a guard-tripped
// cycle that writes no observation rows (#7840): the fresh marker is what
// keeps that selector's not_listed guard history out of the reclaim. A
// store_error cycle touches it too: an erroring selector is not abandoned,
// so writing the marker on error is the safe direction, and the touch never
// changes the cycle's outcome.
func TestObserverTouchesSelectorCycleOnEveryOutcome(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	truncated := listing
	truncated.Complete = false
	cases := []struct {
		name     string
		listing  Listing
		knownErr error
		outcome  Outcome
	}{
		{name: "evaluated", listing: listing, outcome: OutcomeEvaluated},
		{name: "guard tripped", listing: Listing{Complete: true}, outcome: OutcomeGuardTripped},
		{name: "listing truncated", listing: truncated, outcome: OutcomeListingTruncated},
		{name: "store error", listing: listing, knownErr: errors.New("conn reset"), outcome: OutcomeStoreError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeStore{known: known, knownErr: tc.knownErr}
			h := newObserverHarness(t, store)
			request := qaRequest(tc.listing)
			result := h.observer.Observe(context.Background(), request)
			if result.Outcome != tc.outcome {
				t.Fatalf("outcome = %q, want %q", result.Outcome, tc.outcome)
			}
			if len(store.touches) != 1 {
				t.Fatalf("selector touches = %d, want exactly one per cycle", len(store.touches))
			}
			if got := store.touches[0]; got.selector.ID != testSelector.ID || !got.at.Equal(cycleOne) || got.window != livenessWindow(request.LivenessWindow) {
				t.Fatalf("touch = %+v, want selector %s at %v window %v", got, testSelector.ID, cycleOne, livenessWindow(request.LivenessWindow))
			}
		})
	}
}

func TestObserverSelectorTouchFailureKeepsOutcomeAndLogsClass(t *testing.T) {
	t.Parallel()

	known, _ := qaFixture()
	store := &fakeStore{known: known, touchErr: errors.New("conn reset")}
	h := newObserverHarness(t, store)
	result := h.observer.Observe(context.Background(), qaRequest(Listing{Complete: true}))
	if result.Outcome != OutcomeGuardTripped {
		t.Fatalf("outcome = %q, want %q: a marker failure must not change the outcome", result.Outcome, OutcomeGuardTripped)
	}
	warn := h.logLine(t, "git_repository_selection_store_failed")
	if warn["failure_class"] != FailureClassSelectorCycleUpsert {
		t.Fatalf("failure_class = %v, want %s (log %v)", warn["failure_class"], FailureClassSelectorCycleUpsert, warn)
	}
}

func TestObserverReportsReclaimedNotListed(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	store := &fakeStore{known: known, sweepDeleted: 10, sweepReclaimed: 3}
	h := newObserverHarness(t, store)
	result := h.observer.Observe(context.Background(), qaRequest(listing))
	if result.ExpiredDeleted != 10 || result.NotListedReclaimed != 3 {
		t.Fatalf("deleted = %d reclaimed = %d, want 10 and 3", result.ExpiredDeleted, result.NotListedReclaimed)
	}
	if info := h.logLine(t, "git_repository_selection_evaluated"); info["not_listed_reclaimed_count"] != float64(3) {
		t.Fatalf("evaluated log not_listed_reclaimed_count = %v, want 3 (log %v)", info["not_listed_reclaimed_count"], info)
	}
	const reclaimedMetric = "eshu_dp_collector_repository_selection_not_listed_reclaimed_total"
	var points []metricdata.DataPoint[int64]
	for _, scope := range h.collect(t).ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == reclaimedMetric {
				points = m.Data.(metricdata.Sum[int64]).DataPoints
			}
		}
	}
	if len(points) != 1 || points[0].Value != 3 {
		t.Fatalf("%s points = %v, want one sample of 3", reclaimedMetric, points)
	}
	collector, _ := points[0].Attributes.Value(telemetry.MetricDimensionCollectorKind)
	if collector.AsString() != "git" || points[0].Attributes.Len() != 1 {
		t.Fatalf("%s sample attrs = %v, want collector_kind=git only", reclaimedMetric, points[0].Attributes.ToSlice())
	}
}
