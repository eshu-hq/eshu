// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

const (
	timeSeriesDefaultWindow     = "24h"
	timeSeriesDefaultStep       = "30m"
	metricsTimeSeriesCapability = "platform_metrics.timeseries"
)

var errInvalidMetricsRange = errors.New("invalid metrics time-series range")

// MetricPoint is one timestamped sample in a metric series.
type MetricPoint struct {
	T string  `json:"t"`
	V float64 `json:"v"`
}

// MetricsRangeQuery bounds a single time-series read.
type MetricsRangeQuery struct {
	Metric string
	Window string
	Step   string
}

// MetricsTimeSeriesSource reads historical metric series, typically backed by the
// Prometheus/Mimir collector. It is defined here because the query layer is its
// only consumer; an implementation is wired in at the API command. When no source
// is configured the endpoint returns empty points rather than failing.
type MetricsTimeSeriesSource interface {
	RangeQuery(ctx context.Context, query MetricsRangeQuery) ([]MetricPoint, error)
}

// metricUnits maps each supported logical metric to its display unit. The key set
// is also the allow-list of metrics this endpoint serves.
var metricUnits = map[string]string{
	"ingest_rate":  "facts/min",
	"queue_depth":  "items",
	"dead_letters": "items",
	"graph_nodes":  "count",
	"graph_edges":  "count",
	"query_p50":    "ms",
	"query_p95":    "ms",
	"query_p99":    "ms",
}

// MetricsHandler serves historical time-series for dashboard and operations
// charts. Source may be nil when no Prometheus/Mimir collector is configured, in
// which case the endpoint returns empty points with unavailable freshness.
type MetricsHandler struct {
	Source  MetricsTimeSeriesSource
	Profile QueryProfile
}

func (h *MetricsHandler) profile() QueryProfile {
	if h == nil {
		return ProfileProduction
	}
	return NormalizeQueryProfile(string(h.Profile))
}

// Mount registers the metrics time-series route.
func (h *MetricsHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/metrics/timeseries", h.getTimeSeries)
}

// getTimeSeries returns an ordered point series for one metric over a window.
// GET /api/v0/metrics/timeseries?metric=&window=&step=
func (h *MetricsHandler) getTimeSeries(w http.ResponseWriter, r *http.Request) {
	metric := QueryParam(r, "metric")
	if metric == "" {
		WriteError(w, http.StatusBadRequest, "metric is required")
		return
	}
	unit, ok := metricUnits[metric]
	if !ok {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("unsupported metric %q", metric))
		return
	}

	query := MetricsRangeQuery{
		Metric: metric,
		Window: queryParamOrDefault(r, "window", timeSeriesDefaultWindow),
		Step:   queryParamOrDefault(r, "step", timeSeriesDefaultStep),
	}

	if h == nil || h.Source == nil {
		// No metrics collector configured: empty points, not an error.
		h.writeSeries(w, r, query, unit, []MetricPoint{}, FreshnessUnavailable,
			"no metrics time-series source configured; configure the Prometheus/Mimir collector to enable trends")
		return
	}

	points, err := h.Source.RangeQuery(r.Context(), query)
	if err != nil {
		if errors.Is(err, errInvalidMetricsRange) {
			WriteError(w, http.StatusBadRequest, fmt.Sprintf("invalid metrics range: %v", err))
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("metrics query failed: %v", err))
		return
	}
	if points == nil {
		points = []MetricPoint{}
	}
	freshness := FreshnessFresh
	reason := "resolved from the metrics time-series source"
	if len(points) == 0 {
		// History not yet available for this metric — building, not an error.
		freshness = FreshnessBuilding
		reason = "metric has no history yet"
	}
	h.writeSeries(w, r, query, unit, points, freshness, reason)
}

func (h *MetricsHandler) writeSeries(
	w http.ResponseWriter,
	r *http.Request,
	query MetricsRangeQuery,
	unit string,
	points []MetricPoint,
	freshness FreshnessState,
	reason string,
) {
	level := TruthLevelDerived
	if freshness == FreshnessUnavailable {
		level = TruthLevelFallback
	}
	truth := BuildTruthEnvelope(h.profile(), metricsTimeSeriesCapability, TruthBasisSemanticFacts, reason)
	truth.Level = level
	truth.Freshness = TruthFreshness{State: freshness}
	// Attach a freshness cause only on the proven branch. This handler holds the
	// evidence for exactly two non-fresh states: an unavailable series means the
	// Prometheus/Mimir collector is not reporting (missing collector completion),
	// and a building series means the metric has no indexed history yet (content
	// coverage unavailable). No other cause is provable here, so none is guessed.
	switch freshness {
	case FreshnessUnavailable:
		WithFreshnessCause(truth, FreshnessCauseMissingCollectorCompletion)
	case FreshnessBuilding:
		WithFreshnessCause(truth, FreshnessCauseContentCoverageUnavailable)
	}
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"metric": query.Metric,
		"unit":   unit,
		"window": query.Window,
		"step":   query.Step,
		"points": points,
	}, truth)
}

func queryParamOrDefault(r *http.Request, name, fallback string) string {
	if v := QueryParam(r, name); v != "" {
		return v
	}
	return fallback
}
