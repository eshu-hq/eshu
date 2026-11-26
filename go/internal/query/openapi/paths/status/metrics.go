// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// Metrics documents the historical metrics time-series route.
const Metrics = `
    "/api/v0/metrics/timeseries": {
      "get": {
        "tags": ["status"],
        "summary": "Get a historical metric time-series",
        "description": "Returns an ordered point series for one metric over a window, sourced from the Prometheus/Mimir collector. When no metrics source is configured the response is empty points with unavailable freshness (not an error); a metric with no history yet returns empty points with building freshness.",
        "operationId": "getMetricsTimeSeries",
        "parameters": [
          {"name": "metric", "in": "query", "required": true, "schema": {"type": "string", "enum": ["ingest_rate", "queue_depth", "dead_letters", "graph_nodes", "graph_edges", "query_p50", "query_p95", "query_p99"]}},
          {"name": "window", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Lookback window, e.g. 24h. Defaults to 24h and must be at most 30d."},
          {"name": "step", "in": "query", "required": false, "schema": {"type": "string"}, "description": "Sample step, e.g. 30m. Defaults to 30m, must be at least 10s, and the range must request at most 2000 samples."}
        ],
        "responses": {
          "200": {
            "description": "Metric time-series",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "metric": {"type": "string", "description": "Requested metric name echoed back by the time-series handler."},
                    "unit": {"type": "string", "description": "Unit of the series values, chosen by the handler per metric."},
                    "window": {"type": "string", "description": "Lookback window the handler served, for example 24h."},
                    "step": {"type": "string", "description": "Sample step the handler served, for example 30m."},
                    "points": {
                      "type": "array",
                      "description": "Ordered sample points over the window; empty with unavailable or building freshness when the metrics source is unconfigured or the metric has no history yet.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "t": {"type": "string", "format": "date-time", "description": "Sample timestamp in UTC."},
                          "v": {"type": "number", "description": "Sample value in unit."}
                        }
                      }
                    }
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"}
        }
      }
    },
`
