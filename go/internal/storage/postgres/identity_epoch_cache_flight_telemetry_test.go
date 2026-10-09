// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// sumByAttribute collects every Int64 sum data point of one metric, keyed by the
// value of attrKey ("" when the point carries no such attribute).
func sumByAttribute(t *testing.T, reader sdkmetric.Reader, metricName, attrKey string) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != metricName {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s is %T, want Sum[int64]", metricName, m.Data)
			}
			for _, dp := range sum.DataPoints {
				key := ""
				if v, found := dp.Attributes.Value(attrKeyOf(attrKey)); found {
					key = v.AsString()
				}
				out[key] += dp.Value
			}
		}
	}
	return out
}

// TestIdentityEpochCacheFlightTelemetry proves the 3 AM signals: one load
// started, the discard reason when the epoch moved, and how many waiters the
// shared flight served.
func TestIdentityEpochCacheFlightTelemetry(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("flight-telemetry-test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	cache, err := NewIdentityEpochCache(inst, 0)
	if err != nil {
		t.Fatalf("NewIdentityEpochCache: %v", err)
	}
	q := newFlightQueryer(1)
	store := &FactStore{database: q, identityCache: cache}

	leader := startFlightCaller(context.Background(), store)
	awaitLoadStarted(t, q)
	followers := startFlightWaiters(t, store, 3)
	q.epoch.Store(2)
	close(q.gate)
	collectFlightCaller(t, "leader", leader)
	for i, ch := range followers {
		collectFlightCaller(t, string(rune('a'+i)), ch)
	}

	if got := sumByAttribute(t, reader, "eshu_dp_identity_cache_reload_total", "")[""]; got != 1 {
		t.Fatalf("reload_total = %d, want 1 load started", got)
	}
	if got := sumByAttribute(t, reader, "eshu_dp_identity_cache_passthrough_total", "reason")[identityDiscardEpochMoved]; got != 1 {
		t.Fatalf("passthrough_total{reason=epoch_moved} = %d, want 1", got)
	}
	if got := sumByAttribute(t, reader, "eshu_dp_identity_cache_flight_waiter_total", "outcome")[identityWaiterShared]; got != 3 {
		t.Fatalf("flight_waiter_total{outcome=shared} = %d, want 3 waiters served", got)
	}
}

// attrKeyOf builds the attribute key lookup used by sumByAttribute.
func attrKeyOf(key string) attribute.Key { return attribute.Key(key) }
