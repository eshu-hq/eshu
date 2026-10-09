// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type fakeStore struct {
	selector   Selector
	known      []KnownScope
	prior      []Observation
	knownErr   error
	priorErr   error
	upsertErr  error
	knownCalls int
	priorCalls int
	upserts    []Batch
	// knownHosts records every KnownScopes host argument. When knownByHost
	// is non-nil, KnownScopes returns knownByHost[host] instead of known,
	// the way the Postgres store filters by remote host.
	knownHosts  []string
	knownByHost map[string][]KnownScope
	// sweeps records every DeleteExpiredObservations call; it returns
	// sweepDeleted and sweepErr.
	sweeps       []sweepCall
	sweepDeleted int64
	sweepErr     error
}

type sweepCall struct {
	now   time.Time
	grace time.Duration
}

func (s *fakeStore) DeleteExpiredObservations(_ context.Context, now time.Time, grace time.Duration) (int64, error) {
	s.sweeps = append(s.sweeps, sweepCall{now: now, grace: grace})
	return s.sweepDeleted, s.sweepErr
}

func (s *fakeStore) expected() Selector {
	if s.selector.ID == "" {
		return testSelector
	}
	return s.selector
}

func (s *fakeStore) KnownScopes(_ context.Context, owner, host string) ([]KnownScope, error) {
	s.knownCalls++
	s.knownHosts = append(s.knownHosts, host)
	if owner != s.expected().Owner {
		return nil, errors.New("unexpected owner " + owner)
	}
	if s.knownByHost != nil {
		return s.knownByHost[host], s.knownErr
	}
	return s.known, s.knownErr
}

func (s *fakeStore) Observations(_ context.Context, selectorID string) ([]Observation, error) {
	s.priorCalls++
	if selectorID != s.expected().ID {
		return nil, errors.New("unexpected selector " + selectorID)
	}
	return s.prior, s.priorErr
}

func (s *fakeStore) UpsertObservations(_ context.Context, batch Batch) error {
	s.upserts = append(s.upserts, batch)
	return s.upsertErr
}

type observerHarness struct {
	observer Observer
	reader   *sdkmetric.ManualReader
	logs     *bytes.Buffer
}

func newObserverHarness(t *testing.T, store Store) observerHarness {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("selection-observer-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	logs := &bytes.Buffer{}
	return observerHarness{
		observer: Observer{
			Store: store, Instruments: inst,
			Logger: slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		},
		reader: reader,
		logs:   logs,
	}
}

func qaRequest(listing Listing) Request {
	return Request{Selector: testSelector, SourceMode: "githubOrg", RepoShardCount: 1, RepoLimit: 4000, Now: cycleOne, LivenessWindow: testWindow, Listing: listing, SweepExpired: true}
}

func TestObserverTruncatedListingNeverTouchesTheStore(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	h := newObserverHarness(t, store)
	_, listing := qaFixture()
	listing.Complete = false
	result := h.observer.Observe(context.Background(), qaRequest(listing))
	if result.Outcome != OutcomeListingTruncated {
		t.Fatalf("outcome = %q, want %q", result.Outcome, OutcomeListingTruncated)
	}
	if store.knownCalls+store.priorCalls+len(store.upserts) != 0 {
		t.Fatalf("store calls = %d known, %d prior, %d upserts; want none", store.knownCalls, store.priorCalls, len(store.upserts))
	}
	warn := h.logLine(t, "git_repository_selection_listing_truncated")
	if warn["level"] != "WARN" || warn["repo_limit"] != float64(4000) {
		t.Fatalf("truncated log = %v, want WARN with repo_limit 4000", warn)
	}
	h.assertOutcomeCount(t, OutcomeListingTruncated, 1)
	h.assertNoGauge(t)
}

func TestObserverStoreFailuresAreClassifiedAndNonFatal(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	cases := []struct {
		name      string
		store     *fakeStore
		wantClass string
		upserts   int
	}{
		{name: "known scopes read", store: &fakeStore{knownErr: errors.New("conn reset")}, wantClass: FailureClassKnownScopesRead},
		{name: "observations read", store: &fakeStore{known: known, priorErr: errors.New("conn reset")}, wantClass: FailureClassObservationsRead},
		{name: "upsert", store: &fakeStore{known: known, upsertErr: errors.New("deadline")}, wantClass: FailureClassUpsert, upserts: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newObserverHarness(t, tc.store)
			result := h.observer.Observe(context.Background(), qaRequest(listing))
			if result.Outcome != OutcomeStoreError {
				t.Fatalf("outcome = %q, want %q", result.Outcome, OutcomeStoreError)
			}
			if len(tc.store.upserts) != tc.upserts {
				t.Fatalf("upserts = %d, want %d", len(tc.store.upserts), tc.upserts)
			}
			warn := h.logLine(t, "git_repository_selection_store_failed")
			if warn["level"] != "WARN" || warn["failure_class"] != tc.wantClass {
				t.Fatalf("store failure log = %v, want WARN failure_class %s", warn, tc.wantClass)
			}
			h.assertOutcomeCount(t, OutcomeStoreError, 1)
			h.assertNoGauge(t)
		})
	}
}

func TestObserverWithoutStoreReportsAStoreError(t *testing.T) {
	t.Parallel()

	h := newObserverHarness(t, nil)
	_, listing := qaFixture()
	if result := h.observer.Observe(context.Background(), qaRequest(listing)); result.Outcome != OutcomeStoreError {
		t.Fatalf("outcome = %q, want %q", result.Outcome, OutcomeStoreError)
	}
	if warn := h.logLine(t, "git_repository_selection_store_failed"); warn["failure_class"] != FailureClassStoreMissing {
		t.Fatalf("store failure log = %v, want failure_class %s", warn, FailureClassStoreMissing)
	}
}

func TestObserverGuardTrippedWritesNothing(t *testing.T) {
	t.Parallel()

	known, _ := qaFixture()
	store := &fakeStore{known: known}
	h := newObserverHarness(t, store)
	result := h.observer.Observe(context.Background(), qaRequest(Listing{Complete: true}))
	if result.Outcome != OutcomeGuardTripped || len(store.upserts) != 0 {
		t.Fatalf("empty listing = %q with %d upserts, want %q with none", result.Outcome, len(store.upserts), OutcomeGuardTripped)
	}
	warn := h.logLine(t, "git_repository_selection_guard_tripped")
	if warn["level"] != "WARN" || warn["guard_threshold"] != float64(81) || warn["known_scope_count"] != float64(802) {
		t.Fatalf("guard log = %v, want WARN with threshold 81 over 802 known", warn)
	}
	h.assertOutcomeCount(t, OutcomeGuardTripped, 1)
	h.assertNoGauge(t)
}

func TestObserverEvaluatedWritesOneBatchAndSamplesTheGauge(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	store := &fakeStore{known: known}
	h := newObserverHarness(t, store)
	result := h.observer.Observe(context.Background(), qaRequest(listing))
	if result.Outcome != OutcomeEvaluated || len(store.upserts) != 1 {
		t.Fatalf("outcome = %q with %d upserts, want %q with one", result.Outcome, len(store.upserts), OutcomeEvaluated)
	}
	if got := store.upserts[0]; got.Selector != testSelector || len(got.Rows) != 802 || !got.EvaluatedAt.Equal(cycleOne) {
		t.Fatalf("upsert batch = selector %+v, %d rows at %v", got.Selector, len(got.Rows), got.EvaluatedAt)
	}
	info := h.logLine(t, "git_repository_selection_evaluated")
	want := map[string]any{
		"level": "INFO", "collector_kind": "git", "selector_id": testSelector.ID, "selector_kind": "github_org",
		"source_mode": "githubOrg", "repo_shard_count": float64(1), "listing_complete": true, "listed_count": float64(779),
		"selectable_count": float64(778), "archived_excluded_count": float64(1), "rule_excluded_count": float64(0),
		"known_scope_count": float64(802), "newly_unlisted_count": float64(25), "not_listed_count": float64(25),
		"relisted_count": float64(0), "outcome": "evaluated",
		"evaluation_gap_seconds": float64(0), "liveness_window_seconds": float64(172800),
	}
	for key, value := range want {
		if info[key] != value {
			t.Fatalf("evaluated log %s = %v, want %v (log %v)", key, info[key], value, info)
		}
	}
	if _, ok := info["evaluation_interval_seconds"]; ok {
		t.Fatalf("evaluated log still carries evaluation_interval_seconds: %v", info)
	}
	if h.hasLog(t, "git_repository_selection_liveness_lapsed") {
		t.Fatal("a first evaluation has no gap and must not log liveness_lapsed")
	}
	if sample, ok := info["not_listed_sample"].([]any); !ok || len(sample) != 10 {
		t.Fatalf("not_listed_sample = %v, want 10 slugs", info["not_listed_sample"])
	}
	h.assertOutcomeCount(t, OutcomeEvaluated, 1)
	gauge := h.gauge(t)
	wantGauge := map[string]int64{
		telemetry.RepositorySelectionStateSelected:         776,
		telemetry.RepositorySelectionStateNotListedPending: 25,
		telemetry.RepositorySelectionStateNotListed:        0,
		telemetry.RepositorySelectionStateArchivedExcluded: 1,
		telemetry.RepositorySelectionStateRuleExcluded:     0,
	}
	for state, value := range wantGauge {
		if got, ok := gauge[state]; !ok || got != value {
			t.Fatalf("gauge state %s = %d (present %v), want %d; gauge %v", state, got, ok, value, gauge)
		}
	}
}

func (h observerHarness) logLine(t *testing.T, msg string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(h.logs.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		if record["msg"] == msg {
			return record
		}
	}
	t.Fatalf("no %q log in:\n%s", msg, h.logs.String())
	return nil
}

func (h observerHarness) collect(t *testing.T) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return rm
}

func (h observerHarness) hasLog(t *testing.T, msg string) bool {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(h.logs.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		if record["msg"] == msg {
			return true
		}
	}
	return false
}

func (h observerHarness) assertOutcomeCount(t *testing.T, outcome Outcome, want int64) {
	t.Helper()
	h.assertKindOutcomeCount(t, KindGitHubOrg, outcome, want)
}

// assertKindOutcomeCount asserts the counter sample for selectorKind and
// outcome, and that it is the only evaluation sample recorded.
func (h observerHarness) assertKindOutcomeCount(t *testing.T, selectorKind string, outcome Outcome, want int64) {
	t.Helper()
	for _, scope := range h.collect(t).ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_collector_repository_selection_evaluations_total" {
				continue
			}
			points := m.Data.(metricdata.Sum[int64]).DataPoints
			if len(points) != 1 {
				t.Fatalf("evaluation counter has %d samples, want 1", len(points))
			}
			dp := points[0]
			collector, _ := dp.Attributes.Value(telemetry.MetricDimensionCollectorKind)
			kind, _ := dp.Attributes.Value(telemetry.MetricDimensionSelectorKind)
			got, _ := dp.Attributes.Value(telemetry.MetricDimensionOutcome)
			if collector.AsString() != "git" || kind.AsString() != selectorKind || got.AsString() != string(outcome) || dp.Value != want {
				t.Fatalf("evaluation sample = %v value %d, want git/%s/%s value %d", dp.Attributes.ToSlice(), dp.Value, selectorKind, outcome, want)
			}
			return
		}
	}
	t.Fatalf("no evaluation counter sample for %s outcome %s", selectorKind, outcome)
}

func (h observerHarness) gauge(t *testing.T) map[string]int64 {
	t.Helper()
	values := map[string]int64{}
	for _, scope := range h.collect(t).ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_collector_repository_selection_scopes" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Gauge[int64]).DataPoints {
				state, _ := dp.Attributes.Value(telemetry.MetricDimensionState)
				values[state.AsString()] = dp.Value
			}
		}
	}
	return values
}

func (h observerHarness) assertNoGauge(t *testing.T) {
	t.Helper()
	if gauge := h.gauge(t); len(gauge) != 0 {
		t.Fatalf("gauge sampled %v, want no sample", gauge)
	}
}
