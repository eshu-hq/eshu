#!/usr/bin/env bash
#
# verify-doc-citations.sh - fail a docs page that cites a phantom, renamed, or
# moved Go test function, or a fixture path nobody actually exercises (#5406,
# spun off from #5339/#5402 as its named second deliverable: #5402 fixed the
# drifted citations, this gate prevents the class from recurring).
#
# Scope: every Markdown page under docs/, including internal evidence notes.
# Test and fixture scans cover the same docs tree that triggers this gate.
#
# Three independent citation kinds are scanned, each with its own resolution
# rule:
#
#   1. TEST citations: `<path>.go::TestName` naming a specific Go test
#      function. Checked FILE-SCOPED: `rg -q '^func TestName\(' <cited-file>`
#      against the cited file only (never a repo-wide search), so a test that
#      moved to a different file, was renamed, or never existed is caught
#      even if a same-named function happens to exist elsewhere. A subtest
#      form (`TestName/subtest_case`) is REJECTED unconditionally as a format
#      error, never baseline-able: `rg` can confirm a `func` declaration
#      exists, but a subtest name is a runtime string built from
#      `t.Run(name, ...)` with no static declaration to grep for, so citing
#      one gives this gate nothing to verify — cite the parent test function
#      instead (the fix applied to kotlin.md/php.md in this same change,
#      dropping their `TestRouteQueryProofMatrix/{kotlin_spring,php_laravel}`
#      subtest suffixes since the parent `TestRouteQueryProofMatrix` already
#      resolves).
#   2. FIXTURE citations: a `tests/fixtures/...` or `testdata/...` path.
#      Checked in two steps: (a) existence — the full cited path joined to
#      the repo root, or (repo-wide fallback, mirroring
#      verify-docs-refs.sh's bare-citation convention) the final path
#      component (basename, trailing slash stripped) found anywhere in the
#      tree; (b) usage — that same basename appears as a fixed string in at
#      least one non-Markdown file elsewhere in the repo (Go test code is
#      expected to reference a shared fixture via its basename through a
#      helper such as `repoFixturePath("ecosystems", "java_comprehensive")`
#      or `filepath.Join(root, "tests", "fixtures", ..., name)` — never the
#      literal joined path string — so the basename, not the full path, is
#      the correct usage signal). A path that fails (a) is MISSING; a path
#      that passes (a) but fails (b) is UNUSED (decorative: it exists on
#      disk, nothing exercises it, and it needs a human to either wire it
#      into a test or delete it). Both are baseline-able.
#   3. LINE citations: a raw `go/internal/**/*.go:<line>` pointer anywhere
#      under go/ or docs/. Existing source/target pairs are burn-down debt.
#      New pairs fail even when the target line exists because range validity
#      cannot prove that line supports the surrounding claim. Use a symbol or
#      file anchor. Historical evidence may use a full-SHA `#L<line>` link.
#
# LIMITATION (also see the issue that motivated this note, #5398's
# kubernetes_live route): existence-checking cannot catch a citation that is
# STRUCTURALLY valid — the named test function really does exist, the
# fixture path really is on disk and really is used somewhere — but
# semantically wrong for the claim the surrounding prose makes (the test
# proves a different scenario than the sentence next to it claims, or the
# fixture demonstrates a related-but-distinct case). This gate only proves
# "the citation points at something real"; it cannot prove "the citation
# proves what the doc says it proves". That remains a human review
# responsibility.
#
# Burn-down baseline: scripts/docs-citations-baseline.txt. Three record shapes
# (see the header written by cmd_update / baseline_header):
#   TEST <doc-relpath> <cited-file>::<TestName>
#   FIXTURE <fixture-path>
#   LINE <source-relpath> <cited-file>:<line> (one row per occurrence)
# A TEST record is keyed per (doc, citation) pair, mirroring
# scripts/docs-refs-baseline.txt. A FIXTURE record is keyed by VALUE ONLY,
# deduplicated across every citing doc: whether a fixture is wired into a
# test is a repo-global fact, not a per-page one, so fixing it resolves every
# citing page at once and one baseline line is enough to track it. A subtest
# format error is NEVER written to either bucket — it always fails the gate
# until the doc is fixed to cite the parent test function.
#
# Regenerate with: bash scripts/verify-doc-citations.sh -update
#
# Runs under both macOS's stock /bin/bash 3.2 and Homebrew bash >= 5.1: no
# `declare -A`, no bash arrays for the baseline diff (comm(1) does the set
# difference), and no heredoc/`<<<` here-string carries a body of
# consequence (#5019/#4718 class of hang) — every multi-line body here is a
# fixed sequence of `printf` calls.
set -euo pipefail

repo_root="${ESHU_DOC_CITATIONS_REPO_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
docs_root="${repo_root}/docs"
baseline_path="${ESHU_DOC_CITATIONS_BASELINE_PATH:-${repo_root}/scripts/docs-citations-baseline.txt}"
script_dir="$(cd "$(dirname "$0")" && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

log() {
  printf 'verify-doc-citations: %s\n' "$*" >&2
}

# shellcheck source=scripts/lib/gate-diff-base.sh
source "${script_dir}/lib/gate-diff-base.sh"
# shellcheck source=scripts/lib/doc-citation-lines.sh
source "${script_dir}/lib/doc-citation-lines.sh"

usage() {
  printf 'usage: %s [-update]\n' "${0##*/}"
  printf '  (no args)  check doc test/fixture and raw Go line citations against %s\n' "${baseline_path}"
  printf '  -update    reconcile base debt; cannot add branch-authored LINE occurrences\n'
}

command -v rg >/dev/null 2>&1 || {
  log "missing required tool: rg"
  exit 1
}
[[ -d "${docs_root}" ]] || {
  log "docs root not found: ${docs_root}"
  exit 1
}

# scan_test_citations emits one "<doc-relpath> <citation>" line per unique
# `<path>.go::TestName[/subtest...]` citation across the scanned doc set,
# sorted for determinism. The subtest suffix is captured here (not filtered)
# so classify_test_citations downstream can tell a well-formed citation from
# a format error.
scan_test_citations() {
  (cd "${docs_root}" && rg --hidden --no-heading --no-line-number -o \
    -g '*.md' \
    '[A-Za-z0-9_./-]+\.go::[A-Za-z0-9_]+(/[A-Za-z0-9_]+)*' . 2>/dev/null || true) \
    | LC_ALL=C awk '{
        idx = index($0, ":"); if (idx == 0) next
        rel = substr($0, 1, idx - 1); m = substr($0, idx + 1)
        sub(/^\.\//, "", rel)
        if (m == "" || rel == "") next
        if (m ~ /\.\./) next
        if (m ~ /^\//) next
        if (index(m, "<") || index(m, ">") || index(m, "*") || index(m, "\xe2\x80\xa6")) next
        print rel " " m
      }' \
    | LC_ALL=C sort -u
}

# scan_fixture_citations emits one "<doc-relpath> <citation>" line per unique
# `tests/fixtures/...` or `testdata/...` path citation, same exclusion rules
# as scan_test_citations (placeholder/glob shorthand, parent-escape, absolute
# path).
scan_fixture_citations() {
  (cd "${docs_root}" && rg --hidden --no-heading --no-line-number -o \
    -g '*.md' \
    '(tests/fixtures|testdata)/[A-Za-z0-9_./-]+' . 2>/dev/null || true) \
    | LC_ALL=C awk '{
        idx = index($0, ":"); if (idx == 0) next
        rel = substr($0, 1, idx - 1); m = substr($0, idx + 1)
        sub(/^\.\//, "", rel)
        if (m == "" || rel == "") next
        if (m ~ /\.\./) next
        if (m ~ /^\//) next
        if (index(m, "<") || index(m, ">") || index(m, "*") || index(m, "\xe2\x80\xa6")) next
        print rel " " m
      }' \
    | LC_ALL=C sort -u
}

# classify_test_citations splits scan_test_citations' output into
# format-error rows (subtest form; written to $2, never baseline-able) and
# well-formed rows (written to $1, still needing the func-exists check).
classify_test_citations() {
  local citations_file="$1" ok_out="$2" format_err_out="$3"
  local rel citation test_name
  : >"${ok_out}"
  : >"${format_err_out}"
  while IFS=' ' read -r rel citation; do
    [ -z "${rel}" ] && continue
    test_name="${citation#*::}"
    case "${test_name}" in
      */*) printf '%s %s\n' "${rel}" "${citation}" >>"${format_err_out}" ;;
      *) printf '%s %s\n' "${rel}" "${citation}" >>"${ok_out}" ;;
    esac
  done <"${citations_file}"
}

# dead_test_citations filters well-formed "<rel> <citation>" rows down to
# those whose cited file does not exist, or exists but has no matching
# `^func TestName(` declaration.
dead_test_citations() {
  local citations_file="$1"
  local rel citation go_file test_name
  while IFS=' ' read -r rel citation; do
    [ -z "${rel}" ] && continue
    go_file="${citation%%::*}"
    test_name="${citation#*::}"
    if [[ ! -f "${repo_root}/${go_file}" ]]; then
      printf '%s %s\n' "${rel}" "${citation}"
      continue
    fi
    if ! rg -q "^func ${test_name}\\(" "${repo_root}/${go_file}" 2>/dev/null; then
      printf '%s %s\n' "${rel}" "${citation}"
    fi
  done <"${citations_file}"
}

# fixture_basename strips a trailing slash (directory-style citations) and
# returns the final path component.
fixture_basename() {
  local path="${1%/}"
  printf '%s\n' "${path##*/}"
}

# fixture_resolves reports "ok", "missing", or "unused" for one fixture
# citation path: missing if neither the full path nor a repo-wide basename
# match exists on disk; unused if it exists but its basename is not a fixed
# string in any non-Markdown file elsewhere in the repo (the baseline file
# itself is excluded, so baselining a fixture never masks it as "used" on
# the next -update run).
fixture_resolves() {
  local path="$1" base
  base="$(fixture_basename "${path}")"
  if [[ ! -e "${repo_root}/${path}" ]]; then
    if [ -z "$(cd "${repo_root}" && rg --files -g "**/${base}" . 2>/dev/null | head -1)" ]; then
      printf 'missing\n'
      return
    fi
  fi
  # The trailing "." path argument is load-bearing, not decoration: this
  # function is called from inside dead_fixture_citations' `while read` loop,
  # whose stdin is redirected from the citations file. `rg PATTERN` with NO
  # path argument searches stdin instead of the filesystem whenever stdin is
  # not a controlling terminal -- exactly the case here -- so an omitted "."
  # would silently search the (partially-consumed) citations file instead of
  # the repo tree and this check would never fire.
  local usage_count
  usage_count="$(cd "${repo_root}" && rg -c --fixed-strings "${base}" \
    --glob '!**/*.md' --glob '!scripts/docs-citations-baseline.txt' . 2>/dev/null \
    | wc -l | tr -d ' ')"
  if [[ "${usage_count}" -eq 0 ]]; then
    printf 'unused\n'
  else
    printf 'ok\n'
  fi
}

# dead_fixture_citations filters "<rel> <path>" rows down to those that are
# missing or unused, emitting "<rel> <path> <reason>".
dead_fixture_citations() {
  local citations_file="$1"
  local rel path reason
  while IFS=' ' read -r rel path; do
    [ -z "${rel}" ] && continue
    reason="$(fixture_resolves "${path}")"
    [[ "${reason}" == "ok" ]] && continue
    printf '%s %s %s\n' "${rel}" "${path}" "${reason}"
  done <"${citations_file}"
}

# validate_baseline fails closed: a baseline that exists but is unreadable,
# or that contains a non-comment/non-blank line not matching a record shape,
# is a registry bug.
validate_baseline() {
  local path="$1"
  [[ -e "${path}" ]] || return 0
  if [[ ! -r "${path}" ]]; then
    log "baseline not readable: ${path}"
    return 1
  fi
  # Validate the whole ledger in one process. The ledger is large enough that
  # launching two awk processes per record makes this gate scale with process
  # startup rather than with parsing work.
  if ! LC_ALL=C awk '
    function malformed(expected) {
      printf "verify-doc-citations: baseline malformed at line %d: %s, got: %s\n", NR, expected, $0 > "/dev/stderr"
      exit 1
    }

    $0 == "" || substr($0, 1, 1) == "#" { next }

    $1 == "TEST" {
      if (NF != 3) malformed("expected \"TEST <doc-relpath> <citation>\"")
      next
    }
    $1 == "FIXTURE" {
      if (NF != 2) malformed("expected \"FIXTURE <fixture-path>\"")
      next
    }
    $1 == "LINE" {
      if (NF != 3) malformed("expected \"LINE <source-relpath> <citation>\"")
      next
    }
    { malformed("unknown record kind") }
  ' "${path}"; then
    return 1
  fi
  return 0
}

# baseline_test_pairs emits the baseline's TEST records as "<rel> <citation>"
# (kind prefix stripped), sorted for a deterministic comm(1) diff.
baseline_test_pairs() {
  local path="$1"
  [[ -f "${path}" ]] || return 0
  rg '^TEST ' "${path}" 2>/dev/null | awk '{ if ($2 ~ /^languages\// || $2 == "reference/parity-closure-matrix.md") $2 = "public/" $2; $1 = ""; sub(/^ /, ""); print }' | LC_ALL=C sort -u || true
}

# baseline_fixture_values emits the baseline's FIXTURE records as
# "<fixture-path>" (kind prefix stripped), sorted for a deterministic
# comm(1) diff.
baseline_fixture_values() {
  local path="$1"
  [[ -f "${path}" ]] || return 0
  rg '^FIXTURE ' "${path}" 2>/dev/null | awk '{ print $2 }' | LC_ALL=C sort -u || true
}

baseline_header() {
  printf '%s\n' '# scripts/docs-citations-baseline.txt'
  printf '%s\n' '#'
  printf '%s\n' '# Burn-down baseline for scripts/verify-doc-citations.sh (#5406/#6383).'
  printf '%s\n' '# Every Markdown page under docs/, including docs/internal/evidence/,'
  printf '%s\n' '# is scanned for test and fixture citations. TEST doc paths are relative'
  printf '%s\n' '# to docs/; legacy public-relative TEST keys remain readable.'
  printf '%s\n' '# The full go/ and docs/ trees are also scanned for raw'
  printf '%s\n' '# `go/internal/**/*.go:<line>` citations.'
  printf '%s\n' '#'
  printf '%s\n' '# Three record shapes, one per line:'
  printf '%s\n' '#   TEST <doc-relpath> <cited-file>::<TestName>   - dead/renamed/moved test'
  printf '%s\n' '#   FIXTURE <fixture-path>                        - fixture that exists but'
  printf '%s\n' '#                                                   is not referenced by any'
  printf '%s\n' '#                                                   non-Markdown file in the'
  printf '%s\n' '#                                                   repo (deduplicated across'
  printf '%s\n' '#                                                   every citing doc, since'
  printf '%s\n' '#                                                   fixing the fixture fixes'
  printf '%s\n' '#                                                   every citing page at once)'
  printf '%s\n' '#   LINE <source-relpath> <cited-file>:<line>       - raw line recurrence debt;'
  printf '%s\n' '#                                                   one row per occurrence'
  printf '%s\n' '#'
  printf '%s\n' '# A subtest-form TEST citation (`TestName/subtest_case`) is NEVER written'
  printf '%s\n' '# here — it always fails the gate; cite the parent test function instead.'
  printf '%s\n' '# The updater reconciles immutable-base LINE debt and otherwise only removes rows.'
  printf '%s\n' '# Authority binds exact containing-line bytes at the immutable branch base;'
  printf '%s\n' '# byte-identical lines may move within a source, but rewording is new debt.'
  printf '%s\n' '# Prefer a symbol or file anchor; use a full-SHA #L permalink for historical'
  printf '%s\n' '# evidence. Duplicate LINE rows are intentional multiplicity records.'
  printf '%s\n' '#'
  printf '%s\n' '# Regenerate with:'
  printf '%s\n' '#   bash scripts/verify-doc-citations.sh -update'
}

cmd_update() {
  validate_baseline "${baseline_path}" || exit 1
  check_line_permalink_contract || exit 1
  scan_test_citations >"${tmp_dir}/test-citations.txt"
  scan_fixture_citations >"${tmp_dir}/fixture-citations.txt"
  scan_line_citations >"${tmp_dir}/line-citations.txt" || exit 1
  enforce_immutable_line_authority "${tmp_dir}/line-current-authority.txt" update || return 1
  baseline_line_pairs "${baseline_path}" >"${tmp_dir}/line-baseline.txt"
  if [[ "${line_authority_active}" -eq 0 ]] && ! reject_line_debt_growth "${tmp_dir}/line-citations.txt" "${tmp_dir}/line-baseline.txt"; then
    log "refusing -update: LINE debt may only decrease; baseline left unchanged"
    return 1
  fi
  classify_test_citations "${tmp_dir}/test-citations.txt" \
    "${tmp_dir}/test-ok.txt" "${tmp_dir}/test-format-errors.txt"

  local format_err_count
  format_err_count="$(awk 'NF' "${tmp_dir}/test-format-errors.txt" | wc -l | tr -d ' ')"
  if [[ "${format_err_count}" -gt 0 ]]; then
    log "WARNING: ${format_err_count} subtest-form citation(s) present; these are never baselined and will still fail -- fix them before relying on the regenerated baseline:"
    while IFS=' ' read -r rel citation; do
      [ -z "${rel}" ] && continue
      log "  ${rel} cites ${citation} (subtest form)"
    done <"${tmp_dir}/test-format-errors.txt"
  fi

  dead_test_citations "${tmp_dir}/test-ok.txt" >"${tmp_dir}/test-dead.txt"
  dead_fixture_citations "${tmp_dir}/fixture-citations.txt" >"${tmp_dir}/fixture-dead-detail.txt"

  local tmp="${tmp_dir}/new-baseline.txt"
  {
    baseline_header
    printf '\n'
    printf '%s\n' '# --- TEST records ---'
    while IFS=' ' read -r rel citation; do
      [ -z "${rel}" ] && continue
      printf 'TEST %s %s\n' "${rel}" "${citation}"
    done <"${tmp_dir}/test-dead.txt" | LC_ALL=C sort -u
    printf '%s\n' '# --- FIXTURE records ---'
    awk '{ print $2 }' "${tmp_dir}/fixture-dead-detail.txt" | LC_ALL=C sort -u \
      | while IFS= read -r path; do
        [ -z "${path}" ] && continue
        printf 'FIXTURE %s\n' "${path}"
      done
    printf '%s\n' '# --- LINE records ---'
    write_line_baseline_records "${tmp_dir}/line-citations.txt"
  } >"${tmp}"
  mkdir -p "$(dirname "${baseline_path}")"
  cp "${tmp}" "${baseline_path}"

  local test_n fixture_n line_n
  test_n="$(rg -c '^TEST ' "${baseline_path}" 2>/dev/null || printf '0')"
  fixture_n="$(rg -c '^FIXTURE ' "${baseline_path}" 2>/dev/null || printf '0')"
  line_n="$(rg -c '^LINE ' "${baseline_path}" 2>/dev/null || printf '0')"
  log "baseline updated: ${test_n} TEST record(s), ${fixture_n} FIXTURE record(s), ${line_n} LINE occurrence record(s) at ${baseline_path}"
}

cmd_check() {
  validate_baseline "${baseline_path}" || exit 1
  check_line_permalink_contract || exit 1

  scan_test_citations >"${tmp_dir}/test-citations.txt"
  scan_fixture_citations >"${tmp_dir}/fixture-citations.txt"
  scan_line_citations >"${tmp_dir}/line-citations.txt" || exit 1
  classify_test_citations "${tmp_dir}/test-citations.txt" \
    "${tmp_dir}/test-ok.txt" "${tmp_dir}/test-format-errors.txt"

  local failed=0

  if ! enforce_immutable_line_authority "${tmp_dir}/line-current-authority.txt" check; then
    failed=1
  fi

  baseline_line_pairs "${baseline_path}" >"${tmp_dir}/line-baseline.txt"
  if ! check_line_citations "${tmp_dir}/line-citations.txt" "${tmp_dir}/line-baseline.txt"; then
    failed=1
  fi

  local format_err_count
  format_err_count="$(awk 'NF' "${tmp_dir}/test-format-errors.txt" | wc -l | tr -d ' ')"
  if [[ "${format_err_count}" -gt 0 ]]; then
    while IFS=' ' read -r rel citation; do
      [ -z "${rel}" ] && continue
      log "${rel} cites ${citation} as a subtest -- cite the parent test function instead (subtest names are not statically verifiable)"
    done <"${tmp_dir}/test-format-errors.txt"
    log "${format_err_count} subtest-form citation(s) (never baseline-able)"
    failed=1
  fi

  dead_test_citations "${tmp_dir}/test-ok.txt" >"${tmp_dir}/test-dead.txt"
  baseline_test_pairs "${baseline_path}" >"${tmp_dir}/test-baseline.txt"
  LC_ALL=C comm -23 "${tmp_dir}/test-dead.txt" "${tmp_dir}/test-baseline.txt" >"${tmp_dir}/test-new.txt" || true
  local test_new_count
  test_new_count="$(awk 'NF' "${tmp_dir}/test-new.txt" | wc -l | tr -d ' ')"
  if [[ "${test_new_count}" -gt 0 ]]; then
    while IFS=' ' read -r rel citation; do
      [ -z "${rel}" ] && continue
      log "${rel} cites missing test ${citation} (not in ${baseline_path})"
    done <"${tmp_dir}/test-new.txt"
    log "${test_new_count} dead test citation(s) not in the baseline"
    failed=1
  fi

  dead_fixture_citations "${tmp_dir}/fixture-citations.txt" >"${tmp_dir}/fixture-dead-detail.txt"
  awk '{ print $2 }' "${tmp_dir}/fixture-dead-detail.txt" | LC_ALL=C sort -u >"${tmp_dir}/fixture-dead-values.txt"
  baseline_fixture_values "${baseline_path}" >"${tmp_dir}/fixture-baseline.txt"
  LC_ALL=C comm -23 "${tmp_dir}/fixture-dead-values.txt" "${tmp_dir}/fixture-baseline.txt" >"${tmp_dir}/fixture-new.txt" || true
  local fixture_new_count
  fixture_new_count="$(awk 'NF' "${tmp_dir}/fixture-new.txt" | wc -l | tr -d ' ')"
  if [[ "${fixture_new_count}" -gt 0 ]]; then
    while IFS= read -r path; do
      [ -z "${path}" ] && continue
      while IFS=' ' read -r rel detail_path reason; do
        [[ "${detail_path}" == "${path}" ]] || continue
        log "${rel} cites ${reason} fixture ${path} (not in ${baseline_path})"
      done <"${tmp_dir}/fixture-dead-detail.txt"
    done <"${tmp_dir}/fixture-new.txt"
    log "${fixture_new_count} unresolved fixture citation(s) not in the baseline"
    failed=1
  fi

  if [[ "${failed}" -ne 0 ]]; then
    return 1
  fi

  local test_count fixture_count line_count test_baselined fixture_baselined
  test_count="$(awk 'NF' "${tmp_dir}/test-citations.txt" | wc -l | tr -d ' ')"
  fixture_count="$(awk 'NF' "${tmp_dir}/fixture-citations.txt" | wc -l | tr -d ' ')"
  line_count="$(awk 'NF' "${tmp_dir}/line-citations.txt" | wc -l | tr -d ' ')"
  test_baselined="$(awk 'NF' "${tmp_dir}/test-baseline.txt" | wc -l | tr -d ' ')"
  fixture_baselined="$(awk 'NF' "${tmp_dir}/fixture-baseline.txt" | wc -l | tr -d ' ')"
  log "OK: ${test_count} test citation(s) checked (${test_baselined} baselined dead), ${fixture_count} fixture citation(s) checked (${fixture_baselined} baselined unresolved), ${line_count} raw line citation occurrence(s) tracked"
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
