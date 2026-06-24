# operator-dashboard-panels-1.sh — emits the first chunk of the
# Eshu operator overview Grafana dashboard panels (IDs 1-13:
# "Is Eshu Healthy?" + "Queue and Worker Pool" + "Generation and
# Graph Health" rows). Sourced by scripts/generate-operator-dashboard.sh.
# The function prints the JSON array elements (without the surrounding
# [ ]) to stdout, comma-separated. The main script concatenates the
# output of panels-1 and panels-2 into the dashboard's "panels" array.
# This file is split from panels-2 to keep each shell file under the
# 500-line limit.
#
# Variables in scope (sourced from operator-dashboard-metrics.sh):
# ACTIVE_GENERATIONS, GENERATION_LIVENESS_FAILURES,
# GENERATION_LIVENESS_RECOVERED, GENERATION_LIVENESS_SUPERSEDED,
# QUEUE_DEPTH, QUEUE_OLDEST_AGE, WORKER_POOL_ACTIVE,
# SHARED_ACCEPTANCE_ROWS, GRAPH_ORPHAN_NODES.

operator_dashboard_panels_1() {
  cat <<EOF
    {
      "id": 1,
      "type": "row",
      "title": "Is Eshu Healthy?",
      "gridPos": {"h": 1, "w": 24, "x": 0, "y": 0},
      "collapsed": false,
      "panels": []
    },
    {
      "id": 2,
      "type": "stat",
      "title": "Stuck Generations (alarm)",
      "description": "Number of active scope generations currently in the 'stuck' age bucket. Any non-zero value is a 3 AM alarm: a generation is alive but not advancing. Cross-reference ${GENERATION_LIVENESS_FAILURES} for the recovery counter.",
      "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
      "gridPos": {"h": 4, "w": 6, "x": 0, "y": 1},
      "targets": [
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "max(${ACTIVE_GENERATIONS}{age_bucket=\"stuck\"})",
          "legendFormat": "stuck",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "short",
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {"color": "green", "value": null},
              {"color": "red", "value": 1}
            ]
          }
        },
        "overrides": []
      },
      "options": {
        "colorMode": "background",
        "graphMode": "none",
        "justifyMode": "auto",
        "textMode": "auto"
      }
    },
    {
      "id": 3,
      "type": "stat",
      "title": "Generation Liveness Failures",
      "description": "Total generation liveness sweep failures since process start. Counter; treat any non-zero rate as a recovery-loop alert.",
      "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
      "gridPos": {"h": 4, "w": 6, "x": 6, "y": 1},
      "targets": [
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "sum(rate(${GENERATION_LIVENESS_FAILURES}[5m]))",
          "legendFormat": "rate",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "ops",
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {"color": "green", "value": null},
              {"color": "red", "value": 0.01}
            ]
          }
        },
        "overrides": []
      },
      "options": {
        "colorMode": "value",
        "graphMode": "area",
        "justifyMode": "auto",
        "textMode": "auto"
      }
    },
    {
      "id": 4,
      "type": "stat",
      "title": "Worker Pool Saturation",
      "description": "Current active worker count per pool. Use pool=~'reducer|projector|collector' to see the hot paths.",
      "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
      "gridPos": {"h": 4, "w": 6, "x": 12, "y": 1},
      "targets": [
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "sum(${WORKER_POOL_ACTIVE}{pool=~\"\$pool\"})",
          "legendFormat": "active",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "short",
          "thresholds": {"mode": "absolute", "steps": [{"color": "green", "value": null}]}
        },
        "overrides": []
      },
      "options": {
        "colorMode": "value",
        "graphMode": "area",
        "justifyMode": "auto",
        "textMode": "auto"
      }
    },
    {
      "id": 5,
      "type": "stat",
      "title": "Queue Depth",
      "description": "Current queue depth summed across all queues and statuses. Sustained non-zero is backpressure; pair with ${QUEUE_OLDEST_AGE} for staleness.",
      "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
      "gridPos": {"h": 4, "w": 6, "x": 18, "y": 1},
      "targets": [
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "sum(${QUEUE_DEPTH})",
          "legendFormat": "depth",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "short",
          "thresholds": {"mode": "absolute", "steps": [{"color": "green", "value": null}, {"color": "yellow", "value": 100}, {"color": "red", "value": 10000}]}
        },
        "overrides": []
      },
      "options": {
        "colorMode": "background",
        "graphMode": "area",
        "justifyMode": "auto",
        "textMode": "auto"
      }
    },
    {
      "id": 6,
      "type": "row",
      "title": "Queue and Worker Pool",
      "collapsed": false,
      "gridPos": {"h": 1, "w": 24, "x": 0, "y": 5},
      "panels": []
    },
    {
      "id": 7,
      "type": "timeseries",
      "title": "Queue Depth by Status",
      "description": "Per-status queue depth (pending / claimed / done / failed). Pair with Queue Oldest Age below for staleness.",
      "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
      "gridPos": {"h": 8, "w": 12, "x": 0, "y": 6},
      "targets": [
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "sum(${QUEUE_DEPTH}{queue=~\"\$queue\"}) by (status)",
          "legendFormat": "{{status}}",
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
      "id": 8,
      "type": "timeseries",
      "title": "Queue Oldest Age (seconds)",
      "description": "Age of the oldest item in the queue, in seconds. Stale queues grow this even when depth is bounded.",
      "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
      "gridPos": {"h": 8, "w": 12, "x": 12, "y": 6},
      "targets": [
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "max(${QUEUE_OLDEST_AGE}{queue=~\"\$queue\"})",
          "legendFormat": "{{queue}}",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {"unit": "s", "custom": {"drawStyle": "line", "lineWidth": 1, "fillOpacity": 10}},
        "overrides": []
      },
      "options": {"legend": {"displayMode": "list", "placement": "bottom"}, "tooltip": {"mode": "multi"}}
    },
    {
      "id": 9,
      "type": "row",
      "title": "Generation and Graph Health",
      "collapsed": false,
      "gridPos": {"h": 1, "w": 24, "x": 0, "y": 14},
      "panels": []
    },
    {
      "id": 10,
      "type": "timeseries",
      "title": "Active Generations by Age Bucket",
      "description": "Current active scope generation count by closed activation-age bucket. Sustained 'stuck' value is a 3 AM alarm.",
      "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
      "gridPos": {"h": 8, "w": 12, "x": 0, "y": 15},
      "targets": [
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "${ACTIVE_GENERATIONS}",
          "legendFormat": "{{age_bucket}}",
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
      "id": 11,
      "type": "timeseries",
      "title": "Generation Liveness Recovery / Supersede / Failure",
      "description": "Per-second rate of generation liveness sweep outcomes. Recovered + superseded should dwarf failures on a healthy runtime.",
      "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
      "gridPos": {"h": 8, "w": 12, "x": 12, "y": 15},
      "targets": [
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "sum(rate(${GENERATION_LIVENESS_RECOVERED}[5m]))",
          "legendFormat": "recovered",
          "refId": "A"
        },
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "sum(rate(${GENERATION_LIVENESS_SUPERSEDED}[5m]))",
          "legendFormat": "superseded",
          "refId": "B"
        },
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "sum(rate(${GENERATION_LIVENESS_FAILURES}[5m]))",
          "legendFormat": "failures",
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
      "id": 12,
      "type": "timeseries",
      "title": "Graph Orphan Nodes (current)",
      "description": "Current bounded zero-relationship graph node count by closed node label. Sustained non-zero is cleanup pressure.",
      "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
      "gridPos": {"h": 8, "w": 12, "x": 0, "y": 23},
      "targets": [
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "${GRAPH_ORPHAN_NODES}",
          "legendFormat": "{{node_label}}",
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
      "id": 13,
      "type": "timeseries",
      "title": "Shared Acceptance Rows (current)",
      "description": "Current durable shared-projection acceptance row count. Steady state is bounded by source surface; growth indicates a stuck acceptance.",
      "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
      "gridPos": {"h": 8, "w": 12, "x": 12, "y": 23},
      "targets": [
        {
          "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
          "expr": "${SHARED_ACCEPTANCE_ROWS}",
          "legendFormat": "rows",
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
