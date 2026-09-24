// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B5 root alias shim for #6642: the platform-metrics aliases must live in package query so the APIRouter field and the cmd/api and cmd/mcp-server wiring compile unchanged.

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/metrics"
)

// metrics_alias.go is the root alias shim for the platform-metrics family,
// which moved to internal/query/metrics (#6642). cmd/api and cmd/mcp-server
// build the handler, its Prometheus source, and the request-metrics
// middleware through package query, and the APIRouter field names the
// handler, so these stay until the #6642 alias sweep.

// MetricsHandler serves GET /api/v0/metrics/timeseries. See metrics.Handler.
type MetricsHandler = metrics.Handler

// MetricPoint is one timestamped sample. See metrics.Point.
type MetricPoint = metrics.Point

// MetricsRangeQuery bounds one time-series read. See metrics.RangeQuery.
type MetricsRangeQuery = metrics.RangeQuery

// MetricsTimeSeriesSource reads platform metric series. See
// metrics.TimeSeriesSource.
type MetricsTimeSeriesSource = metrics.TimeSeriesSource

// PrometheusMetricsTimeSeriesConfig configures one Prometheus-compatible
// source. See metrics.PrometheusTimeSeriesConfig.
type PrometheusMetricsTimeSeriesConfig = metrics.PrometheusTimeSeriesConfig

// NewPrometheusMetricsTimeSeriesSource forwards unchanged to
// metrics.NewPrometheusTimeSeriesSource.
func NewPrometheusMetricsTimeSeriesSource(
	config metrics.PrometheusTimeSeriesConfig,
) (*metrics.PrometheusTimeSeriesSource, error) {
	return metrics.NewPrometheusTimeSeriesSource(config)
}

// RequestMetricsMiddleware forwards unchanged to metrics.RequestMiddleware.
func RequestMetricsMiddleware(mux *http.ServeMux) http.Handler {
	return metrics.RequestMiddleware(mux)
}
