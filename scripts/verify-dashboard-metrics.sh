#!/usr/bin/env bash
#
# verify-dashboard-metrics.sh — cross-reference every eshu_dp_* metric name
# referenced in Grafana dashboard JSONs against the canonical metric registry
# go/internal/telemetry/instruments.go.
#
# Direction: dashboard → registry only. This gate catches a panel referencing
# a non-existent metric; it intentionally does NOT require every registered
# metric to appear on a dashboard, because not every metric needs a panel.
# A future reader should not expect registry → dashboard coverage here.
#
# Assumption: all metric names in instruments.go are registered with string
# literals. A metric registered via a constructed (non-literal) name would
# read as an orphan if a dashboard referenced it. This is not the case today
# but is worth noting for future maintainers.
#
# Exit 0 when every dashboard metric resolves to a registered instrument;
# exit non-zero when orphan metrics are found.
#
# Flags:
#   --dashboard PATH   check a single dashboard JSON
#   --table            output a markdown coverage table instead of a pass/fail check
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

instruments_path="${repo_root}/go/internal/telemetry/instruments.go"
dashboards_dir_deploy="${repo_root}/deploy/grafana/dashboards"
dashboards_dir_docs="${repo_root}/docs/dashboards"
dashboards_dir_obs="${repo_root}/docs/public/observability/dashboards"

single_dashboard=""
table_mode=0

while [ $# -gt 0 ]; do
  case "$1" in
    --dashboard)
      if [ $# -lt 2 ]; then
        printf 'verify-dashboard-metrics: --dashboard requires a path argument\n' >&2
        exit 2
      fi
      single_dashboard="$2"
      shift 2
      ;;
    --table)
      table_mode=1
      shift
      ;;
    *)
      printf 'verify-dashboard-metrics: unknown flag %s\n' "$1" >&2
      exit 2
      ;;
  esac
done

# Collect dashboard JSON files.
dashboard_files_tmp="$(mktemp)"
metrics_dashboard_tmp="$(mktemp)"
instruments_metrics_tmp="$(mktemp)"
validate_tmp="$(mktemp)"
trap 'rm -f "${dashboard_files_tmp}" "${metrics_dashboard_tmp}" "${instruments_metrics_tmp}" "${validate_tmp}"' EXIT

if [ -n "${single_dashboard}" ]; then
  if [ ! -f "${single_dashboard}" ]; then
    printf 'verify-dashboard-metrics: dashboard file not found: %s\n' "${single_dashboard}" >&2
    exit 1
  fi
  printf '%s\n' "${single_dashboard}" >"${dashboard_files_tmp}"
else
  for dir in "${dashboards_dir_deploy}" "${dashboards_dir_docs}" "${dashboards_dir_obs}"; do
    if [ -d "${dir}" ]; then
      ls "${dir}"/*.json 2>/dev/null >>"${dashboard_files_tmp}" || true
    fi
  done
fi

if [ ! -s "${dashboard_files_tmp}" ]; then
  printf 'verify-dashboard-metrics: no dashboard JSON files found\n' >&2
  exit 0
fi

# Extract every eshu_dp_* metric reference from every dashboard JSON.  We record
# the file path alongside the metric name so --table mode can attribute orphans.
# rg -IN suppresses line numbers and filenames; we capture filenames per-file.
: >"${metrics_dashboard_tmp}"
while IFS= read -r df; do
  [ -n "${df}" ] || continue
  rg -o 'eshu_dp_[a-zA-Z0-9_]+' "${df}" 2>/dev/null \
    | sed "s|^|${df} |" \
    >>"${metrics_dashboard_tmp}" || true
done <"${dashboard_files_tmp}"

sort -u -o "${metrics_dashboard_tmp}" "${metrics_dashboard_tmp}"

if [ ! -s "${metrics_dashboard_tmp}" ]; then
  printf 'verify-dashboard-metrics: no eshu_dp_* metrics found in dashboard JSONs\n'
  exit 0
fi

# instruments_metrics_tmp: every eshu_dp_* name registered in
# go/internal/telemetry/instruments.go.  PCRE2 mode (-P) is required so \s can
# match across newlines between the constructor open paren and the metric name.
rg -UPo '\.(?:Int64|Float64)(?:Counter|Histogram|UpDownCounter|Gauge|ObservableGauge|ObservableCounter|ObservableUpDownCounter)\(\s*"([a-zA-Z0-9_]+)"' \
  --replace '$1' "${instruments_path}" 2>/dev/null \
  | rg '^eshu_dp_' \
  | sort -u >"${instruments_metrics_tmp}" || true

if [ ! -s "${instruments_metrics_tmp}" ]; then
  printf 'verify-dashboard-metrics: no eshu_dp_* metrics found in %s\n' "${instruments_path}" >&2
  exit 1
fi

# is_registered <name> returns 0 when the metric (or its _bucket/_count/_sum
# -stripped base) appears in instruments.go, 1 otherwise.
is_registered() {
  local name="$1"
  local base
  rg -qxF "${name}" "${instruments_metrics_tmp}" && return 0
  base="${name%_bucket}"
  [ "${base}" != "${name}" ] && rg -qxF "${base}" "${instruments_metrics_tmp}" && return 0
  base="${name%_count}"
  [ "${base}" != "${name}" ] && rg -qxF "${base}" "${instruments_metrics_tmp}" && return 0
  base="${name%_sum}"
  [ "${base}" != "${name}" ] && rg -qxF "${base}" "${instruments_metrics_tmp}" && return 0
  return 1
}

# Build a validated list: file metric status.
: >"${validate_tmp}"
while IFS=' ' read -r file metric; do
  [ -n "${metric}" ] || continue
  if is_registered "${metric}"; then
    printf '%s %s ok\n' "${file}" "${metric}" >>"${validate_tmp}"
  else
    printf '%s %s orphan\n' "${file}" "${metric}" >>"${validate_tmp}"
  fi
done <"${metrics_dashboard_tmp}"

# --- table mode: emit a markdown coverage table ------------------------------
if [ "${table_mode}" -eq 1 ]; then
  printf '| Dashboard | Metric | Found in instruments.go | Status |\n'
  printf '| --- | --- | --- | --- |\n'
  while IFS=' ' read -r file metric status; do
    dash="$(basename "${file}")"
    if [ "${status}" = "ok" ]; then
      printf '| %s | `%s` | yes | ok |\n' "${dash}" "${metric}"
    else
      printf '| %s | `%s` | no | <strong>ORPHAN</strong> |\n' "${dash}" "${metric}"
    fi
  done <"${validate_tmp}"
  exit 0
fi

# --- pass / fail check mode --------------------------------------------------
orphan_count="$(rg -c ' orphan$' "${validate_tmp}" 2>/dev/null || printf 0)"

if [ "${orphan_count}" -eq 0 ]; then
  count="$(wc -l <"${validate_tmp}" | tr -d ' ')"
  printf 'verify-dashboard-metrics: %d dashboard metrics checked, 0 orphans\n' "${count}"
  exit 0
fi

# Build a report for stderr.
{
  printf 'verify-dashboard-metrics: %d orphan dashboard metric(s) detected\n' "${orphan_count}"
  printf '\nThe following dashboard metrics are not registered in %s:\n\n' "${instruments_path}"
  rg ' orphan$' "${validate_tmp}" | while IFS=' ' read -r file metric _; do
    printf '  %s → %s\n' "$(basename "${file}")" "${metric}"
  done
  printf '\nFix: register the missing metric in %s, or remove the orphan\n' "${instruments_path}"
  printf '     reference from the dashboard.\n'
} >&2
exit 1
