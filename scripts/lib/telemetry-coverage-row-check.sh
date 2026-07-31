#!/usr/bin/env bash
#
# telemetry-coverage-row-check.sh — X2 check (3b): validates that every row
# in the X1 doc's stage tables names a real, existing file/glob target and
# carries a non-blank, well-formed metric column. Split out of
# scripts/verify-telemetry-coverage.sh to keep that script under the repo's
# 500-line file cap (#5855).
#
# Sourced by scripts/verify-telemetry-coverage.sh; not meant to run
# standalone. check_stage_table_rows expects repo_root, doc_path, and
# all_rows_tmp to already be set by the caller, and the caller's
# cell_has_signal function to already be defined (the caller defines it
# before check (3) runs, ahead of where this file is sourced/called) — it
# mutates the caller's global `report` and `drift` variables, the same
# pattern scripts/lib/telemetry-coverage-bucket-check.sh's
# check_histogram_bucket_agreement uses.
#
# Registered as its own trigger of the telemetry-coverage gate in BOTH
# specs/ci-gates.v1.yaml (the registry) and the `telemetry:` dorny
# path-filter block in .github/workflows/static-contract-gates.yml (the
# actual CI enforcement), so a change to this file re-runs the gate. Both
# registrations are required: dorny/paths-filter is gitignore-style, where a
# single `*` does not cross `/`, so a registry-only trigger does not make CI
# select this gate for a PR confined to this file (the exact gap #5855's
# third review round found for telemetry-coverage-bucket-check.sh). The
# existing `scripts/lib/telemetry-coverage-*.sh` dorny pattern already
# matches this file's name, so no new workflow pattern is needed here — only
# the registry trigger is new.

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

check_stage_table_rows() {
while IFS='|' read -ra cols; do
  n="${#cols[@]}"
  # No count-based skip here on purpose. A prior version had
  # `[ "$n" -ge 2 ] || continue`, which treated any row splitting to fewer
  # than 2 fields as "not a row" and skipped it before ANY check below ran.
  # A doc line that is exactly the single byte `|` matches the `^\|`
  # selector above but splits to a ONE-element array (`IFS='|' read -ra`
  # drops the trailing empty field when there is no byte after the lone
  # delimiter), so it silently vanished -- the fourth instance of the same
  # "row vanishes before validation" defect across three review rounds
  # (blank stage cell, blank/comma-only path cell, now a bare pipe) (#5855).
  # Every row the selector matches must now reach the content
  # classification below; a row too short to be a real stage-table or
  # histogram-buckets row falls into the `n -lt 5` malformed branch just
  # like a truncated stage-table row already does, and gets reported
  # instead of disappearing. The `${cols[1]:-}`/`${cols[2]:-}` defaults used
  # in that branch already tolerate a missing index for this reason.
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
  # file added since $base; an EXISTING row's metric column going blank OR
  # holding a non-blank placeholder (e.g. `TODO`, `pending`, or any other
  # prose that is not a real eshu_dp_* metric or a recognized marker -- after
  # a bad rebase or merge, or simply never filled in) has no other guard
  # anywhere in this script and would otherwise vanish from the gate the same
  # way a blank path cell does. A prior version of this check tested only
  # `[ -z "$metric_cell" ]`, so any non-blank placeholder passed silently even
  # though the failure message below already promised this exact signal test
  # (#5855, fourth review round). Uses the same cell_has_signal helper check
  # (3) uses so the two checks cannot silently diverge.
  metric_cell="$(trim_ws "${cols[3]:-}")"
  if ! cell_has_signal "$metric_cell"; then
    report="${report}  - doc row \"${stage_name}\" in ${doc_path} is malformed: metric column is blank (expected an eshu_dp_* metric or a No-Observability-Change: marker)
"
    drift=1
  fi

  # Category column (the row's real LAST column) must be non-blank. This
  # cell had NO guard anywhere in this script -- stage name, path, and
  # metric are each checked above, but cols[4] is never even read -- even
  # though the failure message this script's caller prints
  # (verify-telemetry-coverage.sh: "Fix a malformed row missing its
  # file/glob, metric, or category column") already promises rejection of a
  # missing category (#5855 review).
  #
  # The category is read from cols[$((n-1))], the LAST array element, not
  # a hardcoded cols[4]. A row whose metric column contains an embedded,
  # unescaped pipe character (the reconciliation_status=a|b shape case 30/36
  # in the test suite exercise) splits into MORE than 5 fields, so cols[4]
  # holds a fragment of the metric prose, not the category -- a fixed-index
  # check would validate the wrong cell (silently accepting a genuinely
  # blank category whose fragment-at-cols[4] happens to be non-blank), the
  # same "wrong cell" trap the bare-pipe-in-prose fix already avoided for
  # the metric-cell check by scanning the whole cell instead of a
  # sub-string. Reading the last element is correct regardless of how many
  # embedded pipes shifted everything before it.
  category_cell="$(trim_ws "${cols[$((n - 1))]:-}")"
  if [ -z "$category_cell" ]; then
    report="${report}  - doc row \"${stage_name}\" in ${doc_path} is malformed: category column (last column) is blank
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
}
