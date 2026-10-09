// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/snapshot"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// The production store the reducer hands to registerPostgresBackedGauges must
// keep providing the marked-write age; the wrapper only forwards what the
// store offers.
var _ telemetry.ProjectorMarkedWriteObserver = (*postgres.QueueObserverStore)(nil)

// scriptedMarkedWriteStore stands in for postgres.QueueObserverStore: the
// queue observer contracts plus the #7471 marked-write age.
type scriptedMarkedWriteStore struct {
	scriptedPostgresGauges
	age float64
	err error
}

func (s scriptedMarkedWriteStore) ProjectorMarkedWriteOldestAge(context.Context) (float64, error) {
	return s.age, s.err
}

// scriptedMarkedWriteClaimStore additionally reports the #7115 claim
// invariants, so the marked-write wrapper composes with the claim wrapper.
type scriptedMarkedWriteClaimStore struct {
	scriptedMarkedWriteStore
	multipleLeases int64
	missingFence   int64
}

func (s scriptedMarkedWriteClaimStore) ProjectorScopesWithMultipleLiveLeases(context.Context) (int64, error) {
	return s.multipleLeases, s.err
}

func (s scriptedMarkedWriteClaimStore) ProjectorScopesMissingClaimFence(context.Context) (int64, error) {
	return s.missingFence, s.err
}

// scriptedMarkedWriteQueueOnlyStore hides the source-queue methods so the
// non-source cached observer path is exercised.
type scriptedMarkedWriteQueueOnlyStore struct{ inner scriptedMarkedWriteStore }

func (s scriptedMarkedWriteQueueOnlyStore) QueueDepths(ctx context.Context) (map[string]map[string]int64, error) {
	return s.inner.QueueDepths(ctx)
}

func (s scriptedMarkedWriteQueueOnlyStore) QueueOldestAge(ctx context.Context) (map[string]float64, error) {
	return s.inner.QueueOldestAge(ctx)
}

func (s scriptedMarkedWriteQueueOnlyStore) ProjectorMarkedWriteOldestAge(ctx context.Context) (float64, error) {
	return s.inner.ProjectorMarkedWriteOldestAge(ctx)
}

// scriptedMarkedWriteClaimQueueOnlyStore is the queue-only store plus the
// #7115 claim invariants.
type scriptedMarkedWriteClaimQueueOnlyStore struct{ inner scriptedMarkedWriteClaimStore }

func (s scriptedMarkedWriteClaimQueueOnlyStore) QueueDepths(ctx context.Context) (map[string]map[string]int64, error) {
	return s.inner.QueueDepths(ctx)
}

func (s scriptedMarkedWriteClaimQueueOnlyStore) QueueOldestAge(ctx context.Context) (map[string]float64, error) {
	return s.inner.QueueOldestAge(ctx)
}

func (s scriptedMarkedWriteClaimQueueOnlyStore) ProjectorMarkedWriteOldestAge(ctx context.Context) (float64, error) {
	return s.inner.ProjectorMarkedWriteOldestAge(ctx)
}

func (s scriptedMarkedWriteClaimQueueOnlyStore) ProjectorScopesWithMultipleLiveLeases(ctx context.Context) (int64, error) {
	return s.inner.ProjectorScopesWithMultipleLiveLeases(ctx)
}

func (s scriptedMarkedWriteClaimQueueOnlyStore) ProjectorScopesMissingClaimFence(ctx context.Context) (int64, error) {
	return s.inner.ProjectorScopesMissingClaimFence(ctx)
}

// observeMarkedWriteGauge wires queueObs through the production
// registerPostgresBackedGauges path, starts the refresher, and polls the meter
// until the #7471 age gauge reports or the deadline passes.
func observeMarkedWriteGauge(t *testing.T, queueObs telemetry.QueueObserver) (float64, bool) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	getenv := func(string) string { return "" }
	refresher, err := registerPostgresBackedGauges(
		&telemetry.Instruments{}, provider.Meter("test"), queueObs, nil, nil, nil, nil, getenv, nil,
	)
	if err != nil {
		t.Fatalf("registerPostgresBackedGauges() error = %v", err)
	}
	wait := startPostgresGaugeRefresher(ctx, refresher)
	t.Cleanup(func() { cancel(); wait() })

	deadline := time.Now().Add(3 * time.Second)
	for {
		var rm metricdata.ResourceMetrics
		_ = reader.Collect(context.Background(), &rm)
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				if data, ok := m.Data.(metricdata.Gauge[float64]); ok &&
					m.Name == "eshu_dp_projector_marked_write_oldest_age_seconds" &&
					len(data.DataPoints) == 1 {
					return data.DataPoints[0].Value, true
				}
			}
		}
		if time.Now().After(deadline) {
			return 0, false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestPostgresBackedGaugesServeProjectorMarkedWriteAge proves the #7471 age
// gauge registers when the queue observer implements
// telemetry.ProjectorMarkedWriteObserver, alone and composed with the claim
// invariants, and that the snapshot millis round-trip preserves the reading.
func TestPostgresBackedGaugesServeProjectorMarkedWriteAge(t *testing.T) {
	tests := []struct {
		name     string
		queueObs telemetry.QueueObserver
	}{
		{name: "marked-write observer", queueObs: scriptedMarkedWriteStore{age: 90.5}},
		{name: "marked-write plus claim observer", queueObs: scriptedMarkedWriteClaimStore{scriptedMarkedWriteStore: scriptedMarkedWriteStore{age: 90.5}, multipleLeases: 1, missingFence: 0}},
		{name: "queue-only marked-write observer", queueObs: scriptedMarkedWriteQueueOnlyStore{inner: scriptedMarkedWriteStore{age: 90.5}}},
		{name: "queue-only marked-write plus claim observer", queueObs: scriptedMarkedWriteClaimQueueOnlyStore{inner: scriptedMarkedWriteClaimStore{scriptedMarkedWriteStore: scriptedMarkedWriteStore{age: 90.5}, multipleLeases: 1, missingFence: 0}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := observeMarkedWriteGauge(t, tt.queueObs)
			if !ok || got != 90.5 {
				t.Errorf("marked_write_oldest_age_seconds = %v (observed=%v), want 90.5", got, ok)
			}
		})
	}
}

// TestPostgresBackedGaugesServeClaimInvariantsThroughMarkedWriteWrapper proves
// the claim gauges still fire when the marked-write wrapper composes around
// the claim wrapper, which is the production path: the store implements both
// observers, so both wrappers always apply.
func TestPostgresBackedGaugesServeClaimInvariantsThroughMarkedWriteWrapper(t *testing.T) {
	store := scriptedMarkedWriteClaimStore{scriptedMarkedWriteStore: scriptedMarkedWriteStore{age: 12.5}, multipleLeases: 3, missingFence: 5}
	if got, ok := observeMarkedWriteGauge(t, store); !ok || got != 12.5 {
		t.Fatalf("marked_write_oldest_age_seconds = %v (observed=%v), want 12.5", got, ok)
	}
	got := observeClaimGauges(t, store)
	if v, ok := got["eshu_dp_projector_scopes_multiple_live_leases"]; !ok || v != 3 {
		t.Errorf("multiple_live_leases = %d (observed=%v), want 3", v, ok)
	}
	if v, ok := got["eshu_dp_projector_scopes_missing_claim_fence"]; !ok || v != 5 {
		t.Errorf("missing_claim_fence = %d (observed=%v), want 5", v, ok)
	}
}

// TestProjectorMarkedWriteAgeGaugeAbsentWithoutObserver proves an observer
// that does not implement the marked-write contract still registers no age
// gauge, so the assertion keeps its exact meaning.
func TestProjectorMarkedWriteAgeGaugeAbsentWithoutObserver(t *testing.T) {
	if got, ok := observeMarkedWriteGauge(t, scriptedPostgresGauges{}); ok {
		t.Fatalf("marked-write age gauge observed without a marked-write observer: %v", got)
	}
}

// TestProjectorMarkedWriteAgeGaugeReportsNothingBeforeFirstRefresh proves the
// age gauge observes nothing, not a false zero, until the first snapshot
// publishes: a zero means no marked write is outstanding.
func TestProjectorMarkedWriteAgeGaugeReportsNothingBeforeFirstRefresh(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	getenv := func(string) string { return "" }
	if _, err := registerPostgresBackedGauges(
		&telemetry.Instruments{}, provider.Meter("test"),
		scriptedMarkedWriteStore{age: 90.5}, nil, nil, nil, nil, getenv, nil,
	); err != nil {
		t.Fatalf("registerPostgresBackedGauges() error = %v", err)
	}
	var rm metricdata.ResourceMetrics
	_ = reader.Collect(context.Background(), &rm)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "eshu_dp_projector_marked_write_oldest_age_seconds" {
				t.Fatal("marked-write age gauge reported before the first snapshot")
			}
		}
	}
}

// TestWithProjectorMarkedWriteAgePreservesUpstreamContracts pins that every
// upstream cached shape keeps its optional contracts through the marked-write
// wrapper: embedding the telemetry.QueueObserver interface instead would drop
// the source-queue and claim-invariant methods (only the interface's own two
// methods promote) and their gauges would silently unregister.
func TestWithProjectorMarkedWriteAgePreservesUpstreamContracts(t *testing.T) {
	markedObs := scriptedMarkedWriteStore{age: 1}
	tests := []struct {
		name       string
		cached     telemetry.QueueObserver
		wantType   string
		wantSource bool
		wantClaim  bool
	}{
		{name: "plain", cached: cachedQueueObserver{}, wantType: "cachedMarkedWriteQueueObserver"},
		{name: "source", cached: cachedSourceQueueObserver{}, wantType: "cachedMarkedWriteSourceQueueObserver", wantSource: true},
		{name: "claim", cached: cachedClaimQueueObserver{}, wantType: "cachedMarkedWriteClaimQueueObserver", wantClaim: true},
		{name: "claim source", cached: cachedClaimSourceQueueObserver{}, wantType: "cachedMarkedWriteClaimSourceQueueObserver", wantSource: true, wantClaim: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refresher, err := snapshot.New(snapshot.Config{})
			if err != nil {
				t.Fatalf("snapshot.New() error = %v", err)
			}
			wrapped, err := withProjectorMarkedWriteAge(refresher, tt.cached, markedObs)
			if err != nil {
				t.Fatalf("withProjectorMarkedWriteAge() error = %v", err)
			}
			if got := typeName(wrapped); got != tt.wantType {
				t.Fatalf("wrapped type = %s, want %s", got, tt.wantType)
			}
			if _, ok := wrapped.(telemetry.ProjectorMarkedWriteObserver); !ok {
				t.Fatalf("%s does not implement ProjectorMarkedWriteObserver", tt.wantType)
			}
			if _, ok := wrapped.(telemetry.SourceQueueObserver); ok != tt.wantSource {
				t.Fatalf("%s SourceQueueObserver = %v, want %v", tt.wantType, ok, tt.wantSource)
			}
			if _, ok := wrapped.(telemetry.ProjectorClaimInvariantObserver); ok != tt.wantClaim {
				t.Fatalf("%s ProjectorClaimInvariantObserver = %v, want %v", tt.wantType, ok, tt.wantClaim)
			}
		})
	}
}

func typeName(v any) string {
	return strings.TrimPrefix(fmt.Sprintf("%T", v), "main.")
}

// TestProjectorMarkedWriteAgeRefreshFailureKeepsGaugeUnobserved proves a
// failing age read never publishes a zero snapshot.
func TestProjectorMarkedWriteAgeRefreshFailureKeepsGaugeUnobserved(t *testing.T) {
	if got, ok := observeMarkedWriteGauge(t, scriptedMarkedWriteStore{err: errors.New("boom")}); ok {
		t.Fatalf("marked-write age gauge observed after failed refreshes: %v", got)
	}
}
