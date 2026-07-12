#!/usr/bin/env bash
#
# test-verify-docs-refs.sh - hermetic tests for scripts/verify-docs-refs.sh,
# the gate that fails a docs page citing a scripts/*.sh|*.py|*.awk path that
# does not exist in the repo (#5125 workstream 3, deliverable A).
#
# Cases 1-5 and 8 build a scratch repo_root under mktemp and point the
# verifier at it via ESHU_DOCS_REFS_REPO_ROOT (docs_root + existence-check
# root) and, for case 7, ESHU_DOCS_REFS_BASELINE_PATH (baseline read/write
# location only) so a real-tree regeneration never touches the committed
# baseline file. Cases 6-7 run against the actual repo tree to prove the
# committed scripts/docs-refs-baseline.txt is both sufficient (case 6) and
# not stale (case 7).
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-docs-refs.sh"

command -v rg >/dev/null 2>&1 || {
  echo "test-verify-docs-refs: rg is required" >&2
  exit 1
}

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0
record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

assert_contains() {
  local needle="$1"
  local file="$2"
  local label="$3"
  if rg -q --fixed-strings "${needle}" "${file}"; then
    record_pass "${label}"
  else
    record_fail "${label} (expected to find: ${needle})"
    cat "${file}" >&2
  fi
}

# write_doc creates a scratch docs page with the given body lines (one per
# remaining argument).
write_doc() {
  local root="$1"
  local rel="$2"
  shift 2
  local file="${root}/docs/public/${rel}"
  mkdir -p "$(dirname "${file}")"
  printf '%s\n' "$@" >"${file}"
}

# write_script creates a scratch script file so existence checks can resolve
# it as a real, present file.
write_script() {
  local root="$1"
  local rel="$2"
  local file="${root}/${rel}"
  mkdir -p "$(dirname "${file}")"
  printf '#!/usr/bin/env bash\ntrue\n' >"${file}"
}

# run_verifier invokes the verifier under the SAME bash interpreter running
# this test ($BASH), not a bare PATH-resolved "bash" — this repo's PATH-order
# rule means an unqualified "bash" could resolve to a different install than
# the one this suite is being exercised under (see the note in
# test-verify-docs-catalog.sh for the class of bug this avoids).
run_verifier() {
  local root="$1"
  local out="$2"
  shift 2
  ESHU_DOCS_REFS_REPO_ROOT="${root}" "${BASH:-bash}" "${verifier}" "$@" >"${out}" 2>&1
}

# Case 1: a scratch docs tree citing an existing script passes.
test_existing_citation_passes() {
  local root="${tmp_root}/case1"
  local out="${tmp_root}/case1.out"
  write_script "${root}" "scripts/real-script.sh"
  write_doc "${root}" "how-to/example.md" \
    '# Example' \
    '' \
    'Run `scripts/real-script.sh` to do the thing.'
  if run_verifier "${root}" "${out}"; then
    assert_contains "verify-docs-refs: OK" "${out}" "case1: existing citation passes"
  else
    record_fail "case1: existing citation passes (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 2: citing a missing script fails, naming the page and the path.
test_missing_citation_fails() {
  local root="${tmp_root}/case2"
  local out="${tmp_root}/case2.out"
  write_doc "${root}" "how-to/example.md" \
    '# Example' \
    '' \
    'Run `scripts/does-not-exist.sh` to do the thing.'
  if run_verifier "${root}" "${out}"; then
    record_fail "case2: missing citation fails (verifier exited zero)"
    cat "${out}" >&2
  else
    record_pass "case2: missing citation fails"
  fi
  assert_contains "how-to/example.md" "${out}" "case2: failure names the page"
  assert_contains "scripts/does-not-exist.sh" "${out}" "case2: failure names the path"
}

# Case 3: a missing script that IS in the baseline passes.
test_baselined_missing_citation_passes() {
  local root="${tmp_root}/case3"
  local out="${tmp_root}/case3.out"
  write_doc "${root}" "how-to/example.md" \
    '# Example' \
    '' \
    'Run `scripts/does-not-exist.sh` to do the thing.'
  mkdir -p "${root}/scripts"
  printf '# baseline\nhow-to/example.md scripts/does-not-exist.sh\n' \
    >"${root}/scripts/docs-refs-baseline.txt"
  if run_verifier "${root}" "${out}"; then
    assert_contains "verify-docs-refs: OK" "${out}" "case3: baselined missing citation passes"
  else
    record_fail "case3: baselined missing citation passes (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 4: placeholder/glob citations are ignored entirely — a doc containing
# ONLY placeholder-shaped paths passes with zero real citations found.
test_placeholder_citations_ignored() {
  local root="${tmp_root}/case4"
  local out="${tmp_root}/case4.out"
  write_doc "${root}" "reference/example.md" \
    '# Example' \
    '' \
    'Generic form: `scripts/<name>.sh`.' \
    'Glob form: `scripts/*.sh`.' \
    'Prefix glob form: `scripts/verify-*.sh`.'
  if run_verifier "${root}" "${out}"; then
    record_pass "case4: placeholder citations ignored (verifier exits zero)"
  else
    record_fail "case4: placeholder citations ignored (verifier exited non-zero)"
    cat "${out}" >&2
  fi
  if rg -q -- 'does-not-exist|missing script' "${out}"; then
    record_fail "case4: placeholder citations must not be reported as dead"
    cat "${out}" >&2
  else
    record_pass "case4: no placeholder path reported as a dead reference"
  fi
}

# Case 5: baseline regeneration (-update) is idempotent — running it twice on
# an unchanged tree produces byte-identical output.
test_update_is_idempotent() {
  local root="${tmp_root}/case5"
  local out1="${tmp_root}/case5-run1.out"
  local out2="${tmp_root}/case5-run2.out"
  write_doc "${root}" "how-to/example.md" \
    '# Example' \
    '' \
    'Run `scripts/does-not-exist.sh` and `scripts/also-missing.py`.'
  run_verifier "${root}" "${out1}" -update
  local baseline="${root}/scripts/docs-refs-baseline.txt"
  local snap1="${tmp_root}/case5-run1-baseline.txt"
  cp "${baseline}" "${snap1}"
  run_verifier "${root}" "${out2}" -update
  if cmp -s "${snap1}" "${baseline}"; then
    record_pass "case5: -update is idempotent across two runs"
  else
    record_fail "case5: -update is idempotent across two runs (baseline changed)"
    diff "${snap1}" "${baseline}" >&2 || true
  fi
  assert_contains "how-to/example.md scripts/also-missing.py" "${baseline}" \
    "case5: regenerated baseline records the dead reference"
}

# Case 6: the REAL committed docs tree passes with the REAL committed
# baseline (no env override — this exercises actual repo state).
test_real_tree_passes_with_committed_baseline() {
  local out="${tmp_root}/case6.out"
  if "${BASH:-bash}" "${verifier}" >"${out}" 2>&1; then
    assert_contains "verify-docs-refs: OK" "${out}" "case6: real tree passes with committed baseline"
  else
    record_fail "case6: real tree passes with committed baseline (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 7: the committed baseline is byte-identical to a fresh regeneration
# off the real docs tree. ESHU_DOCS_REFS_BASELINE_PATH redirects ONLY the
# baseline read/write location to a scratch file — repo_root/docs_root stay
# real — so this never mutates the committed scripts/docs-refs-baseline.txt.
test_real_baseline_matches_fresh_regeneration() {
  local regenerated="${tmp_root}/case7-regenerated-baseline.txt"
  local out="${tmp_root}/case7.out"
  ESHU_DOCS_REFS_BASELINE_PATH="${regenerated}" "${BASH:-bash}" "${verifier}" -update \
    >"${out}" 2>&1
  local committed="${repo_root}/scripts/docs-refs-baseline.txt"
  if [[ -f "${committed}" ]] && cmp -s "${regenerated}" "${committed}"; then
    record_pass "case7: committed baseline matches a fresh regeneration"
  else
    record_fail "case7: committed baseline matches a fresh regeneration"
    diff "${regenerated}" "${committed}" >&2 || true
  fi
}

# Case 9: resolution semantics (#5125 review P1-1). A citation is checked at
# its FULL cited path, not truncated to the scripts/ suffix; a bare
# scripts/NAME citation that misses at repo root falls back to tree-wide
# resolution (the implied-cd convention extension docs use); a citation is
# dead only when it resolves nowhere.
test_resolution_semantics() {
  local root="${tmp_root}/case9"
  local out="${tmp_root}/case9.out"
  write_script "${root}" "examples/demo/scripts/live-nested.sh"
  write_script "${root}" "examples/pkg/scripts/pkg-local.sh"
  write_doc "${root}" "guides/example.md" \
    '# Example' \
    '' \
    'Nested live: `examples/demo/scripts/live-nested.sh`.' \
    'Nested dead: `examples/demo/scripts/gone-nested.sh`.' \
    'From the package directory run `scripts/pkg-local.sh`.' \
    'Bare dead: `scripts/resolves-nowhere.sh`.'
  if run_verifier "${root}" "${out}"; then
    record_fail "case9: two dead citations must fail the gate (exited zero)"
    cat "${out}" >&2
  else
    record_pass "case9: dead citations still fail the gate"
  fi
  assert_contains "examples/demo/scripts/gone-nested.sh" "${out}" \
    "case9: nested citation missing everywhere is flagged"
  assert_contains "scripts/resolves-nowhere.sh" "${out}" \
    "case9: bare citation resolving nowhere is flagged"
  if rg -q --fixed-strings "live-nested.sh" "${out}"; then
    record_fail "case9: nested citation whose file exists must not be flagged"
    cat "${out}" >&2
  else
    record_pass "case9: nested live citation resolves at its full path"
  fi
  if rg -q --fixed-strings "pkg-local.sh" "${out}"; then
    record_fail "case9: bare citation resolvable in-tree must not be flagged"
    cat "${out}" >&2
  else
    record_pass "case9: bare citation resolves tree-wide (implied-cd convention)"
  fi
}

# Case 8: fail-closed on an unreadable or garbage baseline — never silently
# treat either as an empty baseline.
test_fails_closed_on_bad_baseline() {
  local root="${tmp_root}/case8"
  local out_garbage="${tmp_root}/case8-garbage.out"
  local out_unreadable="${tmp_root}/case8-unreadable.out"
  write_doc "${root}" "how-to/example.md" \
    '# Example' \
    '' \
    'No script citations here.'
  mkdir -p "${root}/scripts"

  printf 'this-line-has-only-one-field\n' \
    >"${root}/scripts/docs-refs-baseline.txt"
  if run_verifier "${root}" "${out_garbage}"; then
    record_fail "case8: garbage baseline fails closed (verifier exited zero)"
    cat "${out_garbage}" >&2
  else
    record_pass "case8: garbage baseline fails closed"
  fi
  assert_contains "malformed" "${out_garbage}" "case8: garbage baseline names the problem"

  printf 'how-to/example.md scripts/does-not-exist.sh\n' \
    >"${root}/scripts/docs-refs-baseline.txt"
  chmod 000 "${root}/scripts/docs-refs-baseline.txt"
  if run_verifier "${root}" "${out_unreadable}"; then
    record_fail "case8: unreadable baseline fails closed (verifier exited zero)"
    cat "${out_unreadable}" >&2
  else
    record_pass "case8: unreadable baseline fails closed"
  fi
  chmod 644 "${root}/scripts/docs-refs-baseline.txt"
}

test_existing_citation_passes
test_missing_citation_fails
test_baselined_missing_citation_passes
test_placeholder_citations_ignored
test_update_is_idempotent
test_real_tree_passes_with_committed_baseline
test_real_baseline_matches_fresh_regeneration
test_fails_closed_on_bad_baseline
test_resolution_semantics

if [[ "${FAIL}" -ne 0 ]]; then
  printf 'test-verify-docs-refs FAILED: %d/%d\n' "${FAIL}" "$((PASS + FAIL))" >&2
  exit 1
fi

printf 'test-verify-docs-refs passed: %d/%d\n' "${PASS}" "$((PASS + FAIL))"
