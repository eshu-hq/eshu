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
registered_anywhere_tmp="$(mktemp)"
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
# Two sets, because the two checks ask different questions.
#
# registered_anywhere_tmp answers "is this documented metric real?" and so
# searches the whole tree. Several first-party metrics are registered in a
# dedicated *_metrics.go beside the code that emits them --
# request_metrics.go, cloud_resources_metrics.go, iac_resources_metrics.go,
# transport_auth_metrics.go. Reading only instruments.go made those look
# unregistered, so the doc rows citing them could not be validated (#5548).
rg -UPo --no-filename --glob '*.go' --glob '!**/*_test.go' \
  --glob '!**/testdata/**' --glob '!**/fixtures/**' \
  '\.(?:Int64|Float64)(?:Counter|Histogram|UpDownCounter|Gauge|ObservableGauge|ObservableCounter|ObservableUpDownCounter)\(\s*"([a-zA-Z0-9_]+)"' \
  --replace '$1' "$repo_root/go" "$repo_root/sdk" "$repo_root/examples" 2>/dev/null \
  | rg '^eshu_dp_' \
  | sort -u >"$registered_anywhere_tmp" || true

# instruments_metrics_tmp answers "is every canonical instrument documented?"
# and stays scoped to instruments.go, the canonical registry. Widening it
# would demand X1 rows for collector-family metrics that have never had them
# -- a real gap, but a pre-existing one and not this change's subject.
rg -UPo --no-filename \
  '\.(?:Int64|Float64)(?:Counter|Histogram|UpDownCounter|Gauge|ObservableGauge|ObservableCounter|ObservableUpDownCounter)\(\s*"([a-zA-Z0-9_]+)"' \
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

# metric_signal_pattern / cell_has_signal: the ONE shared definition of "does
# this metric-column cell carry a real signal", used by BOTH the new-stage
# check (3) below and the existing-row check (3b) further down. A row's
# metric column counts as covered if it names a registered eshu_dp_* metric,
# carries the No-Observability-Change: marker, or (the real X1 doc's own
# narrower convention for structured-log-key rows that are pure correlation
# identifiers, e.g. telemetry-coverage.md:755,760) starts with the literal
# "(no metric" prefix. Before this helper existed, (3)'s has_signal test and
# (3b)'s guard were two independently written regexes; (3b) had drifted to a
# bare `[ -z "$metric_cell" ]` blank-only test, so a metric cell of `TODO` or
# any other non-blank placeholder passed (3b) silently even though the
# failure message it already emitted promised exactly this pattern (#5855,
# fourth review round). Factoring both call sites onto one pattern means they
# cannot silently diverge again.
metric_signal_pattern='eshu_dp_[a-zA-Z0-9_]+|No-Observability-Change:|^\(no metric'
cell_has_signal() {
  printf '%s' "$1" | rg -q "$metric_signal_pattern"
}

# doc_row_signals_tmp: per-doc-row file-path and whether the row's
# metric column carries a real signal per cell_has_signal above. Used by the
# new-stage check to detect rows that name a new file but leave the metric
# column blank or TODO, which would defeat the "every stage must register
# telemetry" policy. Format: <file> <signal> where signal is 1 or 0.
doc_row_signals_tmp="$(mktemp)"
trap 'rm -f "$doc_required_tmp" "$doc_documented_tmp" "$doc_files_tmp" "$instruments_metrics_tmp" "$new_stages_tmp" "$tmp_diff" "$all_rows_tmp" "$required_rows_tmp" "$doc_row_signals_tmp" "$doc_buckets_tmp" "$code_buckets_tmp" "$registered_anywhere_tmp"' EXIT
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
    if cell_has_signal "$metric_col"; then
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
  if ! rg -qx "$metric" "$registered_anywhere_tmp"; then
    report="${report}  - doc references metric \`${metric}\` but no Go file registers it
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
# glob) that actually exists on disk, and carry a real metric signal. Split
# into scripts/lib/telemetry-coverage-row-check.sh to keep this script under
# the repo's file-length cap (#5855); that file is registered as its own
# trigger of the telemetry-coverage gate in specs/ci-gates.v1.yaml. See that
# file's header comment for the full rationale (row-vanishes-before-
# validation history) and the check_stage_table_rows/trim_ws/
# path_target_exists definitions it provides. check_stage_table_rows expects
# repo_root, doc_path, all_rows_tmp, and cell_has_signal (defined above) to
# already be set, and mutates $report/$drift like the checks above.
# shellcheck source=scripts/lib/telemetry-coverage-row-check.sh
source "${script_dir}/lib/telemetry-coverage-row-check.sh"
check_stage_table_rows

# (5) Registered but never emitted (#5548). A synchronous instrument on the
# Instruments struct whose field is referenced nowhere outside instruments.go
# is registered, documented, and dead: it produces no samples, so an operator
# following its X1 row to a dashboard finds an empty panel. Registration is
# not emission, and until this check existed nothing said so -- 23 such
# instruments had accumulated.
#
# Observable instruments are exempt by construction: they are written from a
# RegisterCallback inside instruments.go via o.Observe(...), so their field
# legitimately appears nowhere else.
#
# A reference, not a `.Add(`/`.Record(` call, is the signal. Several
# instruments are emitted indirectly -- passed into a struct field and
# recorded from there, as go/cmd/mcp-server/wiring.go does with
# GovernanceAuditAllowedEmitted -- and requiring a literal call site at the
# field would flag those as dead when they are not.
sync_fields_tmp="$(mktemp)"
referenced_fields_tmp="$(mktemp)"
referenced_fields_all_tmp="$(mktemp)"
rg -Uo --no-filename \
  'inst\.(\w+), err = meter\.(?:Int64|Float64)(?:Counter|Histogram|UpDownCounter|Gauge)\(' \
  --replace '$1' "$repo_root/$instruments_path" 2>/dev/null | sort -u >"$sync_fields_tmp" || true

# One rg pass over the tree with every field name in a single alternation,
# rather than one invocation per field.
#
# The per-field loop this replaces asked `rg -q` 357 times and treated any
# non-zero exit as "not referenced". rg exits 1 for no-match and 2 for an
# error, so anything that made rg fail -- an unsupported flag, an unreadable
# path -- was silently reported as a dead metric. That is a false failure in
# the direction that blocks a merge, and it fired in CI while passing locally
# (#5548 review). Reading the field list out of one match set has no exit code
# to misread, and drops the gate from 357 subprocesses to one.
#
# git grep, not rg, and deliberately so.
#
# Two rg-based versions of this scan passed locally and failed in CI, flagging
# 112 then 113 fields that are demonstrably referenced in the tree. The cause
# was never pinned down -- CI installs ripgrep from apt (14.1.0) against a
# local 15.2.0, and the two disagree about which files this search reaches.
# Rather than keep guessing at the difference, this asks git for the file set
# instead of asking a tree-walker to rediscover it. `git grep` searches tracked
# files, which is exactly what CI checks out, so the search set is the same
# everywhere and does not depend on ignore-file handling, glob semantics, or
# traversal order.
#
# -w is git grep's whole-word flag. -E is POSIX ERE, which has no \b -- using
# it silently matched nothing at all, which the empty-result guard below caught
# rather than reporting every instrument as dead.
if [ -s "$sync_fields_tmp" ]; then
  field_alternation="$(paste -sd'|' - <"$sync_fields_tmp")"
  git -C "$repo_root" grep -h -o -w -E "(${field_alternation})" -- \
    go sdk examples 2>/dev/null \
    | sort -u >"$referenced_fields_all_tmp" || true
  # Drop the registration site and test files. Filtering after the search
  # rather than with pathspecs keeps the pathspec syntax simple and its
  # behaviour obvious.
  git -C "$repo_root" grep -h -o -w -E "(${field_alternation})" -- \
    go sdk examples \
    ':(exclude)go/internal/telemetry/instruments.go' \
    ':(exclude,glob)**/*_test.go' 2>/dev/null \
    | sort -u >"$referenced_fields_tmp" || true

  # A tool failure must not read as a wall of findings. Every registered
  # instrument being unreferenced at once is not a plausible repository state;
  # it means the search did not run. Fail loudly on that instead of emitting
  # 357 confident-looking "dead metric" lines.
  if [ ! -s "$referenced_fields_tmp" ]; then
    report="${report}  - the instrument reference scan matched nothing at all across $(wc -l <"$sync_fields_tmp" | tr -d ' ') registered instruments. That is a search failure, not a repository full of dead metrics: check that git grep ran over ${repo_root}/{go,sdk,examples} and that the pattern compiled.
"
    drift=1
  else

    while IFS= read -r field; do
      [ -n "$field" ] || continue
      report="${report}  - instruments.go registers \`${field}\` but nothing outside instruments.go references it: registered and documented, never emitted
"
      drift=1
    done < <(comm -23 "$sync_fields_tmp" "$referenced_fields_tmp")
  fi
fi
rm -f "$sync_fields_tmp" "$referenced_fields_tmp" "$referenced_fields_all_tmp"

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
