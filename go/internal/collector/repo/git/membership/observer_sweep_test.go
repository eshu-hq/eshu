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

func TestObserverSweepsExpiredRowsAfterAnEvaluation(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	store := &fakeStore{known: known, sweepDeleted: 1203}
	h := newObserverHarness(t, store)
	result := h.observer.Observe(context.Background(), qaRequest(listing))
	if result.Outcome != OutcomeEvaluated || result.ExpiredDeleted != 1203 {
		t.Fatalf("result = %q with %d expired deleted, want %q with 1203", result.Outcome, result.ExpiredDeleted, OutcomeEvaluated)
	}
	if len(store.sweeps) != 1 {
		t.Fatalf("sweeps = %d, want one per evaluation", len(store.sweeps))
	}
	if got := store.sweeps[0]; !got.now.Equal(cycleOne) || got.grace != ExpiredObservationGrace {
		t.Fatalf("sweep = now %v grace %v, want now %v grace %v", got.now, got.grace, cycleOne, ExpiredObservationGrace)
	}
	if info := h.logLine(t, "git_repository_selection_evaluated"); info["expired_deleted_count"] != float64(1203) {
		t.Fatalf("evaluated log expired_deleted_count = %v, want 1203 (log %v)", info["expired_deleted_count"], info)
	}
	h.assertDeletedCount(t, 1203)
}

func TestObserverSweepsAfterAGuardTrip(t *testing.T) {
	t.Parallel()

	known, _ := qaFixture()
	store := &fakeStore{known: known, sweepDeleted: 4}
	h := newObserverHarness(t, store)
	result := h.observer.Observe(context.Background(), qaRequest(Listing{Complete: true}))
	if result.Outcome != OutcomeGuardTripped || len(store.sweeps) != 1 || result.ExpiredDeleted != 4 {
		t.Fatalf("guard trip = %q with %d sweeps and %d deleted, want %q with one sweep deleting 4",
			result.Outcome, len(store.sweeps), result.ExpiredDeleted, OutcomeGuardTripped)
	}
	h.assertDeletedCount(t, 4)
}

func TestObserverSkipsTheSweepWithoutASuccessfulStoreRead(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	truncated := listing
	truncated.Complete = false
	cases := []struct {
		name    string
		store   *fakeStore
		listing Listing
	}{
		{name: "truncated listing", store: &fakeStore{known: known}, listing: truncated},
		{name: "known scopes read failed", store: &fakeStore{knownErr: errors.New("conn reset")}, listing: listing},
		{name: "upsert failed", store: &fakeStore{known: known, upsertErr: errors.New("deadline")}, listing: listing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newObserverHarness(t, tc.store)
			h.observer.Observe(context.Background(), qaRequest(tc.listing))
			if len(tc.store.sweeps) != 0 {
				t.Fatalf("sweeps = %d, want none", len(tc.store.sweeps))
			}
			h.assertNoDeletedCount(t)
		})
	}
}

func TestObserverSweepsOnlyWhenTheRequestAsks(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	store := &fakeStore{known: known, sweepDeleted: 9}
	h := newObserverHarness(t, store)
	request := qaRequest(listing)
	request.SweepExpired = false
	result := h.observer.Observe(context.Background(), request)
	if result.Outcome != OutcomeEvaluated || len(store.sweeps) != 0 || result.ExpiredDeleted != 0 {
		t.Fatalf("result = %q with %d sweeps and %d deleted, want %q with no sweep", result.Outcome, len(store.sweeps), result.ExpiredDeleted, OutcomeEvaluated)
	}
	h.assertNoDeletedCount(t)
}

func TestObserverSweepFailureIsLoggedAndKeepsTheOutcome(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	store := &fakeStore{known: known, sweepDeleted: 500, sweepErr: errors.New("conn reset")}
	h := newObserverHarness(t, store)
	result := h.observer.Observe(context.Background(), qaRequest(listing))
	if result.Outcome != OutcomeEvaluated || len(store.upserts) != 1 {
		t.Fatalf("outcome = %q with %d upserts, want %q with one: a sweep failure must not undo the evaluation", result.Outcome, len(store.upserts), OutcomeEvaluated)
	}
	warn := h.logLine(t, "git_repository_selection_store_failed")
	if warn["level"] != "WARN" || warn["failure_class"] != FailureClassExpiredSweep {
		t.Fatalf("sweep failure log = %v, want WARN failure_class %s", warn, FailureClassExpiredSweep)
	}
	h.assertOutcomeCount(t, OutcomeEvaluated, 1)
	h.assertDeletedCount(t, 500)
}

func TestObserverRecordsNoDeletedSampleWhenNothingExpired(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	store := &fakeStore{known: known}
	h := newObserverHarness(t, store)
	h.observer.Observe(context.Background(), qaRequest(listing))
	if len(store.sweeps) != 1 {
		t.Fatalf("sweeps = %d, want one", len(store.sweeps))
	}
	h.assertNoDeletedCount(t)
}

const deletedMetric = "eshu_dp_collector_repository_selection_observations_deleted_total"

func (h observerHarness) deletedPoints(t *testing.T) []metricdata.DataPoint[int64] {
	t.Helper()
	for _, scope := range h.collect(t).ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == deletedMetric {
				return m.Data.(metricdata.Sum[int64]).DataPoints
			}
		}
	}
	return nil
}

func (h observerHarness) assertDeletedCount(t *testing.T, want int64) {
	t.Helper()
	points := h.deletedPoints(t)
	if len(points) != 1 {
		t.Fatalf("%s has %d samples, want 1", deletedMetric, len(points))
	}
	collector, _ := points[0].Attributes.Value(telemetry.MetricDimensionCollectorKind)
	if collector.AsString() != "git" || points[0].Value != want || points[0].Attributes.Len() != 1 {
		t.Fatalf("%s sample = %v value %d, want collector_kind=git only, value %d", deletedMetric, points[0].Attributes.ToSlice(), points[0].Value, want)
	}
}

func (h observerHarness) assertNoDeletedCount(t *testing.T) {
	t.Helper()
	if points := h.deletedPoints(t); len(points) != 0 {
		t.Fatalf("%s sampled %d points, want none", deletedMetric, len(points))
	}
}
