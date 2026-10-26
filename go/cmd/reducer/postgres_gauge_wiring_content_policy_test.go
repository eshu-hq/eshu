// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/snapshot"
)

type stubContentPolicyRawRows struct{ count int64 }

func (s stubContentPolicyRawRows) ContentPolicyRawRows(context.Context) (int64, error) {
	return s.count, nil
}

// TestContentPolicyRawRowsGaugeServesTheSnapshot proves the #8833 exposure
// gauge is fed by the Postgres gauge refresher (a background read, never a
// query on scrape) and serves the observer's count.
func TestContentPolicyRawRowsGaugeServesTheSnapshot(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	meter := provider.Meter("test")
	refresher, err := snapshot.New(snapshot.Config{Interval: time.Hour, Timeout: time.Second, Meter: meter})
	if err != nil {
		t.Fatalf("snapshot.New: %v", err)
	}
	if err := registerContentPolicyRawRowsGauge(refresher, &telemetry.Instruments{}, meter, stubContentPolicyRawRows{count: 3}); err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	wait := startPostgresGaugeRefresher(ctx, refresher)
	t.Cleanup(func() { cancel(); wait() })

	deadline := time.Now().Add(3 * time.Second)
	for {
		var rm metricdata.ResourceMetrics
		_ = reader.Collect(context.Background(), &rm)
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				if data, ok := m.Data.(metricdata.Gauge[int64]); ok && m.Name == "eshu_dp_content_policy_raw_rows" && len(data.DataPoints) == 1 {
					if got := data.DataPoints[0].Value; got != 3 {
						t.Fatalf("eshu_dp_content_policy_raw_rows = %d, want 3", got)
					}
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("eshu_dp_content_policy_raw_rows never reported")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestContentPolicyRawRowsGaugeSkipsWithoutAPolicy registers nothing when the
// registry governs no path (a nil observer).
func TestContentPolicyRawRowsGaugeSkipsWithoutAPolicy(t *testing.T) {
	if err := registerContentPolicyRawRowsGauge(nil, &telemetry.Instruments{}, nil, nil); err != nil {
		t.Fatalf("a nil observer must be a no-op, got %v", err)
	}
}
