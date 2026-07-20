#!/usr/bin/env bash
#
# test-verify-doc-citations.sh - hermetic tests for
# scripts/verify-doc-citations.sh, the gate that fails a docs page citing a
# phantom/renamed/moved Go test function, a subtest-form citation, or an
# unresolved fixture path (#5406).
#
# Cases 1-8 build a scratch repo_root under mktemp and point the verifier at
# it via ESHU_DOC_CITATIONS_REPO_ROOT (repo root, docs root, and existence-
# check root all derive from it) so a real-tree regeneration never touches
# the committed baseline. Cases 9-10 run against the actual repo tree to
# prove the committed scripts/docs-citations-baseline.txt is both sufficient
# and not stale.
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
  assert_contains "TEST languages/example.md go/internal/gone_test.go::TestSomething" "${baseline}" \
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

# Case 12: the REAL committed docs tree passes with the REAL committed
# baseline (no env override).
test_real_tree_passes_with_committed_baseline() {
  local out="${tmp_root}/case12.out"
  if "${BASH:-bash}" "${verifier}" >"${out}" 2>&1; then
    assert_contains "verify-doc-citations: OK" "${out}" "case12: real tree passes with committed baseline"
  else
    record_fail "case12: real tree passes with committed baseline (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 13: the committed baseline is byte-identical to a fresh
# regeneration off the real docs tree. ESHU_DOC_CITATIONS_BASELINE_PATH
# redirects ONLY the baseline read/write location to a scratch file --
# repo_root/docs_root stay real -- so this never mutates the committed
# scripts/docs-citations-baseline.txt.
test_real_baseline_matches_fresh_regeneration() {
  local regenerated="${tmp_root}/case13-regenerated-baseline.txt" out="${tmp_root}/case13.out"
  ESHU_DOC_CITATIONS_BASELINE_PATH="${regenerated}" "${BASH:-bash}" "${verifier}" -update \
    >"${out}" 2>&1
  local committed="${repo_root}/scripts/docs-citations-baseline.txt"
  if [[ -f "${committed}" ]] && cmp -s "${regenerated}" "${committed}"; then
    record_pass "case13: committed baseline matches a fresh regeneration"
  else
    record_fail "case13: committed baseline matches a fresh regeneration"
    diff "${regenerated}" "${committed}" >&2 || true
  fi
}

test_existing_test_citation_passes
test_phantom_test_citation_fails
test_missing_file_citation_fails
test_baselined_phantom_test_citation_passes
test_subtest_citation_always_fails
test_used_fixture_citation_passes
test_unused_fixture_citation_fails
test_missing_fixture_citation_fails
test_baselined_unused_fixture_shared_across_docs_passes
test_update_is_idempotent
test_fails_closed_on_bad_baseline
test_real_tree_passes_with_committed_baseline
test_real_baseline_matches_fresh_regeneration

if [[ "${FAIL}" -ne 0 ]]; then
  printf 'test-verify-doc-citations FAILED: %d/%d\n' "${FAIL}" "$((PASS + FAIL))" >&2
  exit 1
fi

printf 'test-verify-doc-citations passed: %d/%d\n' "${PASS}" "$((PASS + FAIL))"
