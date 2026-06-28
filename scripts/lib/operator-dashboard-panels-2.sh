# operator-dashboard-panels-2.sh — emits the second chunk of the
# Eshu operator overview Grafana dashboard panels (IDs 14-20:
# "API Request Latency and Errors" + "Cross-Repo and Collector
# Pressure" rows). Sourced by scripts/generate-operator-dashboard.sh.
# The function prints the JSON array elements (without the surrounding
# [ ]) to stdout, comma-separated. The main script concatenates the
# output of panels-1 and panels-2 into the dashboard's "panels" array.
# This file is split from panels-1 to keep each shell file under the
# 500-line limit.
#
# Variables in scope (sourced from operator-dashboard-metrics.sh):
# API_REQUEST_DURATION, API_REQUEST_ERRORS, CROSS_REPO_FENCED,
# COLLECTOR_RECONCILIATION_FULL, COLLECTOR_RECONCILIATION_DRIFT,
# COLLECTOR_RECONCILIATION_CONVERGENCE, COLLECTOR_BACKPRESSURE,
# COLLECTOR_RETRIES, COLLECTOR_DEAD_LETTER.

operator_dashboard_panels_2() {
  cat <<EOF
    {
      "id": 14,
      "type": "row",
      "title": "API Request Latency and Errors",
      "collapsed": false,
      "gridPos": {"h": 1, "w": 24, "x": 0, "y": 31},
      "panels": []
    },
    {
      "id": 15,
      "type": "timeseries",
      "title": "API Request Duration p50 / p95 / p99",
      "description": "Per-route request latency percentiles over a 5-minute window. Sustained p99 growth without saturation is a regression.",
      "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
      "gridPos": {"h": 8, "w": 16, "x": 0, "y": 32},
      "targets": [
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "histogram_quantile(0.50, sum(rate(${API_REQUEST_DURATION}{route=~\"\$route\"}[5m])) by (le, route))",
          "legendFormat": "p50 {{route}}",
          "refId": "A"
        },
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "histogram_quantile(0.95, sum(rate(${API_REQUEST_DURATION}{route=~\"\$route\"}[5m])) by (le, route))",
          "legendFormat": "p95 {{route}}",
          "refId": "B"
        },
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "histogram_quantile(0.99, sum(rate(${API_REQUEST_DURATION}{route=~\"\$route\"}[5m])) by (le, route))",
          "legendFormat": "p99 {{route}}",
          "refId": "C"
        }
      ],
      "fieldConfig": {
        "defaults": {"unit": "s", "custom": {"drawStyle": "line", "lineWidth": 1, "fillOpacity": 5}},
        "overrides": []
      },
      "options": {"legend": {"displayMode": "list", "placement": "right"}, "tooltip": {"mode": "multi"}}
    },
    {
      "id": 16,
      "type": "timeseries",
      "title": "API Request Errors (5xx rate)",
      "description": "Per-route 5xx request rate. Non-zero sustained rate is an alarm.",
      "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
      "gridPos": {"h": 8, "w": 8, "x": 16, "y": 32},
      "targets": [
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "sum(rate(${API_REQUEST_ERRORS}{status_class=~\"5..\"}[5m])) by (route)",
          "legendFormat": "{{route}}",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {"unit": "ops", "custom": {"drawStyle": "line", "lineWidth": 1, "fillOpacity": 10}},
        "overrides": []
      },
      "options": {"legend": {"displayMode": "list", "placement": "bottom"}, "tooltip": {"mode": "multi"}}
    },
    {
      "id": 17,
      "type": "row",
      "title": "Cross-Repo and Collector Pressure",
      "collapsed": false,
      "gridPos": {"h": 1, "w": 24, "x": 0, "y": 40},
      "panels": []
    },
    {
      "id": 18,
      "type": "timeseries",
      "title": "Cross-Repo Activation Fenced",
      "description": "Total cross-repo activation fence events per scope_id. Spikes indicate activation ordering conflicts.",
      "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
      "gridPos": {"h": 8, "w": 12, "x": 0, "y": 41},
      "targets": [
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "sum(rate(${CROSS_REPO_FENCED}{scope_id=~\"\$scope_id\"}[5m])) by (scope_id)",
          "legendFormat": "{{scope_id}}",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {"unit": "ops", "custom": {"drawStyle": "line", "lineWidth": 1, "fillOpacity": 10}},
        "overrides": []
      },
      "options": {"legend": {"displayMode": "list", "placement": "bottom"}, "tooltip": {"mode": "multi"}}
    },
    {
      "id": 19,
      "type": "timeseries",
      "title": "Collector Backpressure / Retries / Dead-Letter",
      "description": "Per-second rate of collector backpressure, retry, and dead-letter events. Pair with the per-collector dashboards under docs/dashboards/ for family-level detail.",
      "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
      "gridPos": {"h": 8, "w": 12, "x": 12, "y": 41},
      "targets": [
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "sum(rate(${COLLECTOR_BACKPRESSURE}[5m]))",
          "legendFormat": "backpressure",
          "refId": "A"
        },
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "sum(rate(${COLLECTOR_RETRIES}[5m]))",
          "legendFormat": "retries",
          "refId": "B"
        },
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "sum(rate(${COLLECTOR_DEAD_LETTER}[5m]))",
          "legendFormat": "dead-letter",
          "refId": "C"
        }
      ],
      "fieldConfig": {
        "defaults": {"unit": "ops", "custom": {"drawStyle": "line", "lineWidth": 1, "fillOpacity": 10}},
        "overrides": []
      },
      "options": {"legend": {"displayMode": "list", "placement": "bottom"}, "tooltip": {"mode": "multi"}}
    },
    {
      "id": 20,
      "type": "timeseries",
      "title": "Collector Reconciliation (full snapshots / drift / convergence)",
      "description": "Per-second rate of collector reconciliation outcomes. Convergence dominates in a steady state; spikes in drift retraction flag collector disagreement.",
      "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
      "gridPos": {"h": 8, "w": 24, "x": 0, "y": 49},
      "targets": [
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "sum(rate(${COLLECTOR_RECONCILIATION_FULL}[5m]))",
          "legendFormat": "full-snapshots",
          "refId": "A"
        },
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "sum(rate(${COLLECTOR_RECONCILIATION_DRIFT}[5m]))",
          "legendFormat": "drift-retractions",
          "refId": "B"
        },
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "sum(rate(${COLLECTOR_RECONCILIATION_CONVERGENCE}[5m]))",
          "legendFormat": "convergence",
          "refId": "C"
        }
      ],
      "fieldConfig": {
        "defaults": {"unit": "ops", "custom": {"drawStyle": "line", "lineWidth": 1, "fillOpacity": 10}},
        "overrides": []
      },
      "options": {"legend": {"displayMode": "list", "placement": "bottom"}, "tooltip": {"mode": "multi"}}
    },
    {
      "id": 21,
      "type": "timeseries",
      "title": "Edges by Source Tool (current)",
      "description": "Current exact graph edge count by closed source_tool label, summed across the Tier-2 relationship types that carry source_tool (relationship-type-index answered). A series dropping to zero means the corresponding parser or ingester stopped writing that edge type. ESHU_GRAPH_COUNT_LIMIT (default 10 000) caps returned label groups, not the rows counted, so per-tool counts are exact.",
      "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
      "gridPos": {"h": 8, "w": 12, "x": 0, "y": 57},
      "targets": [
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "${EDGES_BY_SOURCE_TOOL}",
          "legendFormat": "{{source_tool}}",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {"unit": "short", "custom": {"drawStyle": "line", "lineWidth": 1, "fillOpacity": 10}},
        "overrides": []
      },
      "options": {"legend": {"displayMode": "list", "placement": "bottom"}, "tooltip": {"mode": "multi"}}
    },
    {
      "id": 22,
      "type": "timeseries",
      "title": "Files by Language (current)",
      "description": "Current exact File node count by language (File-label-anchored group). A series dropping to zero means the corresponding parser stopped indexing files. ESHU_GRAPH_COUNT_LIMIT (default 10 000) caps returned language groups, not the rows counted, so per-language counts are exact.",
      "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
      "gridPos": {"h": 8, "w": 12, "x": 12, "y": 57},
      "targets": [
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "${FILES_BY_LANGUAGE}",
          "legendFormat": "{{language}}",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {"unit": "short", "custom": {"drawStyle": "line", "lineWidth": 1, "fillOpacity": 10}},
        "overrides": []
      },
      "options": {"legend": {"displayMode": "list", "placement": "bottom"}, "tooltip": {"mode": "multi"}}
    }
EOF
}
