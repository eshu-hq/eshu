#!/usr/bin/env bash
#
# test-verify-doc-citations.sh - hermetic tests for
# scripts/verify-doc-citations.sh, the gate that fails a docs page citing a
# phantom/renamed/moved Go test function, a subtest-form citation, or an
# unresolved fixture path (#5406).
#
# Cases 1-11 build a scratch repo_root under mktemp and point the verifier at
# it via ESHU_DOC_CITATIONS_REPO_ROOT (repo root, docs root, and existence-
# check root all derive from it) so a real-tree regeneration never touches
# the committed baseline. Cases 12-14 run against the actual repo tree: 12
# proves it passes with the committed scripts/docs-citations-baseline.txt, 13
# proves that baseline is not stale (byte-identical to a fresh regeneration,
# via ESHU_DOC_CITATIONS_BASELINE_PATH redirecting only the write target), and
# 14 asserts the real-tree scan meets a citation floor so a regex/glob
# regression that silently zeroes the scan cannot pass unnoticed. Case 15
# proves the LINE ledger preserves occurrences instead of only unique pairs.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-doc-citations.sh"

command -v rg >/dev/null 2>&1 || {
  echo "test-verify-doc-citations: rg is required" >&2
  exit 1
}

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0
record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

usage() {
  printf 'usage: %s [--repository-only|--fixtures-only|--scope-only]\n' "${0##*/}"
}

mode=full
if [[ "$#" -gt 1 ]]; then
  usage >&2
  exit 2
fi
case "${1:-}" in
  "") ;;
  --repository-only) mode=repository ;;
  --fixtures-only) mode=fixtures ;;
  --scope-only) mode=scope ;;
  *)
    usage >&2
    exit 2
    ;;
esac

assert_contains() {
  local needle="$1" file="$2" label="$3"
  if rg -q --fixed-strings "${needle}" "${file}"; then
    record_pass "${label}"
  else
    record_fail "${label} (expected to find: ${needle})"
    cat "${file}" >&2
  fi
}

# write_doc creates a scratch docs page with the given body lines, under the
# languages/ subtree the verifier scans.
write_doc() {
  local root="$1" rel="$2"
  shift 2
  local file="${root}/docs/public/languages/${rel}"
  mkdir -p "$(dirname "${file}")"
  printf '%s\n' "$@" >"${file}"
}

# write_go_test creates a scratch _test.go file containing the given
# top-level func declarations (one per remaining argument, verbatim), so the
# file-scoped `^func TestName(` check can resolve (or fail to resolve)
# against a real file.
write_go_test() {
  local root="$1" rel="$2"
  shift 2
  local file="${root}/${rel}"
  mkdir -p "$(dirname "${file}")"
  {
    printf 'package scratch\n\n'
    printf '%s\n' "$@"
  } >"${file}"
}

# write_fixture creates a scratch fixture path (file or, with a trailing
# slash, a directory containing a placeholder file) so existence checks can
# resolve it as real.
write_fixture() {
  local root="$1" rel="$2"
  case "${rel}" in
    */)
      mkdir -p "${root}/${rel}"
      touch "${root}/${rel}.keep"
      ;;
    *)
      mkdir -p "$(dirname "${root}/${rel}")"
      printf 'placeholder\n' >"${root}/${rel}"
      ;;
  esac
}

# write_usage creates a scratch non-Markdown file that references the given
# basename as a fixed string, simulating a Go test consuming a fixture.
write_usage() {
  local root="$1" rel="$2" basename="$3"
  local file="${root}/${rel}"
  mkdir -p "$(dirname "${file}")"
  printf 'package scratch\n\nconst fixtureName = "%s"\n' "${basename}" >"${file}"
}

# run_verifier invokes the verifier under the SAME bash interpreter running
# this test ($BASH), not a bare PATH-resolved "bash".
run_verifier() {
  local root="$1" out="$2"
  shift 2
  ESHU_DOC_CITATIONS_REPO_ROOT="${root}" "${BASH:-bash}" "${verifier}" "$@" >"${out}" 2>&1
}

# Case 1: a well-formed test citation whose func exists passes.
test_existing_test_citation_passes() {
  local root="${tmp_root}/case1" out="${tmp_root}/case1.out"
  write_go_test "${root}" "go/internal/foo_test.go" \
    'func TestFooDoesThing(t *testing.T) { t.Parallel() }'
  write_doc "${root}" "example.md" \
    '# Example' '' \
    'Proven by `go/internal/foo_test.go::TestFooDoesThing`.'
  if run_verifier "${root}" "${out}"; then
    assert_contains "verify-doc-citations: OK" "${out}" "case1: existing test citation passes"
  else
    record_fail "case1: existing test citation passes (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 2: a citation naming a phantom/renamed test function fails, naming
# the page and the dead citation (the core drift this gate exists to catch).
test_phantom_test_citation_fails() {
  local root="${tmp_root}/case2" out="${tmp_root}/case2.out"
  write_go_test "${root}" "go/internal/foo_test.go" \
    'func TestFooDoesThing(t *testing.T) { t.Parallel() }'
  write_doc "${root}" "example.md" \
    '# Example' '' \
    'Proven by `go/internal/foo_test.go::TestRenamedAway`.'
  if run_verifier "${root}" "${out}"; then
    record_fail "case2: phantom test citation fails (verifier exited zero)"
    cat "${out}" >&2
  else
    record_pass "case2: phantom test citation fails"
  fi
  assert_contains "languages/example.md" "${out}" "case2: failure names the page"
  assert_contains "TestRenamedAway" "${out}" "case2: failure names the missing test"
}

# Case 3: a citation whose cited FILE does not exist at all fails the same
# way as a phantom func (file-scoped: never falls back to a repo-wide
# search for a same-named test elsewhere).
test_missing_file_citation_fails() {
  local root="${tmp_root}/case3" out="${tmp_root}/case3.out"
  write_doc "${root}" "example.md" \
    '# Example' '' \
    'Proven by `go/internal/gone_test.go::TestSomething`.'
  if run_verifier "${root}" "${out}"; then
    record_fail "case3: missing-file citation fails (verifier exited zero)"
    cat "${out}" >&2
  else
    record_pass "case3: missing-file citation fails"
  fi
  assert_contains "gone_test.go" "${out}" "case3: failure names the missing file"
}

# Case 4: a phantom test citation already in the baseline passes (burn-down).
test_baselined_phantom_test_citation_passes() {
  local root="${tmp_root}/case4" out="${tmp_root}/case4.out"
  write_go_test "${root}" "go/internal/foo_test.go" \
    'func TestFooDoesThing(t *testing.T) { t.Parallel() }'
  write_doc "${root}" "example.md" \
    '# Example' '' \
    'Proven by `go/internal/foo_test.go::TestRenamedAway`.'
  mkdir -p "${root}/scripts"
  printf '# baseline\nTEST languages/example.md go/internal/foo_test.go::TestRenamedAway\n' \
    >"${root}/scripts/docs-citations-baseline.txt"
  if run_verifier "${root}" "${out}"; then
    assert_contains "verify-doc-citations: OK" "${out}" "case4: baselined phantom test citation passes"
  else
    record_fail "case4: baselined phantom test citation passes (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 5: a subtest-form citation ALWAYS fails, even when the parent test
# genuinely exists and even when an identical baseline entry is attempted --
# subtests are never baseline-able (acceptance criterion 1/5406).
test_subtest_citation_always_fails() {
  local root="${tmp_root}/case5" out="${tmp_root}/case5.out"
  write_go_test "${root}" "go/internal/foo_test.go" \
    'func TestFooDoesThing(t *testing.T) { t.Run("case_a", func(t *testing.T) {}) }'
  write_doc "${root}" "example.md" \
    '# Example' '' \
    'Proven by `go/internal/foo_test.go::TestFooDoesThing/case_a`.'
  mkdir -p "${root}/scripts"
  printf '# baseline\nTEST languages/example.md go/internal/foo_test.go::TestFooDoesThing/case_a\n' \
    >"${root}/scripts/docs-citations-baseline.txt"
  if run_verifier "${root}" "${out}"; then
    record_fail "case5: subtest citation always fails, even baselined (verifier exited zero)"
    cat "${out}" >&2
  else
    record_pass "case5: subtest citation always fails, even baselined"
  fi
  assert_contains "subtest" "${out}" "case5: failure names the format problem"
}

# Case 6: a fixture citation that exists AND is used by a non-Markdown file
# passes with zero fixture findings.
test_used_fixture_citation_passes() {
  local root="${tmp_root}/case6" out="${tmp_root}/case6.out"
  write_fixture "${root}" "tests/fixtures/ecosystems/widget_comprehensive/"
  write_usage "${root}" "go/internal/widget_test.go" "widget_comprehensive"
  write_doc "${root}" "example.md" \
    '# Example' '' \
    'Fixture: `tests/fixtures/ecosystems/widget_comprehensive/`.'
  if run_verifier "${root}" "${out}"; then
    assert_contains "verify-doc-citations: OK" "${out}" "case6: used fixture citation passes"
  else
    record_fail "case6: used fixture citation passes (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 7: a fixture that exists on disk but is referenced by NOTHING outside
# Markdown (decorative) fails as "unused", naming the page and the path.
test_unused_fixture_citation_fails() {
  local root="${tmp_root}/case7" out="${tmp_root}/case7.out"
  write_fixture "${root}" "tests/fixtures/ecosystems/orphan_comprehensive/"
  write_doc "${root}" "example.md" \
    '# Example' '' \
    'Fixture: `tests/fixtures/ecosystems/orphan_comprehensive/`.'
  if run_verifier "${root}" "${out}"; then
    record_fail "case7: unused fixture citation fails (verifier exited zero)"
    cat "${out}" >&2
  else
    record_pass "case7: unused fixture citation fails"
  fi
  assert_contains "unused fixture" "${out}" "case7: failure names the reason"
  assert_contains "orphan_comprehensive" "${out}" "case7: failure names the fixture path"
}

# Case 8: a fixture path that does not exist anywhere in the tree fails as
# "missing", distinct from (but equally blocking as) "unused".
test_missing_fixture_citation_fails() {
  local root="${tmp_root}/case8" out="${tmp_root}/case8.out"
  write_doc "${root}" "example.md" \
    '# Example' '' \
    'Fixture: `tests/fixtures/ecosystems/never_existed/`.'
  if run_verifier "${root}" "${out}"; then
    record_fail "case8: missing fixture citation fails (verifier exited zero)"
    cat "${out}" >&2
  else
    record_pass "case8: missing fixture citation fails"
  fi
  assert_contains "missing fixture" "${out}" "case8: failure names the reason"
}

# Case 9: an unused fixture already in the baseline passes, and the FIXTURE
# baseline record is deduplicated by VALUE across every citing doc -- two
# pages citing the same orphan fixture need only one baseline line.
test_baselined_unused_fixture_shared_across_docs_passes() {
  local root="${tmp_root}/case9" out="${tmp_root}/case9.out"
  write_fixture "${root}" "tests/fixtures/ecosystems/orphan_comprehensive/"
  write_doc "${root}" "example-a.md" \
    '# Example A' '' \
    'Fixture: `tests/fixtures/ecosystems/orphan_comprehensive/`.'
  write_doc "${root}" "example-b.md" \
    '# Example B' '' \
    'Fixture: `tests/fixtures/ecosystems/orphan_comprehensive/`.'
  mkdir -p "${root}/scripts"
  printf '# baseline\nFIXTURE tests/fixtures/ecosystems/orphan_comprehensive/\n' \
    >"${root}/scripts/docs-citations-baseline.txt"
  if run_verifier "${root}" "${out}"; then
    assert_contains "verify-doc-citations: OK" "${out}" "case9: baselined fixture shared across two citing docs passes"
  else
    record_fail "case9: baselined fixture shared across two citing docs passes (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 10: baseline regeneration (-update) is idempotent -- running it twice
# on an unchanged tree produces byte-identical output.
test_update_is_idempotent() {
  local root="${tmp_root}/case10" out1="${tmp_root}/case10-run1.out" out2="${tmp_root}/case10-run2.out"
  write_doc "${root}" "example.md" \
    '# Example' '' \
    'Proven by `go/internal/gone_test.go::TestSomething`.' \
    'Fixture: `tests/fixtures/ecosystems/orphan_comprehensive/`.'
  write_fixture "${root}" "tests/fixtures/ecosystems/orphan_comprehensive/"
  run_verifier "${root}" "${out1}" -update
  local baseline="${root}/scripts/docs-citations-baseline.txt"
  local snap1="${tmp_root}/case10-run1-baseline.txt"
  cp "${baseline}" "${snap1}"
  run_verifier "${root}" "${out2}" -update
  if cmp -s "${snap1}" "${baseline}"; then
    record_pass "case10: -update is idempotent across two runs"
  else
    record_fail "case10: -update is idempotent across two runs (baseline changed)"
    diff "${snap1}" "${baseline}" >&2 || true
  fi
  assert_contains "TEST public/languages/example.md go/internal/gone_test.go::TestSomething" "${baseline}" \
    "case10: regenerated baseline records the dead test citation"
  assert_contains "FIXTURE tests/fixtures/ecosystems/orphan_comprehensive/" "${baseline}" \
    "case10: regenerated baseline records the unused fixture citation"
}

# Case 11: fail-closed on an unreadable or garbage baseline -- never
# silently treat either as an empty baseline.
test_fails_closed_on_bad_baseline() {
  local root="${tmp_root}/case11" out_garbage="${tmp_root}/case11-garbage.out" out_unreadable="${tmp_root}/case11-unreadable.out"
  write_doc "${root}" "example.md" '# Example' '' 'No citations here.'
  mkdir -p "${root}/scripts"

  printf 'TEST only-two-fields\n' >"${root}/scripts/docs-citations-baseline.txt"
  if run_verifier "${root}" "${out_garbage}"; then
    record_fail "case11: garbage baseline fails closed (verifier exited zero)"
    cat "${out_garbage}" >&2
  else
    record_pass "case11: garbage baseline fails closed"
  fi
  assert_contains "malformed" "${out_garbage}" "case11: garbage baseline names the problem"

  if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
    record_pass "case11: unreadable-baseline assertion skipped as root (chmod 000 cannot deny uid 0)"
  else
    printf 'FIXTURE tests/fixtures/ecosystems/orphan_comprehensive/\n' \
      >"${root}/scripts/docs-citations-baseline.txt"
    chmod 000 "${root}/scripts/docs-citations-baseline.txt"
    if run_verifier "${root}" "${out_unreadable}"; then
      record_fail "case11: unreadable baseline fails closed (verifier exited zero)"
      cat "${out_unreadable}" >&2
    else
      record_pass "case11: unreadable baseline fails closed"
    fi
    chmod 644 "${root}/scripts/docs-citations-baseline.txt"
  fi
}

# Keep the hostile raw file-and-line cases in a separate file so this test
# entrypoint stays below the repository's 500-line cap.
# shellcheck source=scripts/lib/test-verify-doc-citations-line-cases.sh
source "${repo_root}/scripts/lib/test-verify-doc-citations-line-cases.sh"
# shellcheck source=scripts/lib/test-verify-doc-citations-review-cases.sh
source "${repo_root}/scripts/lib/test-verify-doc-citations-review-cases.sh"
# shellcheck source=scripts/lib/test-verify-doc-citations-binary-cases.sh
source "${repo_root}/scripts/lib/test-verify-doc-citations-binary-cases.sh"
# shellcheck source=scripts/lib/test-verify-doc-citations-preparation-cases.sh
source "${repo_root}/scripts/lib/test-verify-doc-citations-preparation-cases.sh"

# shellcheck source=scripts/lib/test-verify-doc-citations-scope-cases.sh
source "${repo_root}/scripts/lib/test-verify-doc-citations-scope-cases.sh"

test_mode_partition_contract() {
  local out="${tmp_root}/mode-dispatch.out" status
  local helper_dir="${repo_root}/scripts/lib"
  local full fixtures repository moved_dir added_dir removed_dir
  mode_trace() {
    "${BASH:-bash}" -c '
      set -euo pipefail
      source "$1/test-verify-doc-citations-line-cases.sh"
      source "$1/test-verify-doc-citations-review-cases.sh"
      source "$1/test-verify-doc-citations-binary-cases.sh"
      source "$1/test-verify-doc-citations-preparation-cases.sh"
      source "$1/test-verify-doc-citations-scope-cases.sh"
      for test_case in $(declare -F | awk "{ print \$3 }"); do
        case "$test_case" in
          test_*) eval "$test_case() { printf \"%s\\n\" \"$test_case\"; }" ;;
        esac
      done
      run_doc_citation_case() { printf "%s\n" "$1"; }
      begin_real_tree_verifier_reuse_proof() { :; }
      end_real_tree_verifier_reuse_proof() { :; }
      run_doc_citation_test_mode "$2"
    ' bash "$1" "$2"
  }
  full="$(mode_trace "${helper_dir}" full)"
  fixtures="$(mode_trace "${helper_dir}" fixtures)"
  repository="$(mode_trace "${helper_dir}" repository)"
  if doc_citation_mode_partition_holds "${full}" "${fixtures}" "${repository}"; then
    record_pass "mode dispatch: leaf repository and fixture cases partition the default suite"
  else
    record_fail "mode dispatch: leaf repository and fixture cases must partition the default suite"
  fi

  moved_dir="${tmp_root}/mode-cases-moved"
  copy_doc_citation_mode_helpers "${helper_dir}" "${moved_dir}"
  move_doc_citation_real_case_to_fixtures \
    "${helper_dir}/test-verify-doc-citations-scope-cases.sh" \
    "${moved_dir}/test-verify-doc-citations-scope-cases.sh"
  if doc_citation_mode_partition_holds \
    "$(mode_trace "${moved_dir}" full)" \
    "$(mode_trace "${moved_dir}" fixtures)" \
    "$(mode_trace "${moved_dir}" repository)"; then
    record_fail "mode dispatch: moving a real-tree case into a fixture runner is rejected"
  else
    record_pass "mode dispatch: moving a real-tree case into a fixture runner is rejected"
  fi

  added_dir="${tmp_root}/mode-cases-added"
  copy_doc_citation_mode_helpers "${helper_dir}" "${added_dir}"
  add_doc_citation_real_case_to_fixture_runner \
    "${helper_dir}/test-verify-doc-citations-review-cases.sh" \
    "${added_dir}/test-verify-doc-citations-review-cases.sh"
  if doc_citation_mode_partition_holds \
    "$(mode_trace "${added_dir}" full)" \
    "$(mode_trace "${added_dir}" fixtures)" \
    "$(mode_trace "${added_dir}" repository)"; then
    record_fail "mode dispatch: adding a real-tree case to a fixture runner is rejected"
  else
    record_pass "mode dispatch: adding a real-tree case to a fixture runner is rejected"
  fi

  removed_dir="${tmp_root}/mode-cases-floor-removed"
  copy_doc_citation_mode_helpers "${helper_dir}" "${removed_dir}"
  remove_doc_citation_repository_floor_case \
    "${helper_dir}/test-verify-doc-citations-scope-cases.sh" \
    "${removed_dir}/test-verify-doc-citations-scope-cases.sh"
  if doc_citation_mode_partition_holds \
    "$(mode_trace "${removed_dir}" full)" \
    "$(mode_trace "${removed_dir}" fixtures)" \
    "$(mode_trace "${removed_dir}" repository)"; then
    record_fail "mode dispatch: removing the real-tree floor assertion is rejected"
  else
    record_pass "mode dispatch: removing the real-tree floor assertion is rejected"
  fi

  if "${BASH:-bash}" "${repo_root}/scripts/test-verify-doc-citations.sh" --unknown >"${out}" 2>&1; then
    record_fail "mode dispatch: an unknown option fails"
  else
    status=$?
    if [[ "${status}" -eq 2 ]] && rg -q '^usage:' "${out}"; then
      record_pass "mode dispatch: an unknown option fails with usage and exit 2"
    else
      record_fail "mode dispatch: unknown option exit=${status}, want usage and exit 2"
    fi
  fi
}

if [[ "${mode}" != scope ]]; then
  test_mode_partition_contract
fi
run_doc_citation_test_mode "${mode}"

if [[ "${FAIL}" -ne 0 ]]; then
  printf 'test-verify-doc-citations FAILED: %d/%d\n' "${FAIL}" "$((PASS + FAIL))" >&2
  exit 1
fi

if [[ "${mode}" == scope ]]; then
  printf 'scope tests: %d passed, %d failed\n' "${PASS}" "${FAIL}"
  exit 0
fi
printf 'test-verify-doc-citations passed: %d/%d\n' "${PASS}" "$((PASS + FAIL))"
