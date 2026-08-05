// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// resetImageInstrumentsForTest rebinds the lazily registered image-list
// duration histogram and error counter so a test can register them against
// its own meter provider regardless of test ordering (mirrors
// resetTagHistoryInstrumentsForTest in tag_history_telemetry_test.go).
func resetImageInstrumentsForTest() {
	imageQueryInstrumentsOnce = sync.Once{}
	imageListDuration = nil
	imageListErrors = nil
}

// withImageMetricReader installs a process-global manual-reader meter
// provider and resets the lazily registered image-list instruments so the
// test observes only its own datapoints (mirrors withTagHistoryMetricReader).
//
// Before installing the reader's own provider, it burns the OTel global
// delegate-once on a throwaway provider. otel.Meter() inside a sync.Once
// resolves its meter from whichever provider is current at the moment that
// Once fires; if some other test file's sync.Once fires first in this
// process and its own SetMeterProvider call is the very first one ever made,
// the OTel global proxy binds that meter's delegate permanently to it (see
// initImageQueryInstruments' doc comment). Without this throwaway call, this
// test would only be defended by being first among the package's test files
// to install a provider — an accident of alphabetical file ordering
// (images_telemetry_test.go sorts before request_metrics_, semantic_search_,
// and tag_history_ telemetry test files), not a property of the code under
// test. Burning the delegate-once here first guarantees any
// package-var-cached meter is bound to the throwaway, never to this test's
// reader, so a buggy handler that caches its meter at init time fails
// deterministically instead of passing by file-order luck.
func withImageMetricReader(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider())
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	resetImageInstrumentsForTest()
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		resetImageInstrumentsForTest()
		_ = provider.Shutdown(context.Background())
	})
	return reader
}

func collectImageMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return rm
}

// imageErrorCounterValue sums eshu_dp_query_image_list_errors_total
// datapoints whose reason attribute equals want.
func imageErrorCounterValue(t *testing.T, rm metricdata.ResourceMetrics, want string) int64 {
	t.Helper()
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_query_image_list_errors_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %q data = %T, want Sum[int64]", m.Name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				got, ok := dp.Attributes.Value(attribute.Key("reason"))
				if ok && got.AsString() == want {
					total += dp.Value
				}
			}
		}
	}
	return total
}

// imageDurationOutcomeCount sums the eshu_dp_query_image_list_duration_seconds
// histogram datapoint counts whose outcome attribute equals want.
func imageDurationOutcomeCount(t *testing.T, rm metricdata.ResourceMetrics, want string) uint64 {
	t.Helper()
	var total uint64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_query_image_list_duration_seconds" {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %q data = %T, want Histogram[float64]", m.Name, m.Data)
			}
			for _, dp := range hist.DataPoints {
				got, ok := dp.Attributes.Value(attribute.Key("outcome"))
				if ok && got.AsString() == want {
					total += dp.Count
				}
			}
		}
	}
	return total
}

// TestImageHandlerNilBackendRecordsBackendUnavailableOutcome pins the
// outcome="backend_unavailable" metric-label contract on the h.Neo4j == nil
// guard branch (images.go:122-136), and — more importantly — proves the
// lazily registered image-list instruments actually observe datapoints
// against a meter provider installed by the test rather than a stale one
// captured at package-init time. Before the fix, imageQueryMeter was a
// package-level var initialized via otel.Meter(...) once at package load,
// before any test installs a meter provider; the OTel global proxy binds
// such a meter's delegate permanently on the first ever
// otel.SetMeterProvider call in the process, so a later test installing its
// own reader would silently record onto an earlier (possibly already
// shut-down) provider instead of its own manual reader. Running this test
// twice in the same process (-count=2) or alongside the tag-history tests
// (which also install their own meter provider) reproduces that: on the
// second SetMeterProvider install, imageQueryMeter still resolves against
// the first provider it ever saw, so this test's manual reader observes zero
// datapoints and the assertions below fail.
func TestImageHandlerNilBackendRecordsBackendUnavailableOutcome(t *testing.T) {
	reader := withImageMetricReader(t)

	handler := &ImageHandler{Neo4j: nil, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newImageRequest("/api/v0/images?limit=5"))

	if got, want := w.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	rm := collectImageMetrics(t, reader)
	if got, want := imageErrorCounterValue(t, rm, "backend_unavailable"), int64(1); got != want {
		t.Fatalf("errors counter reason=backend_unavailable = %d, want %d", got, want)
	}
	if got, want := imageDurationOutcomeCount(t, rm, "backend_unavailable"), uint64(1); got != want {
		t.Fatalf("duration histogram outcome=backend_unavailable count = %d, want %d", got, want)
	}
}
