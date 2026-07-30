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

# script_dir always resolves to where THIS script actually lives, regardless
# of ESHU_TELEMETRY_COVERAGE_REPO_ROOT below (which retargets repo_root at a
# fixture or another worktree for doc/instruments lookups, but never moves
# this script's own sibling files). Used to source scripts/lib/*.sh chunks
# so sourcing keeps working under a copied-script fixture (see case 9 in
# test-verify-telemetry-coverage.sh) exactly like the GIT_DIR-safe repo_root
# derivation below.
script_dir="$(cd "$(dirname "$0")" && pwd)"

repo_root="${ESHU_TELEMETRY_COVERAGE_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  # Derive the repo root from the script's own location, NOT
  # `git rev-parse --show-toplevel`. Git hooks (pre-commit/pre-push) export
  # GIT_DIR, and with GIT_DIR set `git -C scripts rev-parse --show-toplevel`
  # returns the -C directory (<repo>/scripts) instead of the repo root, so the
  # `$repo_root/<doc>` existence checks below fail with a false "missing". The
  # script always lives at <repo>/scripts/, so dirname/.. is the repo root and is
  # both worktree- and hook-safe.
  repo_root="$(cd "$script_dir/.." && pwd)"
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
# with a pipe, full stop -- selection must not also require the FIRST CELL
# to be non-blank. An earlier version of this regex
# (^\|[[:space:]]*[^|[:space:]]) required a non-pipe, non-space character
# right after the opening pipe, so a row with a blank stage-name cell (e.g.
# "|  | go/internal/reducer/does_not_exist.go:1 | ... |") never entered
# all_rows_tmp at all -- invisible to every downstream check, including the
# (3b) stale-target check below that would otherwise catch its nonexistent
# path (#5855, third review round: a P1 one layer upstream of the blank-path
# and blank-metric P1s, which fixed row VALIDATION but could not help a row
# that never reached validation). The header and GFM separator rows are
# deliberately NOT excluded here by position or shape -- they are excluded
# downstream, by CONTENT, in every check that classifies rows (see the
# 'file:line'/'boundary_values' and ^[-:]+$ content checks in the (3b) loop
# below), so broadening this selector cannot let them slip through as data.
all_rows_tmp="$(mktemp)"
trap 'rm -f "$doc_required_tmp" "$doc_documented_tmp" "$doc_files_tmp" "$instruments_metrics_tmp" "$new_stages_tmp" "$tmp_diff" "$all_rows_tmp"' EXIT
rg -N --no-line-number '^\|' "$repo_root/$doc_path" >"$all_rows_tmp" 2>/dev/null || true

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
trap 'rm -f "$doc_required_tmp" "$doc_documented_tmp" "$doc_files_tmp" "$instruments_metrics_tmp" "$new_stages_tmp" "$tmp_diff" "$all_rows_tmp" "$required_rows_tmp" "$doc_row_signals_tmp" "$doc_buckets_tmp" "$code_buckets_tmp"' EXIT
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

# (3b) Reverse of (3): every row in the stage tables must name a file (or
# glob) that actually exists on disk. Check (3) only fires on a *new*
# stage file with no doc row; a row whose target file was deleted or
# renamed leaves no diff signal for (3) to catch, so a stale row passes
# the gate forever (#5855).
#
# Rows are classified by CONTENT, not position or column count alone: a
# position-based cutoff derived from the histogram-buckets section-marker
# LINE NUMBER was tried first and rejected — it silently excluded any
# stage-table row placed textually after that marker (including a
# malformed one appended with no section boundary of its own). A pure
# column-count classifier (5 elements for the 4-column stage-table shape,
# 3 for the 2-column histogram-buckets shape when split on '|' with
# `IFS='|' read -ra cols`) was tried second and also rejected: a
# truncated stage-table row missing its trailing metric/category cells
# collapses to the same 3-element shape as a histogram row and vanishes
# from the gate the same way (review of #5855, narrower trigger — drop
# two trailing cells instead of appending past a marker). A row that is
# too short for the stage-table shape is now only treated as a genuine
# histogram-buckets row if its second column is recognizably that
# table's content (the literal `boundary_values` header, an all-dash/
# colon separator, or a comma-separated numeric list); anything else that
# short is reported as a malformed row rather than silently dropped. One
# doc row's metric column legitimately contains bare (unescaped) pipe
# characters in its prose
# (`reconciliation_status=not_requested|applied|suppressed_input_invalid`),
# pushing that row's field count to 7; `-ge 5` still classifies it as a
# stage-table row (its stage/path columns are unaffected, since the extra
# pipes are later in the row), whereas an exact `-eq 5` would have missed
# it.
#
# trim_ws <var> — strip leading/trailing whitespace using parameter
# expansion, so the trim itself never forks an external sed/awk process.
# Each `$(trim_ws ...)` call site below still forks a bash subshell for
# the command substitution — cheaper than an external-binary fork, but
# not free.
trim_ws() {
  local s="$1"
  s="${s#"${s%%[![:space:]]*}"}"
  s="${s%"${s##*[![:space:]]}"}"
  printf '%s' "$s"
}

# path_target_exists <path-or-glob> — true if the token names a real file
# under repo_root, or (for a token containing '*') at least one file
# matches the glob. Called directly (not via command substitution), so
# neither branch forks anything: `compgen` and `[ -f ]` are bash builtins.
# A glob only proves at least one file under it exists, not that the
# specific file implementing the row's described stage is still there
# (see the Limitations note in docs/internal/telemetry-discipline-precedent.md).
path_target_exists() {
  case "$1" in
    *'*'*)
      compgen -G "$repo_root/$1" >/dev/null 2>&1
      ;;
    *)
      [ -f "$repo_root/$1" ]
      ;;
  esac
}

while IFS='|' read -ra cols; do
  n="${#cols[@]}"
  [ "$n" -ge 2 ] || continue
  col2="$(trim_ws "${cols[2]:-}")"
  # Header row (either table shape) or a GFM separator row (plain `---`
  # or colon-alignment `:---`, `:---:`, `---:`) — recognized by content,
  # not position, so it is excluded regardless of which table it belongs
  # to or where that table sits in the doc.
  case "$col2" in
    'file:line'|'boundary_values') continue ;;
  esac
  if [[ "$col2" =~ ^[-:]+$ ]]; then
    continue
  fi

  if [ "$n" -lt 5 ]; then
    # Too few columns for the 4-column stage-table shape. Accept
    # silently only if this is genuinely histogram-buckets data: a
    # comma-separated numeric boundary list. Anything else this short
    # is a malformed/truncated row and must fail loud rather than
    # vanish from the gate.
    if [[ "$col2" =~ ^[0-9]+(\.[0-9]+)?([[:space:]]*,[[:space:]]*[0-9]+(\.[0-9]+)?)*$ ]]; then
      continue
    fi
    stage_name="$(trim_ws "${cols[1]:-}")"
    report="${report}  - doc row \"${stage_name}\" in ${doc_path} is malformed: expected 4 columns (stage, file/glob, metric, category), found $((n - 1))
"
    drift=1
    continue
  fi

  stage_name="$(trim_ws "${cols[1]}")"
  path_cell="$col2"

  # Stage-name column must be non-blank. Before the row-selection fix above,
  # a blank stage-name cell kept the whole row out of all_rows_tmp, so no
  # check anywhere ever saw it -- not this one, not the path-existence check
  # below. Now that the row reaches validation, a blank stage name must fail
  # loud on its own, independent of whether the path and metric cells happen
  # to be otherwise valid (#5855, third review round).
  if [ -z "$stage_name" ]; then
    report="${report}  - doc row in ${doc_path} is malformed: stage name (column 1) is blank
"
    drift=1
  fi

  # Metric column must carry a real signal even for a row that names no
  # *new* stage file. Check (3)'s has_signal only guards a row that names a
  # file added since $base; an EXISTING row's metric column going blank
  # (e.g. after a bad rebase or merge that keeps the row but drops the
  # cell) has no other guard anywhere in this script and would otherwise
  # vanish from the gate the same way a blank path cell does (#5855).
  metric_cell="$(trim_ws "${cols[3]:-}")"
  if [ -z "$metric_cell" ]; then
    report="${report}  - doc row \"${stage_name}\" in ${doc_path} is malformed: metric column is blank (expected an eshu_dp_* metric or a No-Observability-Change: marker)
"
    drift=1
  fi

  # A cell may name more than one target, comma-separated (e.g.
  # "contract.go:389-470, contract_z_observability_coverage.go:10"). A
  # bare filename with no directory in a later part inherits the
  # directory of the previous part in the same cell.
  #
  # checked_any tracks whether the comma-split loop below ever reached a
  # non-empty token. `IFS=',' read -ra path_parts <<<""` yields ZERO array
  # elements for a blank path_cell, so the loop below silently runs zero
  # times; a path_cell of only commas/whitespace ("," / " , ") yields
  # elements that each trim to empty and get skipped by
  # `[ -n "$part" ] || continue`, reaching the same zero-real-tokens
  # outcome through a different shape. Both are the same "vanish instead
  # of fail loud" bug the blank-path P1 finding reported: neither the
  # per-token existence check nor any other check in this script ever
  # fires for that row, so it passes forever un-anchored from a real
  # dispatcher (#5855).
  checked_any=0
  prev_dir=""
  IFS=',' read -ra path_parts <<<"$path_cell"
  for raw_part in "${path_parts[@]}"; do
    part="$(trim_ws "$raw_part")"
    [ -n "$part" ] || continue
    token="${part%% *}"
    if [[ "$token" =~ ^(.*):[0-9]+(-[0-9]+)?$ ]]; then
      token="${BASH_REMATCH[1]}"
    fi
    [ -n "$token" ] || continue
    checked_any=1
    case "$token" in
      */*) prev_dir="${token%/*}" ;;
      *) [ -n "$prev_dir" ] && token="${prev_dir}/${token}" ;;
    esac
    if ! path_target_exists "$token"; then
      report="${report}  - doc row \"${stage_name}\" in ${doc_path} names ${token}, which does not exist
"
      drift=1
    fi
  done
  if [ "$checked_any" -eq 0 ]; then
    report="${report}  - doc row \"${stage_name}\" in ${doc_path} is malformed: file/glob column is blank or names no real target
"
    drift=1
  fi
done <"$all_rows_tmp"

# (4) Histogram bucket boundary assertion. Parses documented bucket sets
# from the X1 doc's histogram-buckets section and bucket boundary
# definitions from instruments.go, normalizes both to canonical form, and
# asserts bidirectional agreement: every code bucket set must match a doc
# row, and vice versa. Split into scripts/lib/telemetry-coverage-bucket-check.sh
# to keep this script under the repo's file-length cap (#5855); that file
# is registered as its own trigger of the telemetry-coverage gate in
# specs/ci-gates.v1.yaml. check_histogram_bucket_agreement mutates
# $report/$drift like the checks above.
# shellcheck source=scripts/lib/telemetry-coverage-bucket-check.sh
source "${script_dir}/lib/telemetry-coverage-bucket-check.sh"
check_histogram_bucket_agreement

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
    printf '  - Fix or remove a doc row that names a file or glob with no matching target\n'
    printf '  - Fix a malformed row missing its file/glob, metric, or category column\n'
  } >&2
  exit 1
fi

printf 'verify-telemetry-coverage: %s and %s agree, no new untracked stages\n' "$doc_path" "$instruments_path"
