// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeMetricsSource struct {
	points []MetricPoint
	err    error
}

func (f fakeMetricsSource) RangeQuery(_ context.Context, _ MetricsRangeQuery) ([]MetricPoint, error) {
	return f.points, f.err
}

func requestTimeSeries(t *testing.T, handler *MetricsHandler, target string, envelope bool) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if envelope {
		req.Header.Set("Accept", "application/eshu.envelope+json")
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestTimeSeriesRejectsUnknownMetric(t *testing.T) {
	t.Parallel()
	handler := &MetricsHandler{}
	for _, target := range []string{
		"/api/v0/metrics/timeseries",
		"/api/v0/metrics/timeseries?metric=not_a_metric",
	} {
		w := requestTimeSeries(t, handler, target, false)
		if got, want := w.Code, http.StatusBadRequest; got != want {
			t.Fatalf("%s status = %d, want %d", target, got, want)
		}
	}
}

func TestTimeSeriesEmptyPointsWhenNoSourceConfigured(t *testing.T) {
	t.Parallel()
	handler := &MetricsHandler{} // Source nil
	w := requestTimeSeries(t, handler, "/api/v0/metrics/timeseries?metric=queue_depth", true)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var env ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := env.Data.(map[string]any)
	if pts := data["points"].([]any); len(pts) != 0 {
		t.Fatalf("points = %d, want 0 when no source configured", len(pts))
	}
	if env.Truth == nil || env.Truth.Freshness.State != FreshnessUnavailable {
		t.Fatalf("freshness = %#v, want unavailable", env.Truth)
	}
	if env.Truth.Freshness.Cause != FreshnessCauseMissingCollectorCompletion {
		t.Fatalf("cause = %q, want missing_collector_completion", env.Truth.Freshness.Cause)
	}
	if env.Truth.Freshness.NextCheck == nil {
		t.Fatalf("expected a freshness next check on the unavailable series")
	}
}

func TestTimeSeriesReturnsSourcePoints(t *testing.T) {
	t.Parallel()
	handler := &MetricsHandler{Source: fakeMetricsSource{points: []MetricPoint{{T: "2026-06-01T00:00:00Z", V: 12}}}}
	w := requestTimeSeries(t, handler, "/api/v0/metrics/timeseries?metric=ingest_rate&window=6h&step=15m", true)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var env ResponseEnvelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	data := env.Data.(map[string]any)
	if data["metric"] != "ingest_rate" {
		t.Fatalf("metric = %#v", data["metric"])
	}
	if pts := data["points"].([]any); len(pts) != 1 {
		t.Fatalf("points = %d, want 1", len(pts))
	}
	if env.Truth.Freshness.State != FreshnessFresh {
		t.Fatalf("freshness = %#v, want fresh", env.Truth.Freshness.State)
	}
	if env.Truth.Freshness.Cause != "" {
		t.Fatalf("fresh series must carry no cause, got %q", env.Truth.Freshness.Cause)
	}
	if env.Truth.Freshness.NextCheck != nil {
		t.Fatalf("fresh series must carry no next check")
	}
}

func TestTimeSeriesEmptyHistoryIsBuildingNotError(t *testing.T) {
	t.Parallel()
	handler := &MetricsHandler{Source: fakeMetricsSource{points: nil}}
	w := requestTimeSeries(t, handler, "/api/v0/metrics/timeseries?metric=graph_nodes", true)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	var env ResponseEnvelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if env.Truth.Freshness.State != FreshnessBuilding {
		t.Fatalf("freshness = %#v, want building for empty history", env.Truth.Freshness.State)
	}
	if env.Truth.Freshness.Cause != FreshnessCauseContentCoverageUnavailable {
		t.Fatalf("cause = %q, want content_coverage_unavailable", env.Truth.Freshness.Cause)
	}
	if env.Truth.Freshness.NextCheck == nil {
		t.Fatalf("expected a freshness next check on the building series")
	}
}

func TestTimeSeriesRejectsInvalidRangeAsBadRequest(t *testing.T) {
	t.Parallel()
	handler := &MetricsHandler{Source: fakeMetricsSource{err: errInvalidMetricsRange}}
	w := requestTimeSeries(t, handler, "/api/v0/metrics/timeseries?metric=queue_depth&window=30d&step=1s", false)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestTimeSeriesCapabilityIsRegistered(t *testing.T) {
	t.Parallel()

	envelope := BuildTruthEnvelope(
		ProfileProduction,
		metricsTimeSeriesCapability,
		TruthBasisSemanticFacts,
		"test",
	)
	if envelope.Capability != metricsTimeSeriesCapability {
		t.Fatalf("capability = %q, want %q", envelope.Capability, metricsTimeSeriesCapability)
	}
	if envelope.Level != TruthLevelDerived {
		t.Fatalf("level = %q, want derived", envelope.Level)
	}
}
