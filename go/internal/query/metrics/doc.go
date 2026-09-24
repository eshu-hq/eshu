// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package metrics serves GET /api/v0/metrics/timeseries, the bounded
// historical time-series read behind the console's trend panels, and owns the
// request-metrics middleware every API and MCP route runs behind.
//
// Handler serves only the metric names in its allow-list (ingest_rate,
// queue_depth, dead_letters, graph_nodes, graph_edges, query_p50, query_p95,
// query_p99); a missing or unknown metric, or a range the source rejects as
// invalid, is a 400, and any other source error is a 500. It reads through a
// TimeSeriesSource; PrometheusTimeSeriesSource,
// built by NewPrometheusTimeSeriesSource, is the Prometheus/Mimir
// query_range implementation. A handler with no source returns empty points
// with unavailable freshness rather than failing, and an empty history is
// reported as building, not as an error.
//
// RequestMiddleware wraps a ServeMux and records
// eshu_dp_api_request_duration_seconds and eshu_dp_api_request_errors_total
// per matched route pattern and status class. It resolves the route with
// mux.Handler without mutating the request, so label cardinality stays bounded
// by the registered routes, and its response writer forwards Flush and Hijack
// so streaming responses keep working.
//
// Capability and Support declare the route's capability row once:
// internal/query/contract registers Support() for production and
// main_test.go registers it for this package's own tests.
//
// The package moved out of the root query package for #6642. Root keeps
// MetricsHandler, the source and config types, the Prometheus constructor
// and RequestMetricsMiddleware in metrics_alias.go for cmd/api and
// cmd/mcp-server until the #6642 alias sweep.
package metrics
