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
  # Derive the repo root from the script's own location, NOT
  # `git rev-parse --show-toplevel`. Git hooks (pre-commit/pre-push) export
  # GIT_DIR, and with GIT_DIR set `git -C scripts rev-parse --show-toplevel`
  # returns the -C directory (<repo>/scripts) instead of the repo root, so the
  # `$repo_root/<doc>` existence checks below fail with a false "missing". The
  # script always lives at <repo>/scripts/, so dirname/.. is the repo root and is
  # both worktree- and hook-safe.
  repo_root="$(cd "$(dirname "$0")/.." && pwd)"
fi

base="${ESHU_TELEMETRY_COVERAGE_BASE:-}"
if [ -z "$base" ] && [ -n "${GITHUB_BASE_REF:-}" ]; then
  git -C "$repo_root" fetch --no-tags --depth=1 origin "$GITHUB_BASE_REF" >/dev/null 2>&1 || true
  if git -C "$repo_root" rev-parse --verify "origin/$GITHUB_BASE_REF" >/dev/null 2>&1; then
    base="origin/$GITHUB_BASE_REF"
  fi
fi
if [ -z "$base" ]; then
  # Local (non-CI) runs: diff against the branch's divergence point from
  # origin/main, not HEAD~1. On a branch based on a squash-merge commit, HEAD~1
  # is the pre-merge commit, so the new-stage diff would span the MERGE's files
  # and mis-fire. The merge-base with origin/main yields only the branch's own
  # changes. CI keeps using GITHUB_BASE_REF above.
  #
  # Use the origin/main ref the clone already has rather than fetching: a slightly
  # stale base only widens the changed-file set conservatively, and avoids a
  # per-invocation network round-trip.
  if git -C "$repo_root" rev-parse --verify origin/main >/dev/null 2>&1; then
    base="$(git -C "$repo_root" merge-base origin/main HEAD 2>/dev/null || echo origin/main)"
  elif git -C "$repo_root" rev-parse --verify HEAD~1 >/dev/null 2>&1; then
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
trap 'rm -f "$doc_required_tmp" "$doc_documented_tmp" "$doc_files_tmp" "$instruments_metrics_tmp" "$new_stages_tmp" "$tmp_diff" "$all_rows_tmp" "$required_rows_tmp" "$doc_row_signals_tmp" "$doc_buckets_tmp" "$code_buckets_tmp" "$instruments_flat"' EXIT
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

# (4) Histogram bucket boundary assertion.
# Parse documented bucket sets from the X1 doc's histogram-buckets section
# and bucket boundary definitions from instruments.go. Normalize both to
# canonical form (sorted numbers, no whitespace) and assert bidirectional
# agreement: every code bucket set must match a doc row, and vice versa.
canonicalize_buckets() {
  printf '%s' "$1" | tr -d '[:space:]' | tr ',' '\n' | sort -n | paste -sd ',' -
}

doc_buckets_tmp="$(mktemp)"
code_buckets_tmp="$(mktemp)"

# 4a: Parse documented bucket sets from the histogram-buckets section.
section_line=$(rg -n '<!-- eshu:metric:section=histogram-buckets -->' "$repo_root/$doc_path" | head -1 | cut -d: -f1 || true)
if [ -n "$section_line" ]; then
  next_section_line=$(rg -n '<!-- eshu:metric:section=' "$repo_root/$doc_path" | \
    awk -F: -v s="$section_line" '$1 > s {print $1; exit}' || true)
  [ -z "$next_section_line" ] && next_section_line=$(( $(wc -l < "$repo_root/$doc_path") + 1 ))
  sed -n "$((section_line + 1)),$((next_section_line - 1))p" "$repo_root/$doc_path" | \
    while IFS= read -r line; do
      printf '%s' "$line" | rg -q '^\|[-:|[:space:]]+\|' && continue
      if printf '%s' "$line" | rg -q '^\|.*\|.*[0-9].*\|'; then
        boundaries=$(printf '%s' "$line" | sed -n 's/^|[^|]*|\([^|]*\)|$/\1/p')
        if [ -n "$boundaries" ]; then
          boundaries=$(printf '%s' "$boundaries" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
          canonicalize_buckets "$boundaries"
        fi
      fi
    done | sort -u >"$doc_buckets_tmp" 2>/dev/null || true
fi

# 4b: Parse bucket boundary definitions from instruments.go.
# rg --replace is line-oriented even with -U (multiline), so a bucket list
# split across lines produces one output line per number rather than one
# combined capture. Collapse the file to a single line first (remove
# newlines inside []float64{...} and WithExplicitBucketBoundaries(...)
# blocks), then extract the sets with single-line regexps.
: >"$code_buckets_tmp"
instruments_flat="$(mktemp)"
# python3 one-liner: join lines inside matching bracket pairs so each
# bucket definition becomes a single line, then extract.
python3 -c "
import re, sys
with open(sys.argv[1]) as f:
    text = f.read()

# Collapse newlines inside []float64{...} and WithExplicitBucketBoundaries(...)
# blocks so each becomes a single line.
text = re.sub(r'\[\]float64\{[^}]+\}', lambda m: m.group(0).replace('\n', ' '), text)
text = re.sub(r'WithExplicitBucketBoundaries\([^)]+\)', lambda m: m.group(0).replace('\n', ' '), text)

# Extract named variables: = []float64{...}
for m in re.finditer(r'=\s*\[\]float64\{([^}]+)\}', text):
    raw = m.group(1).strip()
    sys.stdout.write(raw + '\n')

# Extract inline literals: WithExplicitBucketBoundaries(N, N, ...)
for m in re.finditer(r'WithExplicitBucketBoundaries\(([^)]+)\)', text):
    raw = m.group(1).strip()
    if not re.search(r'[a-zA-Z_]', raw):  # skip variable references
        sys.stdout.write(raw + '\n')
" "$repo_root/$instruments_path" 2>/dev/null | \
  while IFS= read -r raw_set; do
    [ -n "$raw_set" ] && canonicalize_buckets "$raw_set" >>"$code_buckets_tmp"
  done || true
sort -u -o "$code_buckets_tmp" "$code_buckets_tmp"

# 4c: Bidirectional assertion.
# Every code bucket set must have a matching documented set.
while IFS= read -r code_set; do
  [ -n "$code_set" ] || continue
  if ! rg -qx "$code_set" "$doc_buckets_tmp"; then
    report="${report}  - bucket set [${code_set}] is in ${instruments_path} but not documented in ${doc_path}\n"
    drift=1
  fi
done <"$code_buckets_tmp"

# Every documented bucket set must have a matching code definition.
while IFS= read -r doc_set; do
  [ -n "$doc_set" ] || continue
  if ! rg -qx "$doc_set" "$code_buckets_tmp"; then
    report="${report}  - bucket set [${doc_set}] is documented in ${doc_path} but not defined in ${instruments_path}\n"
    drift=1
  fi
done <"$doc_buckets_tmp"

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
