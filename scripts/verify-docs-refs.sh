#!/usr/bin/env bash
#
# verify-docs-refs.sh - fail a docs page that cites a scripts/*.sh|*.py|*.awk
# path that does not exist in this repo (#5125 workstream 3, deliverable A).
#
# Scope (v1, deliberately narrow): script-PATH existence only. No flag or
# env-var checking inside the cited script — precision first, since false
# positives are how a gate gets muted/ignored instead of trusted.
#
# Discovery regex: \bscripts/[A-Za-z0-9_./-]+\.(sh|py|awk)\b, run against
# every docs/public/**/*.md file, matched over the raw line text so it finds
# a citation whether it sits inside inline code (`scripts/foo.sh`) or plain
# prose. A fenced code block that is clearly EXAMPLE OUTPUT rather than an
# instruction still counts — a cited path is a cited path regardless of
# which markdown construct it sits in, so this script does NOT special-case
# ``` fences.
#
# Placeholder exclusion: a candidate containing <, >, *, or the ellipsis
# character (…) is skipped — patterns like `scripts/<name>.sh`,
# `scripts/*.sh`, or `scripts/verify-*.sh` are documentation shorthand, not a
# citation of a real path. The discovery regex's own character class already
# excludes all four of those characters from ever appearing inside a match
# (none of them is in [A-Za-z0-9_./-]), so this check is normally a no-op;
# it is kept explicit so the exclusion rule survives if the discovery regex
# is ever loosened to catch quoted or bracketed citations.
#
# Burn-down baseline: scripts/docs-refs-baseline.txt lists known dead
# references as "<doc-relpath> <missing-script-path>" pairs (mirroring the
# <path> <count> shape of scripts/heredoc-budget-baseline.txt /
# go/cmd/heredoc-budget). A dead reference NOT in the baseline fails the
# gate; one already in the baseline passes; a baselined pair whose script
# gets created is simply not dead anymore (shrinking is progress, never a
# failure). Regenerate with `bash scripts/verify-docs-refs.sh -update`.
#
# Runs under both macOS's stock /bin/bash 3.2 and Homebrew bash >= 5.1: no
# `declare -A` (bash 4+), no bash arrays for the baseline diff (comm(1) does
# the set difference instead), and no heredoc or `<<<` here-string carries
# any body — every multi-line body here is either a small fixed printf
# sequence or a process-substituted `rg`/`comm` pipeline, so the bash 5.1+
# heredoc-pipe deadlock (#5019/#5074) and the `<<<`-into-while-read hang
# (#4718) cannot occur.
set -euo pipefail

repo_root="${ESHU_DOCS_REFS_REPO_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
docs_root="${repo_root}/docs/public"
baseline_path="${ESHU_DOCS_REFS_BASELINE_PATH:-${repo_root}/scripts/docs-refs-baseline.txt}"

# One process-wide scratch dir, cleaned up on EXIT. A function-local
# `trap ... RETURN` is NOT used for this: bash traps are process-global, not
# function-scoped, so a RETURN trap set inside cmd_check would keep firing
# on every later function return (including main's own), reading a now
# out-of-scope `local` variable and aborting under `set -u`.
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

log() {
  printf 'verify-docs-refs: %s\n' "$*" >&2
}

usage() {
  printf 'usage: %s [-update]\n' "${0##*/}"
  printf '  (no args)  check docs/public/**/*.md script citations against %s\n' "${baseline_path}"
  printf '  -update    regenerate the baseline from the current tree\n'
}

command -v rg >/dev/null 2>&1 || {
  log "missing required tool: rg"
  exit 1
}
[[ -d "${docs_root}" ]] || {
  log "docs root not found: ${docs_root}"
  exit 1
}

# scan_citations emits one "<doc-relpath> <script-path>" line per unique
# citation found across every docs/public/**/*.md page, sorted for
# determinism. Reads never use a heredoc or here-string; the inner loop is
# fed by process substitution so no pipe-body-size limit ever applies.
scan_citations() {
  (cd "${docs_root}" && rg --files -g '*.md') | LC_ALL=C sort | while IFS= read -r rel; do
    [ -z "${rel}" ] && continue
    file="${docs_root}/${rel}"
    while IFS= read -r match; do
      [ -z "${match}" ] && continue
      case "${match}" in
        *'<'* | *'>'* | *'*'* | *'…'*) continue ;;
      esac
      printf '%s %s\n' "${rel}" "${match}"
    done < <(rg -o --no-filename '\bscripts/[A-Za-z0-9_./-]+\.(sh|py|awk)\b' "${file}" 2>/dev/null || true)
  done | LC_ALL=C sort -u
}

# dead_citations filters scan_citations down to pairs whose script path does
# not exist in the repo.
dead_citations() {
  while IFS=' ' read -r rel path; do
    [ -z "${rel}" ] && continue
    [[ -f "${repo_root}/${path}" ]] || printf '%s %s\n' "${rel}" "${path}"
  done < <(scan_citations)
}

# validate_baseline fails closed: a baseline that exists but cannot be read,
# or that contains a non-comment/non-blank line that is not exactly
# "<doc-relpath> <script-path>", is a registry bug, not an empty baseline.
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
      log "baseline malformed at line ${lineno}: expected \"<doc-relpath> <script-path>\", got: ${line}"
      return 1
    fi
  done <"${path}"
  return 0
}

# baseline_pairs emits the baseline's data lines (comments/blanks stripped),
# sorted for a deterministic comm(1) diff. Assumes validate_baseline already
# passed.
baseline_pairs() {
  local path="$1"
  [[ -f "${path}" ]] || return 0
  rg -v '^[[:space:]]*(#.*)?$' "${path}" 2>/dev/null | LC_ALL=C sort -u || true
}

baseline_header() {
  printf '%s\n' '# scripts/docs-refs-baseline.txt'
  printf '%s\n' '#'
  printf '%s\n' '# Burn-down baseline for scripts/verify-docs-refs.sh (#5125 workstream 3).'
  printf '%s\n' '# Every docs/public/**/*.md page is scanned for a cited scripts/*.sh,'
  printf '%s\n' '# *.py, or *.awk path; a citation whose path does not exist in the repo'
  printf '%s\n' '# is a dead reference. The gate fails a dead reference that is NOT listed'
  printf '%s\n' '# below, passes one that is, and treats a baselined pair whose script'
  printf '%s\n' '# later gets created as burn-down progress (shrinking the file is fine;'
  printf '%s\n' '# growing it past the current dead-reference count for an existing pair'
  printf '%s\n' '# is not possible — a pair is either dead or it is removed).'
  printf '%s\n' '#'
  printf '%s\n' '# Regenerate with:'
  printf '%s\n' '#   bash scripts/verify-docs-refs.sh -update'
  printf '%s\n' '#'
  printf '%s\n' '# <doc-page-relpath> <missing-script-path>'
}

cmd_update() {
  local tmp="${tmp_dir}/new-baseline.txt"
  {
    baseline_header
    dead_citations
  } >"${tmp}"
  mkdir -p "$(dirname "${baseline_path}")"
  cp "${tmp}" "${baseline_path}"
  local n
  n="$(rg -c '^[^#[:space:]]' "${baseline_path}" 2>/dev/null || printf '0')"
  log "baseline updated: ${n} dead reference(s) at ${baseline_path}"
}

cmd_check() {
  validate_baseline "${baseline_path}" || exit 1

  dead_citations >"${tmp_dir}/dead.txt"
  baseline_pairs "${baseline_path}" >"${tmp_dir}/baseline.txt"
  comm -23 "${tmp_dir}/dead.txt" "${tmp_dir}/baseline.txt" >"${tmp_dir}/new.txt" || true

  local new_count
  new_count="$(awk 'NF' "${tmp_dir}/new.txt" | wc -l | tr -d ' ')"
  if [[ "${new_count}" -gt 0 ]]; then
    while IFS=' ' read -r rel path; do
      [ -z "${rel}" ] && continue
      log "${rel} cites missing script ${path} (not in ${baseline_path})"
    done <"${tmp_dir}/new.txt"
    log "${new_count} dead script reference(s) not in the baseline"
    return 1
  fi

  local citation_count dead_count
  citation_count="$(scan_citations | awk 'NF' | wc -l | tr -d ' ')"
  dead_count="$(awk 'NF' "${tmp_dir}/dead.txt" | wc -l | tr -d ' ')"
  log "OK: ${citation_count} script citation(s) checked, ${dead_count} baselined dead reference(s)"
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
