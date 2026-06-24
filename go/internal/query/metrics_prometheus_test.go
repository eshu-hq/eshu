// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPrometheusMetricsTimeSeriesSourceQueriesRangeAPI(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotQuery string
	var gotAuthorization string
	var gotTenant string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("query")
		gotAuthorization = r.Header.Get("Authorization")
		gotTenant = r.Header.Get("X-Scope-OrgID")
		WriteJSON(w, http.StatusOK, map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "matrix",
				"result": []map[string]any{{
					"metric": map[string]string{},
					"values": [][]any{
						{float64(1780272000), "4"},
						{float64(1780273800), "8.5"},
					},
				}},
			},
		})
	}))
	defer server.Close()

	source, err := NewPrometheusMetricsTimeSeriesSource(PrometheusMetricsTimeSeriesConfig{
		BaseURL:    server.URL,
		PathPrefix: "/prometheus",
		Token:      "token-value",
		TenantID:   "tenant-prod",
		Client:     server.Client(),
	})
	if err != nil {
		t.Fatalf("NewPrometheusMetricsTimeSeriesSource() error = %v, want nil", err)
	}

	points, err := source.RangeQuery(context.Background(), MetricsRangeQuery{
		Metric: "ingest_rate",
		Window: "1h",
		Step:   "30m",
	})
	if err != nil {
		t.Fatalf("RangeQuery() error = %v, want nil", err)
	}
	if got, want := gotPath, "/prometheus/api/v1/query_range"; got != want {
		t.Fatalf("request path = %q, want %q", got, want)
	}
	if gotQuery == "" || gotQuery == "ingest_rate" {
		t.Fatalf("PromQL query = %q, want closed mapping from logical metric", gotQuery)
	}
	if got, want := gotAuthorization, "Bearer token-value"; got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
	if got, want := gotTenant, "tenant-prod"; got != want {
		t.Fatalf("X-Scope-OrgID = %q, want %q", got, want)
	}
	if got, want := len(points), 2; got != want {
		t.Fatalf("len(points) = %d, want %d", got, want)
	}
	if got, want := points[0].T, "2026-06-01T00:00:00Z"; got != want {
		t.Fatalf("points[0].T = %q, want %q", got, want)
	}
	if got, want := points[1].V, 8.5; got != want {
		t.Fatalf("points[1].V = %v, want %v", got, want)
	}
}

func TestPrometheusMetricExpressionsCoverSupportedMetrics(t *testing.T) {
	t.Parallel()

	for metric := range metricUnits {
		if _, ok := prometheusMetricExpressions[metric]; !ok {
			t.Fatalf("metric %q has a unit but no PromQL expression", metric)
		}
	}
	for metric := range prometheusMetricExpressions {
		if _, ok := metricUnits[metric]; !ok {
			t.Fatalf("metric %q has a PromQL expression but is not publicly supported", metric)
		}
	}
}

func TestPrometheusMetricsTimeSeriesSourceRejectsUnboundedRanges(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("metrics source should reject unbounded range before HTTP request")
	}))
	defer server.Close()

	source, err := NewPrometheusMetricsTimeSeriesSource(PrometheusMetricsTimeSeriesConfig{
		BaseURL: server.URL,
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatalf("NewPrometheusMetricsTimeSeriesSource() error = %v, want nil", err)
	}
	_, err = source.RangeQuery(context.Background(), MetricsRangeQuery{
		Metric: "queue_depth",
		Window: "30d",
		Step:   "1s",
	})
	if !errors.Is(err, errInvalidMetricsRange) {
		t.Fatalf("RangeQuery() error = %v, want errInvalidMetricsRange", err)
	}
}
