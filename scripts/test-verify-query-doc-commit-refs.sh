#!/usr/bin/env bash
#
# Hermetic tests for verify-query-doc-commit-refs.sh. The verifier rejects
# explicit abbreviated commit citations in the two query documentation directories
# without classifying every short hexadecimal token as a Git revision.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-query-doc-commit-refs.sh"
tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0

record_pass() {
  PASS=$((PASS + 1))
  printf 'ok - %s\n' "$1"
}

record_fail() {
  FAIL=$((FAIL + 1))
  printf 'not ok - %s\n' "$1" >&2
}

new_repo() {
  local name="$1"
  local root="${tmp_root}/${name}"
  mkdir -p "${root}/go/internal/query" "${root}/go/internal/queryplan"
  printf '%s\n' "$root"
}

run_verifier() {
  local root="$1" stdout_file="$2" stderr_file="$3"
  shift 3
  ESHU_QUERY_DOC_COMMIT_REFS_REPO_ROOT="$root" \
    "$@" "$verifier" >"$stdout_file" 2>"$stderr_file"
}

test_explicit_abbreviated_commit_citations_fail() {
  local root stdout_file stderr_file code
  root="$(new_repo explicit-citations)"
  stdout_file="${tmp_root}/explicit-citations.out"
  stderr_file="${tmp_root}/explicit-citations.err"
  # shellcheck disable=SC2016 # Literal Markdown fixtures; backticks must not execute.
  printf '%s\n' \
    '# Evidence' \
    'Theory baseline: commit `85e031612`.' \
    'Both commands ran on final implementation commit `54f952ee5`.' \
    'The source commit SHA `abcdef01234` was tested.' \
    'The source commit SHA: `bcdef012345` was tested.' \
    'The baseline commit: cdef0123456 was tested.' \
    'The uppercase commit `ABCDEF01234` was tested.' \
    'A longer abbreviated commit `0123456789abc` remains unstable.' \
    >"${root}/go/internal/query/evidence.md"
  # shellcheck disable=SC2016 # Literal Markdown fixture; backticks must not execute.
  printf '%s\n' \
    '# Query plan evidence' \
    'The source implementation at commit `3de4afa5c8` was exercised live.' \
    >"${root}/go/internal/queryplan/README.md"

  set +e
  run_verifier "$root" "$stdout_file" "$stderr_file" bash
  code=$?
  set -e

  if [ "$code" -ne 0 ] \
    && rg -q '85e031612' "$stderr_file" \
    && rg -q '54f952ee5' "$stderr_file" \
    && rg -q 'abcdef01234' "$stderr_file" \
    && rg -q 'bcdef012345' "$stderr_file" \
    && rg -q 'cdef0123456' "$stderr_file" \
    && rg -q 'ABCDEF01234' "$stderr_file" \
    && rg -q '0123456789abc' "$stderr_file" \
    && rg -q '3de4afa5c8' "$stderr_file"; then
    record_pass "explicit abbreviated commit citations fail and name every reference"
  else
    record_fail "explicit abbreviated commit citations fail and name every reference (code=$code)"
  fi
}

test_non_commit_hex_controls_pass() {
  local root stdout_file stderr_file
  root="$(new_repo non-commit-controls)"
  stdout_file="${tmp_root}/non-commit-controls.out"
  stderr_file="${tmp_root}/non-commit-controls.err"
  # shellcheck disable=SC2016 # Literal Markdown fixtures; backticks must not execute.
  printf '%s\n' \
    '# Evidence controls' \
    'Historical token: `c5fb1de4e8`.' \
    'Image digest: `sha256:36e0eb79ddf8`.' \
    'External tag: `nornicdb-pr177-search-index-flags:80719f25520e`.' \
    'The comparison command was `git diff --exit-code 76931f4d89 a2a5340a9e4b`.' \
    'The ancestry check was `git merge-base --is-ancestor 76931f4d89 HEAD`.' \
    'Source revision: `1492458852588c884c32f70d27ea2ee07086769c`.' \
    'The full commit `1492458852588c884c32f70d27ea2ee07086769c` is unambiguous.' \
    >"${root}/go/internal/query/evidence.md"
  printf '%s\n' '# Query plan' 'PR #5679 is the stable landing reference.' \
    >"${root}/go/internal/queryplan/README.md"

  if run_verifier "$root" "$stdout_file" "$stderr_file" bash \
    && rg -q 'QUERY DOC COMMIT REFS OK' "$stdout_file"; then
    record_pass "raw hex, digest, external tag, Git command, and full revision controls pass"
  else
    record_fail "raw hex, digest, external tag, Git command, and full revision controls pass"
  fi
}

test_missing_scan_directories_fail_closed() {
  local root stdout_file stderr_file code
  root="${tmp_root}/missing-query"
  mkdir -p "${root}/go/internal/queryplan"
  printf '# Query plan\n' >"${root}/go/internal/queryplan/README.md"
  stdout_file="${tmp_root}/missing-query.out"
  stderr_file="${tmp_root}/missing-query.err"

  set +e
  run_verifier "$root" "$stdout_file" "$stderr_file" bash
  code=$?
  set -e
  if [ "$code" -ne 0 ] \
    && rg -q 'QUERY DOC COMMIT REF SCAN FAILED: directory missing' "$stderr_file"; then
    record_pass "a missing query scan directory fails closed"
  else
    record_fail "a missing query scan directory fails closed (code=$code)"
  fi

  root="${tmp_root}/missing-queryplan"
  mkdir -p "${root}/go/internal/query"
  printf '# Query\n' >"${root}/go/internal/query/README.md"
  stdout_file="${tmp_root}/missing-queryplan.out"
  stderr_file="${tmp_root}/missing-queryplan.err"
  set +e
  run_verifier "$root" "$stdout_file" "$stderr_file" bash
  code=$?
  set -e
  if [ "$code" -ne 0 ] \
    && rg -q 'QUERY DOC COMMIT REF SCAN FAILED: directory missing' "$stderr_file"; then
    record_pass "a missing queryplan scan directory fails closed"
  else
    record_fail "a missing queryplan scan directory fails closed (code=$code)"
  fi
}

test_zero_markdown_fails_closed() {
  local root stdout_file stderr_file code
  root="$(new_repo zero-markdown)"
  stdout_file="${tmp_root}/zero-markdown.out"
  stderr_file="${tmp_root}/zero-markdown.err"
  set +e
  run_verifier "$root" "$stdout_file" "$stderr_file" bash
  code=$?
  set -e
  if [ "$code" -ne 0 ] \
    && rg -q 'QUERY DOC COMMIT REF SCAN FAILED: no Markdown files found' "$stderr_file"; then
    record_pass "zero Markdown files fails closed"
  else
    record_fail "zero Markdown files fails closed (code=$code)"
  fi
}

write_rg_shim() {
  local shim_path="$1" failure_arg="$2" real_rg
  real_rg="$(command -v rg)"
  {
    printf '#!/usr/bin/env bash\n'
    # shellcheck disable=SC2016 # Literal shell source for the rg shim.
    printf 'for arg in "$@"; do\n'
    # shellcheck disable=SC2016 # Literal shell source for the rg shim.
    printf '  [ "$arg" = %q ] && exit 2\n' "$failure_arg"
    printf 'done\n'
    # shellcheck disable=SC2016 # Literal shell source for the rg shim.
    printf 'exec %q "$@"\n' "$real_rg"
  } >"$shim_path"
  chmod +x "$shim_path"
}

test_rg_hard_errors_fail_closed() {
  local root shim_dir stdout_file stderr_file code
  root="$(new_repo rg-hard-error)"
  printf '# Query\n' >"${root}/go/internal/query/README.md"
  printf '# Query plan\n' >"${root}/go/internal/queryplan/README.md"

  shim_dir="${tmp_root}/rg-files-shim"
  mkdir -p "$shim_dir"
  write_rg_shim "${shim_dir}/rg" '--files'
  stdout_file="${tmp_root}/rg-files.out"
  stderr_file="${tmp_root}/rg-files.err"
  set +e
  PATH="${shim_dir}:${PATH}" run_verifier "$root" "$stdout_file" "$stderr_file" bash
  code=$?
  set -e
  if [ "$code" -ne 0 ] \
    && rg -q 'QUERY DOC COMMIT REF SCAN FAILED: rg could not list Markdown files' "$stderr_file" \
    && ! rg -q 'QUERY DOC COMMIT REFS OK' "$stdout_file" "$stderr_file"; then
    record_pass "an rg file-list hard error fails closed"
  else
    record_fail "an rg file-list hard error fails closed (code=$code)"
  fi

  shim_dir="${tmp_root}/rg-content-shim"
  mkdir -p "$shim_dir"
  write_rg_shim "${shim_dir}/rg" '-n'
  stdout_file="${tmp_root}/rg-content.out"
  stderr_file="${tmp_root}/rg-content.err"
  set +e
  PATH="${shim_dir}:${PATH}" run_verifier "$root" "$stdout_file" "$stderr_file" bash
  code=$?
  set -e
  if [ "$code" -ne 0 ] \
    && rg -q 'QUERY DOC COMMIT REF SCAN FAILED: rg could not scan Markdown files' "$stderr_file" \
    && ! rg -q 'QUERY DOC COMMIT REFS OK' "$stdout_file" "$stderr_file"; then
    record_pass "an rg content-scan hard error fails closed"
  else
    record_fail "an rg content-scan hard error fails closed (code=$code)"
  fi
}

test_explicit_abbreviated_commit_citations_fail
test_non_commit_hex_controls_pass
test_missing_scan_directories_fail_closed
test_zero_markdown_fails_closed
test_rg_hard_errors_fail_closed

printf '\n%d tests passed, %d tests failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -ne 0 ]; then
  exit 1
fi
