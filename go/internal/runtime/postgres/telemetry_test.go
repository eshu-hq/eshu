// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNewObserverEmitsClosedStageMetricAndSpan(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	spans := tracetest.NewSpanRecorder()
	traces := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	observer, err := NewObserver(provider.Meter("test"), traces.Tracer("test"))
	if err != nil {
		t.Fatalf("NewObserver: %v", err)
	}
	observer.Observe("reader", StageReaderReplay, OutcomeDeadline, 250*time.Millisecond)
	observer.Observe("secret-host", Stage("secret-stage"), Outcome("secret-error"), time.Second)
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	points := histogramPoints(t, rm, MetricReaderStageDuration)
	if len(points) != 2 {
		t.Fatalf("points = %d, want 2", len(points))
	}
	want := map[string]string{"role": "reader", "stage": "reader_replay", "outcome": "deadline"}
	foundExpected, foundUnknown := false, false
	for _, point := range points {
		attrs := map[string]string{}
		for _, attr := range point.Attributes.ToSlice() {
			attrs[string(attr.Key)] = attr.Value.AsString()
		}
		if len(attrs) != 3 || point.Count != 1 {
			t.Fatalf("unexpected reader stage point: %+v", point)
		}
		if attrs["role"] == want["role"] && attrs["stage"] == want["stage"] && attrs["outcome"] == want["outcome"] {
			wantBounds := []float64{0, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
			if !slices.Equal(point.Bounds, wantBounds) {
				t.Fatalf("reader stage seconds buckets = %v, want %v", point.Bounds, wantBounds)
			}
			if len(point.BucketCounts) != len(wantBounds)+1 || point.BucketCounts[6] != 1 {
				t.Fatalf("250ms reader stage bucket counts = %v", point.BucketCounts)
			}
			if point.Sum != 0.25 {
				t.Fatalf("reader replay duration = %f, want 0.25", point.Sum)
			}
			foundExpected = true
			continue
		}
		if attrs["role"] == "unknown" && attrs["stage"] == "unknown" && attrs["outcome"] == "unknown" {
			foundUnknown = true
			continue
		}
		t.Fatalf("unexpected reader stage attributes: %v", attrs)
	}
	if !foundExpected || !foundUnknown {
		t.Fatalf("reader stage points: expected=%t unknown=%t", foundExpected, foundUnknown)
	}
	if got := len(spans.Ended()); got != 2 {
		t.Fatalf("ended spans = %d, want 2", got)
	}
	for _, span := range spans.Ended() {
		if span.Name() != readerAccessSpanName || len(span.Attributes()) != 3 {
			t.Fatalf("span name %q, attributes %v", span.Name(), span.Attributes())
		}
		for _, attr := range span.Attributes() {
			if attr.Key != attribute.Key("role") && attr.Key != attribute.Key("stage") && attr.Key != attribute.Key("outcome") {
				t.Fatalf("unexpected span label %s", attr.Key)
			}
		}
	}
	if _, err := NewObserver(nil, traces.Tracer("test")); err == nil {
		t.Fatal("nil meter accepted")
	}
	if _, err := NewObserver(provider.Meter("test"), nil); err == nil {
		t.Fatal("nil tracer accepted")
	}
	var absent *otelObserver
	absent.Observe("reader", StageReaderReplay, OutcomeOK, 0)
}

func TestRegisterPoolMetricsReportsBothRolesAndUnregisters(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	writerDB, err := sql.Open("pgx", "postgres://localhost/unused")
	if err != nil {
		t.Fatal(err)
	}
	defer writerDB.Close()
	readerDB, err := sql.Open("pgx", "postgres://localhost/unused")
	if err != nil {
		t.Fatal(err)
	}
	defer readerDB.Close()
	writerDB.SetMaxOpenConns(7)
	readerDB.SetMaxOpenConns(5)
	access := &Access{writer: writerDB, reader: readerDB}
	registration, err := RegisterPoolMetrics(provider.Meter("test"), access)
	if err != nil {
		t.Fatalf("RegisterPoolMetrics: %v", err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	wantMax := map[string]int64{"writer": 7, "reader": 5}
	wantStates := map[string]bool{"writer/open": false, "writer/in_use": false, "reader/open": false, "reader/in_use": false}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != MetricReaderPoolConnections {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			if !ok {
				t.Fatalf("connections data = %T", m.Data)
			}
			for _, point := range gauge.DataPoints {
				attrs := point.Attributes.ToSlice()
				if len(attrs) != 2 {
					t.Fatalf("pool attrs = %v", attrs)
				}
				var role, state string
				for _, attr := range attrs {
					switch attr.Key {
					case attribute.Key("role"):
						role = attr.Value.AsString()
					case attribute.Key("state"):
						state = attr.Value.AsString()
					default:
						t.Fatalf("unexpected pool label %s", attr.Key)
					}
				}
				if state == "max_open" {
					if point.Value != wantMax[role] {
						t.Fatalf("%s max = %d, want %d", role, point.Value, wantMax[role])
					}
					delete(wantMax, role)
				} else {
					wantStates[role+"/"+state] = true
				}
			}
		}
	}
	if len(wantMax) != 0 {
		t.Fatalf("missing pool max points: %v", wantMax)
	}
	for key, seen := range wantStates {
		if !seen {
			t.Fatalf("missing pool state %s", key)
		}
	}
	assertPoolWaitMetric(t, rm, MetricReaderPoolWaits, false)
	assertPoolWaitMetric(t, rm, MetricReaderPoolWaitTime, true)
	if err := access.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect after Close: %v", err)
	}
	if err := registration.Unregister(); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	if _, err := RegisterPoolMetrics(nil, access); err == nil {
		t.Fatal("nil meter accepted")
	}
	if _, err := RegisterPoolMetrics(provider.Meter("test"), nil); err == nil {
		t.Fatal("nil access accepted")
	}
	if _, err := RegisterPoolMetrics(provider.Meter("test"), &Access{}); err == nil {
		t.Fatal("access with no pools accepted")
	}
}

func histogramPoints(t *testing.T, rm metricdata.ResourceMetrics, name string) []metricdata.HistogramDataPoint[float64] {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == name {
				histogram, ok := m.Data.(metricdata.Histogram[float64])
				if !ok {
					t.Fatalf("%s data = %T", name, m.Data)
				}
				return histogram.DataPoints
			}
		}
	}
	t.Fatalf("metric %s missing", name)
	return nil
}

func assertPoolWaitMetric(t *testing.T, rm metricdata.ResourceMetrics, name string, floating bool) {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			roles := map[string]bool{"writer": false, "reader": false}
			if floating {
				sum, ok := m.Data.(metricdata.Sum[float64])
				if !ok {
					t.Fatalf("%s data = %T", name, m.Data)
				}
				for _, point := range sum.DataPoints {
					if point.Value != 0 {
						t.Fatalf("%s = %f, want 0", name, point.Value)
					}
					for _, attr := range point.Attributes.ToSlice() {
						if attr.Key != attribute.Key("role") {
							t.Fatalf("%s unexpected label %s", name, attr.Key)
						}
						roles[attr.Value.AsString()] = true
					}
				}
			} else {
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok {
					t.Fatalf("%s data = %T", name, m.Data)
				}
				for _, point := range sum.DataPoints {
					if point.Value != 0 {
						t.Fatalf("%s = %d, want 0", name, point.Value)
					}
					for _, attr := range point.Attributes.ToSlice() {
						if attr.Key != attribute.Key("role") {
							t.Fatalf("%s unexpected label %s", name, attr.Key)
						}
						roles[attr.Value.AsString()] = true
					}
				}
			}
			for role, seen := range roles {
				if !seen {
					t.Fatalf("%s missing %s", name, role)
				}
			}
			return
		}
	}
	t.Fatalf("metric %s missing", name)
}
