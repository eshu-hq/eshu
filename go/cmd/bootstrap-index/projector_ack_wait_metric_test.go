// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestDrainProjectorWorkItemRecordsAckWaitMetrics proves bootstrap-index hands
// its instruments to the Ack wait, so a busy-scope deferral during a
// bootstrap drain is counted like one in the projector service.
func TestDrainProjectorWorkItemRecordsAckWaitMetrics(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	work := projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: "scope-ack-wait", SourceSystem: "git"},
		Generation:   scope.ScopeGeneration{GenerationID: "generation-1"},
		AttemptCount: 1,
	}
	var completed atomic.Int64
	err = drainProjectorWorkItem(ctx,
		&fakeWorkSource{items: []projector.ScopeGenerationWork{work}},
		&fakeFactStore{}, &fakeProjectionRunner{},
		&claimLostSink{ackErr: fmt.Errorf("lock timeout: %w", projector.ErrWorkAckDeferred)}, nil,
		time.Millisecond, 0, &completed, sdktrace.NewTracerProvider().Tracer("test"), instruments,
		slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("drainProjectorWorkItem() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	deferrals := map[string]int64{}
	waits := map[string]uint64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				if m.Name != "eshu_dp_projector_ack_deferrals_total" {
					continue
				}
				for _, point := range data.DataPoints {
					outcome, _ := point.Attributes.Value(telemetry.MetricDimensionOutcome)
					deferrals[outcome.AsString()] += point.Value
				}
			case metricdata.Histogram[float64]:
				if m.Name != "eshu_dp_projector_ack_wait_seconds" {
					continue
				}
				for _, point := range data.DataPoints {
					outcome, _ := point.Attributes.Value(telemetry.MetricDimensionOutcome)
					waits[outcome.AsString()] += point.Count
				}
			}
		}
	}
	if len(deferrals) != 1 || deferrals["shutdown"] != 1 {
		t.Fatalf("ack_deferrals_total = %v, want map[shutdown:1]", deferrals)
	}
	if len(waits) != 1 || waits["shutdown"] != 1 {
		t.Fatalf("ack_wait_seconds count = %v, want map[shutdown:1]", waits)
	}
}
