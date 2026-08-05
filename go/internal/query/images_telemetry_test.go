// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

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
// test observes only its own datapoints. It is a thin wrapper around the
// shared withPackageMetricReader (metric_reader_test.go), which also backs
// withTagHistoryMetricReader in tag_history_telemetry_test.go; see that
// helper's doc comment for why the throwaway-provider install and the
// previous-provider capture order both matter.
func withImageMetricReader(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	return withPackageMetricReader(t, resetImageInstrumentsForTest)
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
// guard branch in listImages (images.go, the sole nil-backend check in the
// file), and — more importantly — proves the lazily registered image-list
// instruments actually observe datapoints against a meter provider installed
// by the test rather than a stale one captured earlier in the process.
// initImageQueryInstruments (images_telemetry.go) fetches its meter from the
// current global provider inside a sync.Once, so a meter resolved before any
// provider is installed would bind permanently to whichever provider first
// calls otel.SetMeterProvider in the process — see that function's doc
// comment for the OTel global-proxy mechanics. Because
// withImageMetricReader (via the shared withPackageMetricReader in
// metric_reader_test.go) always burns that delegate-once on a throwaway
// provider before installing this test's own reader, a handler that wrongly
// cached its meter would fail this assertion even running this single test
// alone at -count=1 — the throwaway install is itself the first
// SetMeterProvider call in that run, so the bug no longer needs -count=2 or
// another telemetry test file running first to reproduce.
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

// TestImageHandlerGraphReadErrorRecordsBackendUnavailableOutcome pins the
// outcome="backend_unavailable" metric-label contract on the
// WriteGraphReadError guard branch in listImages (images.go, the sole call of
// that helper in the file), distinct from the nil-backend branch above: this
// one actually invokes h.Neo4j.Run (a configured reader) and only trips
// because that call returned ErrGraphUnavailable. Before this test, that
// branch returned without recording either instrument, so a real graph outage
// or read-deadline on GET /api/v0/images produced a correct 503/504 response
// but zero datapoints on eshu_dp_query_image_list_duration_seconds and
// eshu_dp_query_image_list_errors_total. Asserting fakeReader.lastCypher is
// non-empty proves the graph-read-error branch, not the nil-backend branch,
// was exercised.
func TestImageHandlerGraphReadErrorRecordsBackendUnavailableOutcome(t *testing.T) {
	reader := withImageMetricReader(t)

	fakeReader := &fakeImageGraphReader{err: ErrGraphUnavailable}
	handler := &ImageHandler{Neo4j: fakeReader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newImageRequest("/api/v0/images?limit=5"))

	if got, want := w.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if fakeReader.lastCypher == "" {
		t.Fatalf("h.Neo4j.Run was never called; test did not exercise the graph-read-error guard branch")
	}

	rm := collectImageMetrics(t, reader)
	if got, want := imageErrorCounterValue(t, rm, "backend_unavailable"), int64(1); got != want {
		t.Fatalf("errors counter reason=backend_unavailable = %d, want %d", got, want)
	}
	if got, want := imageDurationOutcomeCount(t, rm, "backend_unavailable"), uint64(1); got != want {
		t.Fatalf("duration histogram outcome=backend_unavailable count = %d, want %d", got, want)
	}
}
