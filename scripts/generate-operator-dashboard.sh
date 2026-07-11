#!/usr/bin/env bash
#
# generate-operator-dashboard.sh — emit the Eshu operator overview
# Grafana dashboard JSON to docs/public/observability/dashboards/.
# The script is the source of truth for the dashboard; the generated
# file is the committed artifact. To change a panel, metric
# expression, or title, edit the metric registry
# (scripts/lib/operator-dashboard-metrics.sh) or the panel lib
# (scripts/lib/operator-dashboard-panels-{1,2}.sh), re-run this
# script, and commit the regenerated output. The test mirror
# scripts/test-generate-operator-dashboard.sh asserts that the
# committed artifact is well-formed, matches what this script
# produces (idempotency), and contains the headline panels.
#
# The dashboard is the operator-visible surface of the Epic X
# telemetry coverage discipline. It covers the headline eshu_dp_*
# families listed in docs/public/reference/telemetry/index.md and
# docs/public/observability/telemetry-coverage.md.
set -euo pipefail

repo_root="${ESHU_OPERATOR_DASHBOARD_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
    || (cd "$(dirname "$0")/.." && pwd))"
fi

output_path="${ESHU_OPERATOR_DASHBOARD_OUTPUT_PATH:-${repo_root}/docs/public/observability/dashboards/eshu-operator-overview.json}"

lib_dir="$(dirname "$0")/lib"
# shellcheck source=scripts/lib/operator-dashboard-metrics.sh
. "${lib_dir}/operator-dashboard-metrics.sh"
# shellcheck source=scripts/lib/operator-dashboard-panels-1.sh
. "${lib_dir}/operator-dashboard-panels-1.sh"
# shellcheck source=scripts/lib/operator-dashboard-panels-2.sh
. "${lib_dir}/operator-dashboard-panels-2.sh"

# Emit each panel block's large `cat <<EOF` heredoc to a temp file (its stdout
# redirected to a real file, NOT captured inside a `$(...)`), then read the file
# back with the `$(<file)` builtin — which uses no `cat` subprocess. Capturing a
# 300+-line heredoc directly via `panels_N=$(operator_dashboard_panels_N)` is the
# large-input-in-command-substitution construct that hangs on Homebrew bash
# 5.3.15 (issue #5019, same class as the `<<<` here-string fixed in #4718). Both
# `$(func)` and `$(<file)` strip trailing newlines identically, so the assembled
# output below is byte-for-byte unchanged.
panels_1_file=$(mktemp "${TMPDIR:-/tmp}/eshu-operator-panels-1.XXXXXX")
panels_2_file=$(mktemp "${TMPDIR:-/tmp}/eshu-operator-panels-2.XXXXXX")
trap 'rm -f "$panels_1_file" "$panels_2_file"' EXIT
operator_dashboard_panels_1 >"$panels_1_file"
operator_dashboard_panels_2 >"$panels_2_file"
panels_1=$(<"$panels_1_file")
panels_2=$(<"$panels_2_file")

mkdir -p "$(dirname "$output_path")"

cat >"$output_path" <<EOF
{
  "__inputs": [
    {
      "name": "DS_PROMETHEUS",
      "label": "Prometheus",
      "description": "",
      "type": "datasource",
      "pluginId": "prometheus",
      "pluginName": "Prometheus"
    }
  ],
  "__requires": [
    {
      "type": "grafana",
      "id": "grafana",
      "name": "Grafana",
      "version": "10.0.0"
    },
    {
      "type": "datasource",
      "id": "prometheus",
      "name": "Prometheus",
      "version": "1.0.0"
    },
    {
      "type": "panel",
      "id": "stat",
      "name": "Stat",
      "version": ""
    },
    {
      "type": "panel",
      "id": "timeseries",
      "name": "Time series",
      "version": ""
    }
  ],
  "annotations": {
    "list": []
  },
  "description": "Eshu operator overview. Surfaces the headline eshu_dp_* metric families for the queue, worker pool, generation liveness, graph pressure, cross-repo activation, API latency and errors, and per-collector backpressure. Imported via docs/public/observability/dashboards/eshu-operator-overview.json. The metric source of truth is go/internal/telemetry/instruments.go and docs/public/observability/telemetry-coverage.md.",
  "editable": true,
  "fiscalYearStartMonth": 0,
  "graphTooltip": 1,
  "id": null,
  "links": [],
  "liveNow": false,
  "panels": [
${panels_1},
${panels_2}
  ],
  "refresh": "30s",
  "schemaVersion": 39,
  "tags": ["eshu", "operator", "overview", "epic-x"],
  "templating": {
    "list": [
      {
        "name": "datasource",
        "label": "Data source",
        "type": "datasource",
        "query": "prometheus",
        "current": {"selected": false, "text": "Prometheus", "value": "Prometheus"},
        "hide": 0
      },
      {
        "name": "pool",
        "label": "Worker pool",
        "type": "query",
        "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
        "query": "label_values(${WORKER_POOL_ACTIVE}, pool)",
        "refresh": 2,
        "includeAll": true,
        "multi": true,
        "current": {"selected": false, "text": "All", "value": "\$__all"}
      },
      {
        "name": "queue",
        "label": "Queue",
        "type": "query",
        "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
        "query": "label_values(${QUEUE_DEPTH}, queue)",
        "refresh": 2,
        "includeAll": true,
        "multi": true,
        "current": {"selected": false, "text": "All", "value": "\$__all"}
      },
      {
        "name": "route",
        "label": "API route",
        "type": "query",
        "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
        "query": "label_values(${API_REQUEST_DURATION}, route)",
        "refresh": 2,
        "includeAll": true,
        "multi": true,
        "current": {"selected": false, "text": "All", "value": "\$__all"}
      },
      {
        "name": "scope_id",
        "label": "Scope ID",
        "type": "query",
        "datasource": {"type": "prometheus", "uid": "\${DS_PROMETHEUS}"},
        "query": "label_values(${CROSS_REPO_FENCED}, scope_id)",
        "refresh": 2,
        "includeAll": true,
        "multi": true,
        "current": {"selected": false, "text": "All", "value": "\$__all"}
      }
    ]
  },
  "time": {"from": "now-1h", "to": "now"},
  "timepicker": {},
  "timezone": "",
  "title": "Eshu Operator Overview",
  "uid": "eshu-operator-overview",
  "version": 1,
  "weekStart": ""
}
EOF

printf 'generate-operator-dashboard: wrote %s\n' "$output_path"
