// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package metrics

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	timeSeriesDefaultWindow = "24h"
	timeSeriesDefaultStep   = "30m"
)

var errInvalidMetricsRange = errors.New("invalid metrics time-series range")

// Point is one timestamped sample in a metric series.
type Point struct {
	T string  `json:"t"`
	V float64 `json:"v"`
}

// RangeQuery bounds a single time-series read.
type RangeQuery struct {
	Metric string
	Window string
	Step   string
}

// TimeSeriesSource reads historical metric series, typically backed by the
// Prometheus/Mimir collector. It is defined here because the query layer is its
// only consumer; an implementation is wired in at the API command. When no source
// is configured the endpoint returns empty points rather than failing.
type TimeSeriesSource interface {
	RangeQuery(ctx context.Context, query RangeQuery) ([]Point, error)
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

// Handler serves historical time-series for dashboard and operations
// charts. Source may be nil when no Prometheus/Mimir collector is configured, in
// which case the endpoint returns empty points with unavailable freshness.
type Handler struct {
	Source  TimeSeriesSource
	Profile querycontract.QueryProfile
}

func (h *Handler) profile() querycontract.QueryProfile {
	if h == nil {
		return querycontract.ProfileProduction
	}
	return querycontract.NormalizeQueryProfile(string(h.Profile))
}

// Mount registers the metrics time-series route.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/metrics/timeseries", h.getTimeSeries)
}

// getTimeSeries returns an ordered point series for one metric over a window.
// GET /api/v0/metrics/timeseries?metric=&window=&step=
func (h *Handler) getTimeSeries(w http.ResponseWriter, r *http.Request) {
	metric := querycontract.QueryParam(r, "metric")
	if metric == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "metric is required")
		return
	}
	unit, ok := metricUnits[metric]
	if !ok {
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("unsupported metric %q", metric))
		return
	}

	query := RangeQuery{
		Metric: metric,
		Window: queryParamOrDefault(r, "window", timeSeriesDefaultWindow),
		Step:   queryParamOrDefault(r, "step", timeSeriesDefaultStep),
	}

	if h == nil || h.Source == nil {
		// No metrics collector configured: empty points, not an error.
		h.writeSeries(w, r, query, unit, []Point{}, querycontract.FreshnessUnavailable,
			"no metrics time-series source configured; configure the Prometheus/Mimir collector to enable trends")
		return
	}

	points, err := h.Source.RangeQuery(r.Context(), query)
	if err != nil {
		if errors.Is(err, errInvalidMetricsRange) {
			querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("invalid metrics range: %v", err))
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("metrics query failed: %v", err))
		return
	}
	if points == nil {
		points = []Point{}
	}
	freshness := querycontract.FreshnessFresh
	reason := "resolved from the metrics time-series source"
	if len(points) == 0 {
		// History not yet available for this metric — building, not an error.
		freshness = querycontract.FreshnessBuilding
		reason = "metric has no history yet"
	}
	h.writeSeries(w, r, query, unit, points, freshness, reason)
}

func (h *Handler) writeSeries(
	w http.ResponseWriter,
	r *http.Request,
	query RangeQuery,
	unit string,
	points []Point,
	freshness querycontract.FreshnessState,
	reason string,
) {
	level := querycontract.TruthLevelDerived
	if freshness == querycontract.FreshnessUnavailable {
		level = querycontract.TruthLevelFallback
	}
	truth := querycontract.BuildTruthEnvelope(h.profile(), Capability, querycontract.TruthBasisSemanticFacts, reason)
	truth.Level = level
	truth.Freshness = querycontract.TruthFreshness{State: freshness}
	// Attach a freshness cause only on the proven branch. This handler holds the
	// evidence for exactly two non-fresh states: an unavailable series means the
	// Prometheus/Mimir collector is not reporting (missing collector completion),
	// and a building series means the metric has no indexed history yet (content
	// coverage unavailable). No other cause is provable here, so none is guessed.
	switch freshness {
	case querycontract.FreshnessUnavailable:
		querycontract.WithFreshnessCause(truth, querycontract.FreshnessCauseMissingCollectorCompletion)
	case querycontract.FreshnessBuilding:
		querycontract.WithFreshnessCause(truth, querycontract.FreshnessCauseContentCoverageUnavailable)
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"metric": query.Metric,
		"unit":   unit,
		"window": query.Window,
		"step":   query.Step,
		"points": points,
	}, truth)
}

func queryParamOrDefault(r *http.Request, name, fallback string) string {
	if v := querycontract.QueryParam(r, name); v != "" {
		return v
	}
	return fallback
}
