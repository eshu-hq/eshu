// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// manifestDependencyReadSample is one histogram data point of a per-read
// manifest dependency signal, keyed by its read label.
type manifestDependencyReadSample struct {
	count uint64
	sum   float64
}

// collectManifestDependencyRead returns the data points of the named per-read
// histogram by read label.
func collectManifestDependencyRead(t *testing.T, reader sdkmetric.Reader, name string) map[string]manifestDependencyReadSample {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	samples := map[string]manifestDependencyReadSample{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			switch data := m.Data.(type) {
			case metricdata.Histogram[float64]:
				for _, dp := range data.DataPoints {
					read, _ := dp.Attributes.Value(telemetry.MetricDimensionRead)
					samples[read.AsString()] = manifestDependencyReadSample{count: dp.Count, sum: dp.Sum}
				}
			case metricdata.Histogram[int64]:
				for _, dp := range data.DataPoints {
					read, _ := dp.Attributes.Value(telemetry.MetricDimensionRead)
					samples[read.AsString()] = manifestDependencyReadSample{count: dp.Count, sum: float64(dp.Sum)}
				}
			default:
				t.Fatalf("%s is %T, want a histogram", m.Name, m.Data)
			}
		}
	}
	return samples
}

// TestListActivePackageManifestDependencyFactsRecordsPagesAndWallTime proves
// the #9586 operator signal: one sample per whole read with the page count and
// the wall time, labeled by which query text ran.
func TestListActivePackageManifestDependencyFactsRecordsPagesAndWallTime(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	fullPage := make([][]any, 0, listFactsByKindPageSize)
	for i := range listFactsByKindPageSize {
		fullPage = append(fullPage, manifestDependencyFakeRow(i))
	}
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: fullPage},
		{rows: [][]any{manifestDependencyFakeRow(listFactsByKindPageSize)}},
		{},
	}}
	store := NewFactStore(db).WithInstruments(instruments)
	if _, err := store.ListActivePackageManifestDependencyFacts(context.Background(), []string{"npm"}, []string{"left-pad"}); err != nil {
		t.Fatalf("plain read: %v", err)
	}
	if _, err := store.ListActivePackageManifestDependencyFacts(context.Background(), []string{"maven"}, []string{"g:a"}); err != nil {
		t.Fatalf("maven read: %v", err)
	}

	pages := collectManifestDependencyRead(t, reader, "eshu_dp_package_manifest_dependency_read_pages")
	if got, want := pages["plain"], (manifestDependencyReadSample{count: 1, sum: 2}); got != want {
		t.Fatalf("plain pages = %+v, want %+v (all: %+v)", got, want, pages)
	}
	if got, want := pages["maven_family"], (manifestDependencyReadSample{count: 1, sum: 1}); got != want {
		t.Fatalf("maven_family pages = %+v, want %+v (all: %+v)", got, want, pages)
	}
	seconds := collectManifestDependencyRead(t, reader, "eshu_dp_package_manifest_dependency_read_seconds")
	for _, read := range []string{"plain", "maven_family"} {
		if got := seconds[read]; got.count != 1 {
			t.Fatalf("%s seconds = %+v, want one sample (all: %+v)", read, got, seconds)
		}
	}
}

// TestListActivePackageManifestDependencyFactsSkipsSignalOnError keeps a
// failed read out of the wall-time histogram, so its tail reflects completed
// reads only; the error itself reaches the reducer's failure path.
func TestListActivePackageManifestDependencyFactsSkipsSignalOnError(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{err: context.DeadlineExceeded}}}
	if _, err := NewFactStore(db).WithInstruments(instruments).ListActivePackageManifestDependencyFacts(
		context.Background(), []string{"npm"}, []string{"left-pad"},
	); err == nil {
		t.Fatal("read error = nil, want the query error")
	}
	if got := collectManifestDependencyRead(t, reader, "eshu_dp_package_manifest_dependency_read_seconds"); len(got) != 0 {
		t.Fatalf("failed read recorded wall time: %+v", got)
	}
}
