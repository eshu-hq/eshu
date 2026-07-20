#!/usr/bin/env bash
#
# verify-docs-contradiction.sh - advisory gate that flags a docs/public page
# self-contradicting about the same fact (#5340).
#
# Two checks, both driven by a single awk pass over every non-generated
# docs/public/**/*.md file (scripts/lib/docs-contradiction-checks.awk):
#
#   1. Modal-polarity with a shared-subject anchor: a page is flagged only
#      when the SAME specific subject — a backticked code span, a bare
#      capability-ID string, or a stopword-free 3+-word n-gram — appears near
#      both a positive-polarity phrase (is/are/now implemented, supported, is
#      available) and a negative-polarity phrase (not yet implemented, not
#      implemented, planned, unsupported). Mere co-occurrence of a positive
#      and a negative word in the same file is NOT enough — the anchor
#      requirement is the false-positive guard (see the awk file's header for
#      the worked php.md Laravel-row counter-example).
#   2. Duplicate table-row key: within one contiguous markdown table block
#      (reset at a blank line or a line that does not start with "|"), the
#      same first-column cell value must not repeat.
#
# Scope is deliberately unconditional: EVERY docs/public/**/*.md page is
# scanned, not just docs-catalog "reference"/"proof" types the way
# verify-docs-prose-quality.sh filters — a reference page (php.md,
# collector-reducer-readiness.md) is exactly where a stale capability-status
# paragraph is most likely to drift from a table row that was kept current, so
# filtering by page type would miss the two contradictions this gate exists
# to catch.
#
# Advisory first (like docs-prose-quality): reports findings but exits 0
# unless DOCS_CONTRADICTION_ENFORCE=true. See
# docs/public/reference/docs-contradiction.md.
#
# Burn-down baseline: scripts/docs-contradiction-baseline.txt lists known
# findings as "<doc-relpath> <kind>:<slug>" pairs (mirroring
# scripts/docs-refs-baseline.txt). A finding NOT in the baseline is reported
# (and fails the gate under enforcement); one already in the baseline is
# silent; a baselined pair whose page gets fixed is simply not a finding
# anymore. Regenerate with `bash scripts/verify-docs-contradiction.sh -update`.
#
# Runs under both macOS's stock /bin/bash 3.2 and Homebrew bash >= 5.1: no
# `declare -A`, no bash arrays for the baseline diff (comm(1) does the set
# difference), and no heredoc/`<<<` here-string carries a body of consequence
# (#5019/#4718 class of hang).
set -euo pipefail

tool_root="$(cd "$(dirname "$0")/.." && pwd)"
repo_root="${ESHU_DOCS_CONTRADICTION_REPO_ROOT:-${tool_root}}"
docs_root="${repo_root}/docs/public"
# lib_awk defaults to the SCRIPT's own location, never
# ESHU_DOCS_CONTRADICTION_REPO_ROOT: the awk library is part of the tool, not
# part of the docs tree under test, so a test that points docs_root at a
# scratch fixture tree still finds the real checker logic.
# ESHU_DOCS_CONTRADICTION_LIB_AWK overrides only the checker library path, for
# the test mirror's RED->GREEN proof (run the same fixture through the current
# and a prior-logic copy of the library); production callers never set it.
lib_awk="${ESHU_DOCS_CONTRADICTION_LIB_AWK:-${tool_root}/scripts/lib/docs-contradiction-checks.awk}"
baseline_path="${ESHU_DOCS_CONTRADICTION_BASELINE_PATH:-${repo_root}/scripts/docs-contradiction-baseline.txt}"
enforce="${DOCS_CONTRADICTION_ENFORCE:-false}"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

log() {
  printf 'docs-contradiction: %s\n' "$*" >&2
}

usage() {
  printf 'usage: %s [-update]\n' "${0##*/}"
  printf '  (no args)  check docs/public/**/*.md self-contradictions against %s\n' "${baseline_path}"
  printf '  -update    regenerate the baseline from the current tree\n'
}

command -v rg >/dev/null 2>&1 || {
  log "missing required tool: rg"
  exit 1
}
command -v awk >/dev/null 2>&1 || {
  log "missing required tool: awk"
  exit 1
}
[[ -d "${docs_root}" ]] || {
  log "docs root not found: ${docs_root}"
  exit 1
}
[[ -f "${lib_awk}" ]] || {
  log "missing awk library: ${lib_awk}"
  exit 1
}

is_generated_doc() {
  rg -q -i '^<!--[[:space:]]*(generated|.*generated from|.*do not edit by hand)' "$1"
}

# scannable_files lists every docs/public/**/*.md page, relative to
# docs_root, excluding generated/do-not-edit pages, sorted for determinism.
scannable_files() {
  local rel file
  while IFS= read -r rel; do
    [ -z "${rel}" ] && continue
    file="${docs_root}/${rel}"
    if is_generated_doc "${file}"; then
      continue
    fi
    printf '%s\n' "${rel}"
  done < <(cd "${docs_root}" && rg --files -g '*.md' | LC_ALL=C sort)
}

# scan_findings runs the single awk pass over every scannable file (run from
# inside docs_root so FILENAME is already the doc-relative path) and emits
# one detailed finding line per contradiction, sorted for determinism.
scan_findings() {
  local files_list="${tmp_dir}/files.txt"
  scannable_files >"${files_list}"
  if [[ ! -s "${files_list}" ]]; then
    log "no scannable docs/public/**/*.md pages found"
    return 0
  fi
  (
    cd "${docs_root}"
    # shellcheck disable=SC2046 # word-splitting is intentional: one argument
    # per newline-delimited relative path, none of which contain spaces.
    awk -f "${lib_awk}" $(cat "${files_list}")
  ) | LC_ALL=C sort -u
}

# baseline_key_of extracts the "<relpath> <kind>:<slug>" baseline key from a
# detailed finding line (which carries extra pos=/neg=/anchor="..." or
# first_line=/dup_line=/key="..." detail after the key).
baseline_key_of() {
  awk '{ print $1, $2 }'
}

validate_baseline() {
  local path="$1"
  [[ -e "${path}" ]] || return 0
  if [[ ! -r "${path}" ]]; then
    log "baseline not readable: ${path}"
    return 1
  fi
  local lineno=0
  local line
  local nf
  while IFS= read -r line || [[ -n "${line}" ]]; do
    lineno=$((lineno + 1))
    [[ -z "${line}" ]] && continue
    case "${line}" in
      '#'*) continue ;;
    esac
    nf="$(printf '%s\n' "${line}" | awk '{ print NF }')"
    if [[ "${nf}" -ne 2 ]]; then
      log "baseline malformed at line ${lineno}: expected \"<doc-relpath> <kind>:<slug>\", got: ${line}"
      return 1
    fi
    case "${line}" in
      *' polarity:'* | *' duplicate-row:'*) ;;
      *)
        log "baseline malformed at line ${lineno}: kind must be polarity: or duplicate-row:, got: ${line}"
        return 1
        ;;
    esac
  done <"${path}"
  return 0
}

baseline_pairs() {
  local path="$1"
  [[ -f "${path}" ]] || return 0
  rg -v '^[[:space:]]*(#.*)?$' "${path}" 2>/dev/null | LC_ALL=C sort -u || true
}

baseline_header() {
  printf '%s\n' '# scripts/docs-contradiction-baseline.txt'
  printf '%s\n' '#'
  printf '%s\n' '# Burn-down baseline for scripts/verify-docs-contradiction.sh (#5340).'
  printf '%s\n' '# Every docs/public/**/*.md page is scanned for a self-contradiction:'
  printf '%s\n' '# modal-polarity findings (a shared subject anchor near both a positive and'
  printf '%s\n' '# a negative implementation-status phrase) and duplicate markdown table-row'
  printf '%s\n' '# keys. A finding NOT listed below fails the gate under enforcement, passes'
  printf '%s\n' '# one that is, and treats a baselined pair whose page gets fixed as burn-down'
  printf '%s\n' '# progress (shrinking the file is fine; a pair is either a finding or removed).'
  printf '%s\n' '#'
  printf '%s\n' '# Regenerate with:'
  printf '%s\n' '#   bash scripts/verify-docs-contradiction.sh -update'
  printf '%s\n' '#'
  printf '%s\n' '# <doc-page-relpath> <kind>:<slug>'
}

cmd_update() {
  local findings="${tmp_dir}/findings.txt"
  scan_findings >"${findings}"
  local tmp="${tmp_dir}/new-baseline.txt"
  {
    baseline_header
    baseline_key_of <"${findings}" | LC_ALL=C sort -u
  } >"${tmp}"
  mkdir -p "$(dirname "${baseline_path}")"
  cp "${tmp}" "${baseline_path}"
  local n
  n="$(rg -c '^[^#[:space:]]' "${baseline_path}" 2>/dev/null || printf '0')"
  log "baseline updated: ${n} finding(s) at ${baseline_path}"
}

cmd_check() {
  validate_baseline "${baseline_path}" || exit 1

  local findings="${tmp_dir}/findings.txt"
  scan_findings >"${findings}"
  baseline_key_of <"${findings}" | LC_ALL=C sort -u >"${tmp_dir}/keys.txt"
  baseline_pairs "${baseline_path}" >"${tmp_dir}/baseline.txt"
  comm -23 "${tmp_dir}/keys.txt" "${tmp_dir}/baseline.txt" >"${tmp_dir}/new-keys.txt" || true

  local new_count
  new_count="$(awk 'NF' "${tmp_dir}/new-keys.txt" | wc -l | tr -d ' ')"
  if [[ "${new_count}" -gt 0 ]]; then
    while IFS= read -r finding; do
      [ -z "${finding}" ] && continue
      case "${finding}" in
        *' polarity:'* | *' duplicate-row:'*)
          local key
          key="$(printf '%s\n' "${finding}" | baseline_key_of)"
          if rg -qF "${key}" "${tmp_dir}/new-keys.txt" 2>/dev/null; then
            log "${finding}"
          fi
          ;;
      esac
    done <"${findings}"
    log "${new_count} contradiction finding(s) not in the baseline"
    if [[ "${enforce}" == "true" ]]; then
      return 1
    fi
    log "ADVISORY (DOCS_CONTRADICTION_ENFORCE!=true) - not failing"
    return 0
  fi

  local finding_count baseline_count
  finding_count="$(awk 'NF' "${tmp_dir}/keys.txt" | wc -l | tr -d ' ')"
  baseline_count="$(awk 'NF' "${tmp_dir}/baseline.txt" | wc -l | tr -d ' ')"
  log "OK: ${finding_count} finding(s) checked, ${baseline_count} baselined"
  return 0
}

main() {
  case "${1:-}" in
    "") cmd_check ;;
    -update) cmd_update ;;
    -h | --help) usage ;;
    *)
      log "unknown argument: $1"
      usage >&2
      exit 2
      ;;
  esac
}

main "$@"
