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

// TestProjectorMarkedWriteAgeRefreshFailureKeepsGaugeUnobserved proves a
// failing age read never publishes a zero snapshot.
func TestProjectorMarkedWriteAgeRefreshFailureKeepsGaugeUnobserved(t *testing.T) {
	if got, ok := observeMarkedWriteGauge(t, scriptedMarkedWriteStore{err: errors.New("boom")}); ok {
		t.Fatalf("marked-write age gauge observed after failed refreshes: %v", got)
	}
}
