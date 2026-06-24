// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsMetrics documents the historical metrics time-series route.
const openAPIPathsMetrics = `
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
                    "metric": {"type": "string"},
                    "unit": {"type": "string"},
                    "window": {"type": "string"},
                    "step": {"type": "string"},
                    "points": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "t": {"type": "string", "format": "date-time"},
                          "v": {"type": "number"}
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
