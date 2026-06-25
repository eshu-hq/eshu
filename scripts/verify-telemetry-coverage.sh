#!/usr/bin/env bash
#
# verify-telemetry-coverage.sh — fail if docs/public/observability/telemetry-coverage.md
# drifts from go/internal/telemetry/instruments.go, or if a new pipeline stage
# is added under go/internal/ or go/cmd/collector-* without a corresponding
# row in the X1 doc.
#
# This is the X2 static-analysis gate. It is the load-bearing piece of the
# "every pipeline stage must register telemetry" policy in
# docs/internal/agent-guide.md:120-146. Without this script the policy is
# human-enforced and the #3633 failure class (defined-but-never-registered
# counters) recurs.
#
# Exit 0 on success; non-zero with a per-stage diff on drift.
set -euo pipefail

repo_root="${ESHU_TELEMETRY_COVERAGE_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
    || (cd "$(dirname "$0")/.." && pwd))"
fi

base="${ESHU_TELEMETRY_COVERAGE_BASE:-}"
if [ -z "$base" ] && [ -n "${GITHUB_BASE_REF:-}" ]; then
  git -C "$repo_root" fetch --no-tags --depth=1 origin "$GITHUB_BASE_REF" >/dev/null 2>&1 || true
  if git -C "$repo_root" rev-parse --verify "origin/$GITHUB_BASE_REF" >/dev/null 2>&1; then
    base="origin/$GITHUB_BASE_REF"
  fi
fi
if [ -z "$base" ]; then
  if git -C "$repo_root" rev-parse --verify HEAD~1 >/dev/null 2>&1; then
    base="HEAD~1"
  else
    printf 'verify-telemetry-coverage: no base commit available, skipping\n'
    exit 0
  fi
fi
# If the caller passed a base ref but it does not resolve in this repo
# (e.g. the test fixture has only one commit), skip rather than fail the
# new-stage diff. The doc/instruments checks below still run.
if ! git -C "$repo_root" rev-parse --verify "$base" >/dev/null 2>&1; then
  printf 'verify-telemetry-coverage: base ref %s is not a valid revision in this repo, skipping stage-diff check\n' "$base"
  base=""
fi

doc_path="docs/public/observability/telemetry-coverage.md"
instruments_path="go/internal/telemetry/instruments.go"

if [ ! -f "$repo_root/$doc_path" ]; then
  printf 'verify-telemetry-coverage: %s is missing\n' "$doc_path" >&2
  exit 1
fi
if [ ! -f "$repo_root/$instruments_path" ]; then
  printf 'verify-telemetry-coverage: %s is missing\n' "$instruments_path" >&2
  exit 1
fi

doc_required_tmp="$(mktemp)"
doc_documented_tmp="$(mktemp)"
doc_files_tmp="$(mktemp)"
instruments_metrics_tmp="$(mktemp)"
new_stages_tmp="$(mktemp)"
tmp_diff="$(mktemp)"
trap 'rm -f "$doc_required_tmp" "$doc_documented_tmp" "$doc_files_tmp" "$instruments_metrics_tmp" "$new_stages_tmp" "$tmp_diff"' EXIT

# Extract all table rows from the X1 doc. A "row" is any line that starts
# with a pipe after optional whitespace, AND is not the header separator
# (a line made of pipes, dashes, and colons).
all_rows_tmp="$(mktemp)"
trap 'rm -f "$doc_required_tmp" "$doc_documented_tmp" "$doc_files_tmp" "$instruments_metrics_tmp" "$new_stages_tmp" "$tmp_diff" "$all_rows_tmp"' EXIT
rg -N --no-line-number '^\|[[:space:]]*[^|[:space:]]' "$repo_root/$doc_path" >"$all_rows_tmp" 2>/dev/null || true

# doc_documented_tmp: every eshu_dp_* name mentioned anywhere in a table
# row. Used for the instruments.go -> doc check (a registered metric must
# be mentioned in the doc, in any form).
rg -o 'eshu_dp_[a-zA-Z0-9_]+' "$all_rows_tmp" 2>/dev/null | sort -u >"$doc_documented_tmp" || true

# doc_required_tmp: every eshu_dp_* name that must be registered in
# instruments.go. Excludes metric names that appear ONLY inside a row whose
# metric column starts with No-Observability-Change:, because those names
# describe counters that the X1 doc explicitly retires. The marker names
# still count as documented (so the inverse check passes for them), but
# the script does not require them to be registered.
required_rows_tmp="$(mktemp)"
trap 'rm -f "$doc_required_tmp" "$doc_documented_tmp" "$doc_files_tmp" "$instruments_metrics_tmp" "$new_stages_tmp" "$tmp_diff" "$all_rows_tmp" "$required_rows_tmp"' EXIT
rg -v 'No-Observability-Change:' "$all_rows_tmp" >"$required_rows_tmp" 2>/dev/null || true
rg -o 'eshu_dp_[a-zA-Z0-9_]+' "$required_rows_tmp" 2>/dev/null | sort -u >"$doc_required_tmp" || true

# doc_files_tmp: file:line dispatcher column. Replaced by
# doc_row_signals_tmp below; kept as a debug artifact for callers that
# want to inspect which file:line entries the parser saw.
rg -N --no-line-number '^\|[[:space:]]*[^|]+\|[[:space:]]*([^|:|[:space:]]+)' \
  --replace '$1' "$all_rows_tmp" >"$doc_files_tmp" 2>/dev/null || true
sort -u -o "$doc_files_tmp" "$doc_files_tmp"

# instruments_metrics_tmp: every eshu_dp_* name registered in
# go/internal/telemetry/instruments.go. We accept any otel/metric
# constructor whose first argument is a string literal. PCRE2 mode (-P)
# is required so \s can match across newlines between the constructor
# open paren and the metric name. The set below covers the constructors
# used by Eshu today (Counter, Histogram, ObservableGauge, Gauge, plus
# the UpDownCounter/ObservableCounter variants for forward compatibility).
rg -UPo '\.(?:Int64|Float64)(?:Counter|Histogram|UpDownCounter|Gauge|ObservableGauge|ObservableCounter|ObservableUpDownCounter)\(\s*"([a-zA-Z0-9_]+)"' \
  --replace '$1' "$repo_root/$instruments_path" 2>/dev/null \
  | rg '^eshu_dp_' \
  | sort -u >"$instruments_metrics_tmp" || true

# new_stages_tmp: pipeline-stage source files added since $base. A
# "stage" is any *.go file that did not exist at $base AND lives under
# a directory the X1 doc treats as a stage owner: collector, reducer,
# projector, correlation, content shape, or a collector-* command
# package. If the base ref is empty (caller passed an unresolvable
# ref, or the repo is a single-commit fixture) skip the diff entirely.
: >"$new_stages_tmp"
if [ -n "$base" ]; then
  if git -C "$repo_root" diff --name-only --diff-filter=A "$base"...HEAD >"$tmp_diff" 2>/dev/null; then
    :
  else
    git -C "$repo_root" diff --name-only --diff-filter=A "$base" HEAD >"$tmp_diff"
  fi
  while IFS= read -r file; do
    [ -n "$file" ] || continue
    case "$file" in
      *_test.go|*_bench_test.go|*/testdata/*|*/vendor/*|*/doc.go) continue ;;
    esac
    # A stage is a new *.go source file (see comment above). Restrict every
    # stage-owner directory to *.go so non-Go additions — package docs, README,
    # AGENTS, evidence-*.md — are never mistaken for a new pipeline stage.
    case "$file" in
      go/internal/collector/*.go) ;;
      go/internal/reducer/*.go) ;;
      go/internal/projector/*.go) ;;
      go/internal/correlation/*.go) ;;
      go/internal/content/shape/*.go) ;;
      go/cmd/collector-*/*.go) ;;
      *) continue ;;
    esac
    printf '%s\n' "$file" >>"$new_stages_tmp"
  done <"$tmp_diff"
  sort -u -o "$new_stages_tmp" "$new_stages_tmp"
fi

# doc_row_signals_tmp: per-doc-row file-path and whether the row's
# metric column carries a real signal (an eshu_dp_* metric or a
# No-Observability-Change: marker). Used by the new-stage check to
# detect rows that name a new file but leave the metric column blank
# or TODO, which would defeat the "every stage must register telemetry"
# policy. Format: <file> <signal> where signal is 1 or 0.
doc_row_signals_tmp="$(mktemp)"
trap 'rm -f "$doc_required_tmp" "$doc_documented_tmp" "$doc_files_tmp" "$instruments_metrics_tmp" "$new_stages_tmp" "$tmp_diff" "$all_rows_tmp" "$required_rows_tmp" "$doc_row_signals_tmp"' EXIT
: >"$doc_row_signals_tmp"
if [ -s "$all_rows_tmp" ]; then
  while IFS= read -r row; do
    [ -n "$row" ] || continue
    file_path="$(printf '%s' "$row" \
      | rg -o '^\|[[:space:]]*[^|]+\|[[:space:]]*([^|:|[:space:]]+)(?::[0-9]+)?[[:space:]]*\|' \
        --replace '$1' 2>/dev/null || true)"
    [ -n "$file_path" ] || continue
    metric_col="$(printf '%s' "$row" \
      | rg -o '^\|[[:space:]]*[^|]+\|[[:space:]]*[^|]+\|[[:space:]]*([^|]+)' \
        --replace '$1' 2>/dev/null || true)"
    if printf '%s' "$metric_col" | rg -q 'eshu_dp_[a-zA-Z0-9_]+|No-Observability-Change:'; then
      signal=1
    else
      signal=0
    fi
    printf ' %s %s\n' "$file_path" "$signal" >>"$doc_row_signals_tmp"
  done <"$all_rows_tmp"
fi

drift=0
report=""

# (1) Doc mentions a metric that is not registered in instruments.go.
# This is the spec's "missing metric registration" failure.
while IFS= read -r metric; do
  [ -n "$metric" ] || continue
  if ! rg -qx "$metric" "$instruments_metrics_tmp"; then
    report="${report}  - doc references metric \`${metric}\` but it is not registered in ${instruments_path}
"
    drift=1
  fi
done <"$doc_required_tmp"

# (2) instruments.go registers a metric that is not mentioned in the doc.
# This is the #3633 defined-but-never-registered drift class. The check
# uses doc_documented_tmp (all names in the doc, including marker prose)
# so retired names that the marker explicitly names still pass.
while IFS= read -r metric; do
  [ -n "$metric" ] || continue
  if ! rg -qx "$metric" "$doc_documented_tmp"; then
    report="${report}  - ${instruments_path} registers \`${metric}\` but the X1 doc has no row that mentions it
"
    drift=1
  fi
done <"$instruments_metrics_tmp"

# (3) A new pipeline-stage source file was added. The doc must have a
# row that names the file AND the row's metric column must carry a
# real signal (an eshu_dp_* metric or a No-Observability-Change:
# marker). A row that names the file but leaves the metric column
# blank or TODO would defeat the "every stage must register telemetry"
# policy.
while IFS= read -r file; do
  [ -n "$file" ] || continue
  matching_rows="$(rg -F " $file" "$doc_row_signals_tmp" 2>/dev/null || true)"
  if [ -z "$matching_rows" ]; then
    report="${report}  - new stage file ${file} is not covered by any row in ${doc_path}
"
    drift=1
    continue
  fi
  has_signal=0
  while IFS= read -r m; do
    [ -n "$m" ] || continue
    sig="${m##* }"
    if [ "$sig" = "1" ]; then
      has_signal=1
      break
    fi
  done <<<"$matching_rows"
  if [ "$has_signal" -eq 0 ]; then
    report="${report}  - new stage file ${file} is mentioned in ${doc_path} but the matching row has no eshu_dp_* metric or No-Observability-Change: marker
"
    drift=1
  fi
done <"$new_stages_tmp"

if [ "$drift" -ne 0 ]; then
  {
    printf 'verify-telemetry-coverage: telemetry coverage drift detected\n'
    printf '\nDrift between %s and %s (base: %s):\n' "$doc_path" "$instruments_path" "$base"
    printf '%s' "$report"
    printf '\nFix one of:\n'
    printf '  - Add a row to %s that names the new stage and the registered metric(s)\n' "$doc_path"
    printf '  - Add the missing metric to %s, OR remove it if it is dead code\n' "$instruments_path"
    printf '  - Replace the metric column with a No-Observability-Change: marker that names\n'
    printf '    the existing signal that already covers the stage\n'
  } >&2
  exit 1
fi

printf 'verify-telemetry-coverage: %s and %s agree, no new untracked stages\n' "$doc_path" "$instruments_path"
