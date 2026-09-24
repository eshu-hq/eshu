// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package metrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

type fakeMetricsSource struct {
	points []Point
	err    error
}

func (f fakeMetricsSource) RangeQuery(_ context.Context, _ RangeQuery) ([]Point, error) {
	return f.points, f.err
}

func requestTimeSeries(t *testing.T, handler *Handler, target string, envelope bool) *httptest.ResponseRecorder {
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
	handler := &Handler{}
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
	handler := &Handler{} // Source nil
	w := requestTimeSeries(t, handler, "/api/v0/metrics/timeseries?metric=queue_depth", true)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var env querycontract.ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := env.Data.(map[string]any)
	if pts := data["points"].([]any); len(pts) != 0 {
		t.Fatalf("points = %d, want 0 when no source configured", len(pts))
	}
	if env.Truth == nil || env.Truth.Freshness.State != querycontract.FreshnessUnavailable {
		t.Fatalf("freshness = %#v, want unavailable", env.Truth)
	}
	if env.Truth.Freshness.Cause != querycontract.FreshnessCauseMissingCollectorCompletion {
		t.Fatalf("cause = %q, want missing_collector_completion", env.Truth.Freshness.Cause)
	}
	if env.Truth.Freshness.NextCheck == nil {
		t.Fatalf("expected a freshness next check on the unavailable series")
	}
}

func TestTimeSeriesReturnsSourcePoints(t *testing.T) {
	t.Parallel()
	handler := &Handler{Source: fakeMetricsSource{points: []Point{{T: "2026-06-01T00:00:00Z", V: 12}}}}
	w := requestTimeSeries(t, handler, "/api/v0/metrics/timeseries?metric=ingest_rate&window=6h&step=15m", true)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var env querycontract.ResponseEnvelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	data := env.Data.(map[string]any)
	if data["metric"] != "ingest_rate" {
		t.Fatalf("metric = %#v", data["metric"])
	}
	if pts := data["points"].([]any); len(pts) != 1 {
		t.Fatalf("points = %d, want 1", len(pts))
	}
	if env.Truth.Freshness.State != querycontract.FreshnessFresh {
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
	handler := &Handler{Source: fakeMetricsSource{points: nil}}
	w := requestTimeSeries(t, handler, "/api/v0/metrics/timeseries?metric=graph_nodes", true)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	var env querycontract.ResponseEnvelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if env.Truth.Freshness.State != querycontract.FreshnessBuilding {
		t.Fatalf("freshness = %#v, want building for empty history", env.Truth.Freshness.State)
	}
	if env.Truth.Freshness.Cause != querycontract.FreshnessCauseContentCoverageUnavailable {
		t.Fatalf("cause = %q, want content_coverage_unavailable", env.Truth.Freshness.Cause)
	}
	if env.Truth.Freshness.NextCheck == nil {
		t.Fatalf("expected a freshness next check on the building series")
	}
}

func TestTimeSeriesRejectsInvalidRangeAsBadRequest(t *testing.T) {
	t.Parallel()
	handler := &Handler{Source: fakeMetricsSource{err: errInvalidMetricsRange}}
	w := requestTimeSeries(t, handler, "/api/v0/metrics/timeseries?metric=queue_depth&window=30d&step=1s", false)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

// TestTimeSeriesSupportIsDerived pins the ceilings Support() declares, which
// capability.go says is the only place this row changes. It reads Support()
// directly, so drift in any ceiling fails here rather than only in root's
// TestCapabilityMatrixMatchesYAMLContract, which proves production registers
// the row against specs/capability-matrix.v1.yaml.
func TestTimeSeriesSupportIsDerived(t *testing.T) {
	t.Parallel()

	support := Support()
	for name, ceiling := range map[string]*querycontract.TruthLevel{
		"LocalLightweightMax":   support.LocalLightweightMax,
		"LocalAuthoritativeMax": support.LocalAuthoritativeMax,
		"LocalFullStackMax":     support.LocalFullStackMax,
		"ProductionMax":         support.ProductionMax,
	} {
		if ceiling == nil {
			t.Errorf("%s = nil, want derived", name)
			continue
		}
		if *ceiling != querycontract.TruthLevelDerived {
			t.Errorf("%s = %q, want derived", name, *ceiling)
		}
	}
	if support.RequiredProfile != "" {
		t.Errorf("RequiredProfile = %q, want none", support.RequiredProfile)
	}
}
