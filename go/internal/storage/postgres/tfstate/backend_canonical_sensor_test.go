// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfstate

import (
	"context"
	"errors"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// sensorInstruments builds Instruments wired to a manual reader so emission
// tests can read back recorded samples without touching global state.
func sensorInstruments(t *testing.T, reader *sdkmetric.ManualReader) *telemetry.Instruments {
	t.Helper()
	inst, err := telemetry.NewInstruments(
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("terraform-backend-canonical-sensor-test"),
	)
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return inst
}

// sensorPoints collects every datapoint of the canonical-read histogram.
func sensorPoints(t *testing.T, ctx context.Context, reader *sdkmetric.ManualReader) []metricdata.HistogramDataPoint[float64] {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var out []metricdata.HistogramDataPoint[float64]
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_terraform_backend_canonical_read_duration_seconds" {
				continue
			}
			data, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s data = %T, want histogram", m.Name, m.Data)
			}
			out = append(out, data.DataPoints...)
		}
	}
	return out
}

// sensorAttr returns the string value of key on the datapoint, or "".
func sensorAttr(point metricdata.HistogramDataPoint[float64], key string) string {
	got := ""
	iter := point.Attributes.Iter()
	for iter.Next() {
		if kv := iter.Attribute(); string(kv.Key) == key {
			got = kv.Value.AsString()
		}
	}
	return got
}

func TestTerraformBackendCanonicalReadEmitsSuccessSample(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	query := PostgresTerraformBackendQuery{
		DB:          &fakeExecQueryer{queryResponses: []queueFakeRows{{}}},
		Instruments: sensorInstruments(t, reader),
	}

	if _, err := query.ListTerraformBackendsByLocator(ctx, "s3", "any-hash"); err != nil {
		t.Fatalf("ListTerraformBackendsByLocator() error = %v, want nil", err)
	}

	points := sensorPoints(t, ctx, reader)
	if len(points) != 1 {
		t.Fatalf("datapoints = %d, want exactly one sample per call", len(points))
	}
	point := points[0]
	if got := sensorAttr(point, "read"); got != "canonical" {
		t.Fatalf("read = %q, want canonical", got)
	}
	if got := sensorAttr(point, "outcome"); got != "success" {
		t.Fatalf("outcome = %q, want success", got)
	}
	if point.Count != 1 {
		t.Fatalf("count = %d, want 1", point.Count)
	}
	if point.Sum <= 0 {
		t.Fatalf("sum = %v, want a positive duration", point.Sum)
	}
}

func TestTerraformBackendCanonicalReadEmitsErrorSample(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	query := PostgresTerraformBackendQuery{
		DB:          &fakeExecQueryer{queryResponses: []queueFakeRows{{err: errors.New("boom")}}},
		Instruments: sensorInstruments(t, reader),
	}

	if _, err := query.ListTerraformBackendsByLocator(ctx, "s3", "any-hash"); err == nil {
		t.Fatal("ListTerraformBackendsByLocator() error = nil, want query failure")
	}

	points := sensorPoints(t, ctx, reader)
	if len(points) != 1 {
		t.Fatalf("datapoints = %d, want exactly one sample per call", len(points))
	}
	if got := sensorAttr(points[0], "read"); got != "canonical" {
		t.Fatalf("read = %q, want canonical", got)
	}
	if got := sensorAttr(points[0], "outcome"); got != "error" {
		t.Fatalf("outcome = %q, want error", got)
	}
}

func TestTerraformBackendFilteredReadEmitsFilteredSample(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	store := TerraformStateBackendFactReader{
		DB:          &fakeExecQueryer{queryResponses: []queueFakeRows{{}, {}}},
		Instruments: sensorInstruments(t, reader),
	}

	_, err := store.TerraformStateCandidates(ctx, terraformstate.DiscoveryQuery{
		BackendFilters: []terraformstate.DiscoveryBackendFilter{{BackendKind: terraformstate.BackendS3}},
	})
	if err != nil {
		t.Fatalf("TerraformStateCandidates() error = %v, want nil", err)
	}

	points := sensorPoints(t, ctx, reader)
	if len(points) != 1 {
		t.Fatalf("datapoints = %d, want exactly one sample per call", len(points))
	}
	if got := sensorAttr(points[0], "read"); got != "filtered" {
		t.Fatalf("read = %q, want filtered", got)
	}
	if got := sensorAttr(points[0], "outcome"); got != "success" {
		t.Fatalf("outcome = %q, want success", got)
	}
}

// TestTerraformBackendCanonicalReadLabelsStayBounded rejects unbounded
// operator identifiers on the sensor: repo, scope, generation, backend-kind,
// and locator values belong in logs and span attributes, never in metric
// labels.
func TestTerraformBackendCanonicalReadLabelsStayBounded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	query := PostgresTerraformBackendQuery{
		DB:          &fakeExecQueryer{queryResponses: []queueFakeRows{{}}},
		Instruments: sensorInstruments(t, reader),
	}

	if _, err := query.ListTerraformBackendsByLocator(ctx, "s3", "any-hash"); err != nil {
		t.Fatalf("ListTerraformBackendsByLocator() error = %v, want nil", err)
	}

	points := sensorPoints(t, ctx, reader)
	if len(points) != 1 {
		t.Fatalf("datapoints = %d, want 1", len(points))
	}
	for _, key := range []string{"repo_id", "scope_id", "generation_id", "backend_kind", "locator_hash"} {
		if got := sensorAttr(points[0], key); got != "" {
			t.Fatalf("label %q = %q, want absent (unbounded cardinality)", key, got)
		}
	}
}
