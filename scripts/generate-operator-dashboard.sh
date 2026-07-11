#!/usr/bin/env bash
#
# generate-operator-dashboard.sh — emit the Eshu operator overview
# Grafana dashboard JSON to docs/public/observability/dashboards/.
# The script is the source of truth for the dashboard; the generated
# file is the committed artifact. To change a panel, metric
# expression, or title, edit the metric registry
# (scripts/lib/operator-dashboard-metrics.sh) or a template body
# (scripts/lib/operator-dashboard-{head,panels-1,panels-2,tail}.json.tmpl),
# re-run this script, and commit the regenerated output. The test
# mirror scripts/test-generate-operator-dashboard.sh asserts that the
# committed artifact is well-formed, matches what this script
# produces (idempotency), and contains the headline panels.
#
# The dashboard is the operator-visible surface of the Epic X
# telemetry coverage discipline. It covers the headline eshu_dp_*
# families listed in docs/public/reference/telemetry/index.md and
# docs/public/observability/telemetry-coverage.md.
#
# Panel provenance (originally split across scripts/lib/operator-dashboard-
# panels-{1,2}.sh, now scripts/lib/operator-dashboard-panels-{1,2}.json.tmpl):
#
# panels-1 covers panel IDs 1-13 ("Is Eshu Healthy?" + "Queue and Worker
# Pool" + "Generation and Graph Health" rows). Variables referenced:
# ACTIVE_GENERATIONS, GENERATION_LIVENESS_FAILURES,
# GENERATION_LIVENESS_RECOVERED, GENERATION_LIVENESS_SUPERSEDED,
# QUEUE_DEPTH, QUEUE_OLDEST_AGE, WORKER_POOL_ACTIVE,
# SHARED_ACCEPTANCE_ROWS, GRAPH_ORPHAN_NODES, REDUCER_INPUT_INVALID_FACTS,
# PROJECTOR_INPUT_INVALID_FACTS.
#
# panels-2 covers panel IDs 14-26 ("API Request Latency and Errors" +
# "Cross-Repo and Collector Pressure" rows). Variables referenced:
# API_REQUEST_DURATION, API_REQUEST_ERRORS, CROSS_REPO_FENCED,
# COLLECTOR_RECONCILIATION_FULL, COLLECTOR_RECONCILIATION_DRIFT,
# COLLECTOR_RECONCILIATION_CONVERGENCE, COLLECTOR_BACKPRESSURE,
# COLLECTOR_RETRIES, COLLECTOR_DEAD_LETTER, EDGES_BY_SOURCE_TOOL,
# FILES_BY_LANGUAGE.
#
# HEREDOC-FREE BY DESIGN (issue #5019, reopened after #5068): bash 5.1+
# delivers a `<<EOF` heredoc body to its reader by writing the ENTIRE body
# to a pipe before the reader is spawned. macOS's pipe buffer is 512 bytes,
# so any heredoc body strictly between 512 bytes and the 64KB pipe-buffer
# ceiling deadlocks under Homebrew bash (observed as bash >=5.3.15 hanging
# where /bin/bash 3.2.57 does not). The panel bodies here are 10-13KB, well
# inside the hang zone. The fix is to never route a large body through a
# heredoc: the JSON bodies live in scripts/lib/*.json.tmpl DATA FILES, read
# with the `$(<file)` builtin (no subprocess, no pipe) and emitted with the
# `printf` builtin (also no subprocess, no pipe). Neither construct touches
# the heredoc pipe, so this generator does not hang under any bash version.
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

# render_template reads a .json.tmpl DATA FILE (never a heredoc) via the
# `$(<file)` builtin and substitutes every ${NAME} token named in the
# OPERATOR_DASHBOARD_METRIC_VARS allowlist (scripts/lib/operator-dashboard-
# metrics.sh) with that variable's value. Anything not on the allowlist —
# notably the literal Grafana ${DS_PROMETHEUS} token (53 occurrences) and
# $__all — is never looked up and so passes through unchanged, by
# construction.
#
# The replacement is deliberately UNQUOTED in the ${content//pattern/value}
# expansion: quoting it makes bash 3.2 emit literal quote characters into
# the JSON output instead of substituting the bare value.
render_template() {
  local content name value pattern
  content="$(<"$1")"
  for name in ${OPERATOR_DASHBOARD_METRIC_VARS}; do
    value="${!name}"
    pattern='${'"$name"'}'
    content="${content//"$pattern"/$value}"
  done
  printf '%s' "$content"
}

mkdir -p "$(dirname "$output_path")"

# Sequential emission: head, panels_1, a literal comma, panels_2, tail. Each
# piece is written with its own printf so the panel bodies (which contain
# literal \" sequences) are never routed through the ${var//...} substitution
# above — only render_template's own local `content` variable is substituted.
# `$(<file)` strips the file's trailing newline, so the newlines below
# reconstruct the exact byte layout of the previously heredoc-emitted file.
{
  render_template "${lib_dir}/operator-dashboard-head.json.tmpl"
  printf '\n'
  render_template "${lib_dir}/operator-dashboard-panels-1.json.tmpl"
  printf ',\n'
  render_template "${lib_dir}/operator-dashboard-panels-2.json.tmpl"
  printf '\n'
  render_template "${lib_dir}/operator-dashboard-tail.json.tmpl"
  printf '\n'
} >"$output_path"

printf 'generate-operator-dashboard: wrote %s\n' "$output_path"
