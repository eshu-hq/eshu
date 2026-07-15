// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRelationshipBreakdownPermitMetricsTrackCancellationReleaseAndReuse(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("meter provider shutdown: %v", err)
		}
	})
	instruments, err := telemetry.NewInstruments(provider.Meter("relationship-breakdown-test"))
	if err != nil {
		t.Fatalf("telemetry.NewInstruments() error = %v", err)
	}
	handler := &InfraHandler{Instruments: instruments}

	releases := make([]func(), 0, relationshipBreakdownMaxConcurrency)
	for range relationshipBreakdownMaxConcurrency {
		release, acquireErr := handler.acquireRelationshipBreakdownSlot(context.Background())
		if acquireErr != nil {
			t.Fatalf("acquireRelationshipBreakdownSlot() error = %v", acquireErr)
		}
		releases = append(releases, release)
	}
	assertRelationshipBreakdownMetrics(t, reader, relationshipBreakdownMaxConcurrency, 0, 4)

	canceledCtx, cancel := context.WithCancel(context.Background())
	canceled := make(chan error, 1)
	go func() {
		_, acquireErr := handler.acquireRelationshipBreakdownSlot(canceledCtx)
		canceled <- acquireErr
	}()

	type acquireResult struct {
		release func()
		err     error
	}
	reused := make(chan acquireResult, 1)
	go func() {
		release, acquireErr := handler.acquireRelationshipBreakdownSlot(context.Background())
		reused <- acquireResult{release: release, err: acquireErr}
	}()

	waitForRelationshipBreakdownState(t, reader, 2, relationshipBreakdownMaxConcurrency)
	cancel()
	select {
	case acquireErr := <-canceled:
		if !errors.Is(acquireErr, context.Canceled) {
			t.Fatalf("canceled acquire error = %v, want context.Canceled", acquireErr)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled permit acquisition did not return")
	}
	assertRelationshipBreakdownMetrics(t, reader, relationshipBreakdownMaxConcurrency+1, 1, 4)

	releases[0]()
	select {
	case result := <-reused:
		if result.err != nil {
			t.Fatalf("reused permit acquisition error = %v", result.err)
		}
		result.release()
	case <-time.After(time.Second):
		t.Fatal("released relationship-breakdown permit was not reused")
	}
	for _, release := range releases[1:] {
		release()
	}
	assertRelationshipBreakdownMetrics(t, reader, relationshipBreakdownMaxConcurrency+2, 0, 0)
}

func waitForRelationshipBreakdownState(t *testing.T, reader *sdkmetric.ManualReader, wantQueued, wantInFlight int64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		rm := collectRelationshipBreakdownMetrics(t, reader)
		if relationshipBreakdownStateValue(t, rm, "eshu_dp_relationship_breakdown_queued") == wantQueued &&
			relationshipBreakdownStateValue(t, rm, "eshu_dp_relationship_breakdown_in_flight") == wantInFlight {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("relationship-breakdown state did not reach queued=%d in_flight=%d", wantQueued, wantInFlight)
}

func assertRelationshipBreakdownMetrics(t *testing.T, reader *sdkmetric.ManualReader, wantWaitCount uint64, wantQueued, wantInFlight int64) {
	t.Helper()
	rm := collectRelationshipBreakdownMetrics(t, reader)
	if got := relationshipBreakdownWaitCount(t, rm); got != wantWaitCount {
		t.Fatalf("permit wait histogram count = %d, want %d", got, wantWaitCount)
	}
	if got := relationshipBreakdownStateValue(t, rm, "eshu_dp_relationship_breakdown_queued"); got != wantQueued {
		t.Fatalf("queued state = %d, want %d", got, wantQueued)
	}
	if got := relationshipBreakdownStateValue(t, rm, "eshu_dp_relationship_breakdown_in_flight"); got != wantInFlight {
		t.Fatalf("in-flight state = %d, want %d", got, wantInFlight)
	}
}

func collectRelationshipBreakdownMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect relationship-breakdown metrics: %v", err)
	}
	return rm
}

func relationshipBreakdownWaitCount(t *testing.T, rm metricdata.ResourceMetrics) uint64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, metricRecord := range scope.Metrics {
			if metricRecord.Name != "eshu_dp_relationship_breakdown_permit_wait_seconds" {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("permit wait metric data = %T, want Histogram[float64]", metricRecord.Data)
			}
			if len(histogram.DataPoints) != 1 {
				t.Fatalf("permit wait datapoints = %d, want 1 label-free datapoint", len(histogram.DataPoints))
			}
			if attrs := histogram.DataPoints[0].Attributes.Len(); attrs != 0 {
				t.Fatalf("permit wait metric attributes = %d, want 0", attrs)
			}
			return histogram.DataPoints[0].Count
		}
	}
	t.Fatal("permit wait histogram not found")
	return 0
}

func relationshipBreakdownStateValue(t *testing.T, rm metricdata.ResourceMetrics, name string) int64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, metricRecord := range scope.Metrics {
			if metricRecord.Name != name {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want Sum[int64]", name, metricRecord.Data)
			}
			if sum.IsMonotonic {
				t.Fatalf("%s is monotonic, want current-state up/down sum", name)
			}
			if len(sum.DataPoints) != 1 {
				t.Fatalf("%s datapoints = %d, want 1 label-free datapoint", name, len(sum.DataPoints))
			}
			if attrs := sum.DataPoints[0].Attributes.Len(); attrs != 0 {
				t.Fatalf("%s attributes = %d, want 0", name, attrs)
			}
			return sum.DataPoints[0].Value
		}
	}
	t.Fatalf("metric %s not found", name)
	return 0
}
