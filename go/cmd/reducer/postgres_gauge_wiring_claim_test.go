// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// The production store the reducer hands to registerPostgresBackedGauges must
// keep providing both invariants and the source-queue gauges; the wrappers only
// forward what the store offers.
var (
	_ telemetry.ProjectorClaimInvariantObserver = (*postgres.QueueObserverStore)(nil)
	_ telemetry.SourceQueueObserver             = (*postgres.QueueObserverStore)(nil)
)

// scriptedClaimStore stands in for postgres.QueueObserverStore: the queue
// observer contracts plus the #7115 projector claim invariants.
type scriptedClaimStore struct {
	scriptedPostgresGauges
	multipleLeases int64
	missingFence   int64
	err            error
}

func (s scriptedClaimStore) ProjectorScopesWithMultipleLiveLeases(context.Context) (int64, error) {
	return s.multipleLeases, s.err
}

func (s scriptedClaimStore) ProjectorScopesMissingClaimFence(context.Context) (int64, error) {
	return s.missingFence, s.err
}

// scriptedClaimQueueOnlyStore hides the source-queue methods so the
// non-source cached observer path is exercised.
type scriptedClaimQueueOnlyStore struct{ inner scriptedClaimStore }

func (s scriptedClaimQueueOnlyStore) QueueDepths(ctx context.Context) (map[string]map[string]int64, error) {
	return s.inner.QueueDepths(ctx)
}

func (s scriptedClaimQueueOnlyStore) QueueOldestAge(ctx context.Context) (map[string]float64, error) {
	return s.inner.QueueOldestAge(ctx)
}

func (s scriptedClaimQueueOnlyStore) ProjectorScopesWithMultipleLiveLeases(ctx context.Context) (int64, error) {
	return s.inner.ProjectorScopesWithMultipleLiveLeases(ctx)
}

func (s scriptedClaimQueueOnlyStore) ProjectorScopesMissingClaimFence(ctx context.Context) (int64, error) {
	return s.inner.ProjectorScopesMissingClaimFence(ctx)
}

// observeClaimGauges wires queueObs through the production
// registerPostgresBackedGauges path, starts the refresher, and polls the meter
// until both #7115 invariant gauges report or the deadline passes. It returns
// the last observed value per gauge name (absent when never observed).
func observeClaimGauges(t *testing.T, queueObs telemetry.QueueObserver) map[string]int64 {
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
		got := map[string]int64{}
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				if data, ok := m.Data.(metricdata.Gauge[int64]); ok &&
					(m.Name == "eshu_dp_projector_scopes_multiple_live_leases" ||
						m.Name == "eshu_dp_projector_scopes_missing_claim_fence") {
					for _, dp := range data.DataPoints {
						got[m.Name] = dp.Value
					}
				}
			}
		}
		if len(got) == 2 || time.Now().After(deadline) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestPostgresBackedGaugesServeProjectorClaimInvariants is the #7115/#7214
// combine regression: the claim-invariant gauges register only when the
// queue observer handed to RegisterObservableGauges implements
// telemetry.ProjectorClaimInvariantObserver. #7214 hands it snapshot wrappers,
// so the wrappers must carry the invariants or neither gauge exists in any
// binary. Both wrapper shapes (with and without source-queue gauges) are
// covered.
func TestPostgresBackedGaugesServeProjectorClaimInvariants(t *testing.T) {
	store := scriptedClaimStore{multipleLeases: 3, missingFence: 5}
	tests := []struct {
		name     string
		queueObs telemetry.QueueObserver
	}{
		{name: "source-queue observer", queueObs: store},
		{name: "queue-only observer", queueObs: scriptedClaimQueueOnlyStore{inner: store}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := observeClaimGauges(t, tt.queueObs)
			if v, ok := got["eshu_dp_projector_scopes_multiple_live_leases"]; !ok || v != 3 {
				t.Errorf("multiple_live_leases = %d (observed=%v), want 3", v, ok)
			}
			if v, ok := got["eshu_dp_projector_scopes_missing_claim_fence"]; !ok || v != 5 {
				t.Errorf("missing_claim_fence = %d (observed=%v), want 5", v, ok)
			}
		})
	}
}

// TestProjectorClaimInvariantGaugesAbsentWithoutObserver proves an observer
// that does not implement the invariants still registers no claim gauges, so
// the assertion keeps its exact meaning.
func TestProjectorClaimInvariantGaugesAbsentWithoutObserver(t *testing.T) {
	got := observeClaimGauges(t, scriptedPostgresGauges{})
	if len(got) != 0 {
		t.Fatalf("claim gauges observed without an invariant observer: %v", got)
	}
}

// TestProjectorClaimInvariantGaugesReportNothingBeforeFirstRefresh proves the
// scalar invariants observe nothing, not a false zero, until the first
// snapshot publishes: a zero means the invariant holds.
func TestProjectorClaimInvariantGaugesReportNothingBeforeFirstRefresh(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	getenv := func(string) string { return "" }
	if _, err := registerPostgresBackedGauges(
		&telemetry.Instruments{}, provider.Meter("test"),
		scriptedClaimStore{multipleLeases: 3, missingFence: 5}, nil, nil, nil, nil, getenv, nil,
	); err != nil {
		t.Fatalf("registerPostgresBackedGauges() error = %v", err)
	}
	var rm metricdata.ResourceMetrics
	_ = reader.Collect(context.Background(), &rm)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "eshu_dp_projector_scopes_multiple_live_leases" ||
				m.Name == "eshu_dp_projector_scopes_missing_claim_fence" {
				t.Fatalf("invariant gauge %q reported before the first snapshot", m.Name)
			}
		}
	}
}

// TestProjectorClaimInvariantRefreshFailureKeepsGaugesUnobserved proves a
// failing invariant read never publishes a zero snapshot.
func TestProjectorClaimInvariantRefreshFailureKeepsGaugesUnobserved(t *testing.T) {
	got := observeClaimGauges(t, scriptedClaimStore{err: errors.New("boom")})
	if len(got) != 0 {
		t.Fatalf("claim gauges observed after failed refreshes: %v", got)
	}
}
