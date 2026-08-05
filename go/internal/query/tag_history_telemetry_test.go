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

// resetTagHistoryInstrumentsForTest rebinds the lazily registered tag-history
// duration histogram and error counter so a test can register them against
// its own meter provider regardless of test ordering (mirrors
// resetSemanticSearchInstrumentsForTest in semantic_search_telemetry_test.go).
func resetTagHistoryInstrumentsForTest() {
	tagHistoryQueryInstrumentsOnce = sync.Once{}
	tagHistoryDuration = nil
	tagHistoryErrors = nil
}

// withTagHistoryMetricReader installs a process-global manual-reader meter
// provider and resets the lazily registered tag-history instruments so the
// test observes only its own datapoints. It is a thin wrapper around the
// shared withPackageMetricReader (metric_reader_test.go), which also backs
// withImageMetricReader in images_telemetry_test.go; see that helper's doc
// comment for why the throwaway-provider install and the previous-provider
// capture order both matter.
func withTagHistoryMetricReader(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	return withPackageMetricReader(t, resetTagHistoryInstrumentsForTest)
}

func collectTagHistoryMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return rm
}

// tagHistoryErrorCounterValue sums
// eshu_dp_query_container_image_tag_history_errors_total datapoints whose
// reason attribute equals want.
func tagHistoryErrorCounterValue(t *testing.T, rm metricdata.ResourceMetrics, want string) int64 {
	t.Helper()
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_query_container_image_tag_history_errors_total" {
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

// tagHistoryDurationOutcomeCount sums the
// eshu_dp_query_container_image_tag_history_duration_seconds histogram
// datapoint counts whose outcome attribute equals want.
func tagHistoryDurationOutcomeCount(t *testing.T, rm metricdata.ResourceMetrics, want string) uint64 {
	t.Helper()
	var total uint64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_query_container_image_tag_history_duration_seconds" {
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

// TestTagHistoryHandlerNilBackendRecordsBackendUnavailableOutcome pins the
// outcome="backend_unavailable" metric-label contract on the h.Neo4j == nil
// guard branch (tag_history.go:145-147): the handler never reaches
// h.Neo4j.Run at all, so the fake reader is never wired and lastCypher stays
// empty. A future refactor that reverts this branch to a generic outcome
// (e.g. "error") must fail this test.
func TestTagHistoryHandlerNilBackendRecordsBackendUnavailableOutcome(t *testing.T) {
	reader := withTagHistoryMetricReader(t)

	handler := &TagHistoryHandler{Neo4j: nil, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTagHistoryRequest("/api/v0/images/tag-history?repository_id=oci-registry://ghcr.io/eshu-hq/demo&tag=1.0.0"))

	if got, want := w.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	rm := collectTagHistoryMetrics(t, reader)
	if got, want := tagHistoryErrorCounterValue(t, rm, "backend_unavailable"), int64(1); got != want {
		t.Fatalf("errors counter reason=backend_unavailable = %d, want %d", got, want)
	}
	if got, want := tagHistoryDurationOutcomeCount(t, rm, "backend_unavailable"), uint64(1); got != want {
		t.Fatalf("duration histogram outcome=backend_unavailable count = %d, want %d", got, want)
	}
}

// TestTagHistoryHandlerGraphReadErrorRecordsBackendUnavailableOutcome pins
// the outcome="backend_unavailable" metric-label contract on the
// WriteGraphReadError guard branch (tag_history.go:175-177), distinct from
// the nil-backend branch above: this one actually invokes h.Neo4j.Run (a
// configured reader) and only trips because that call returned
// ErrGraphUnavailable. Asserting fakeReader.lastCypher is non-empty proves
// the graph-read-error branch, not the nil-backend branch, was exercised.
func TestTagHistoryHandlerGraphReadErrorRecordsBackendUnavailableOutcome(t *testing.T) {
	reader := withTagHistoryMetricReader(t)

	fakeReader := &fakeTagHistoryGraphReader{err: ErrGraphUnavailable}
	handler := &TagHistoryHandler{Neo4j: fakeReader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTagHistoryRequest("/api/v0/images/tag-history?repository_id=oci-registry://ghcr.io/eshu-hq/demo&tag=1.0.0"))

	if got, want := w.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if fakeReader.lastCypher == "" {
		t.Fatalf("h.Neo4j.Run was never called; test did not exercise the graph-read-error guard branch")
	}

	rm := collectTagHistoryMetrics(t, reader)
	if got, want := tagHistoryErrorCounterValue(t, rm, "backend_unavailable"), int64(1); got != want {
		t.Fatalf("errors counter reason=backend_unavailable = %d, want %d", got, want)
	}
	if got, want := tagHistoryDurationOutcomeCount(t, rm, "backend_unavailable"), uint64(1); got != want {
		t.Fatalf("duration histogram outcome=backend_unavailable count = %d, want %d", got, want)
	}
}
