#!/usr/bin/env bash
#
# test-verify-docs-contradiction.sh - hermetic tests for
# scripts/verify-docs-contradiction.sh, the advisory gate that flags a
# docs/public page self-contradicting about the same fact (#5340).
#
# Cases 1-6 build a scratch docs/public tree under mktemp and point the
# verifier at it via ESHU_DOCS_CONTRADICTION_REPO_ROOT (docs_root +
# generated-doc-check root) so a real-tree run never touches the committed
# baseline. Case 7 runs against the actual repo tree to prove the committed
# scripts/docs-contradiction-baseline.txt is sufficient. Case 8 proves the
# committed baseline is not stale (byte-identical to a fresh regeneration).
# Case 9 is the RED->GREEN proof: it reverts the two #5340 doc fixes one at a
# time against the REAL repo tree, shows the gate would have caught each
# reverted contradiction, then restores the fix and shows the gate is quiet
# again — proving the two real docs fixes are exactly what made the gate
# pass, not an accident of the check being too narrow to see them.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-docs-contradiction.sh"

command -v rg >/dev/null 2>&1 || {
  echo "test-verify-docs-contradiction: rg is required" >&2
  exit 1
}
command -v awk >/dev/null 2>&1 || {
  echo "test-verify-docs-contradiction: awk is required" >&2
  exit 1
}

tmp_root="$(mktemp -d)"

# case9 mutates two REAL tracked docs in place and restores them explicitly on
# its happy path. These globals plus the EXIT trap below guarantee both docs
# are restored even if the suite crashes or exits early mid-case9, so a failed
# run never leaves the tracked tree reverted. Empty until case9 records a
# backup. A process-global EXIT trap (not a per-function RETURN trap, which
# would fire on every later return under set -u) is the right hook here.
case9_php_doc=""
case9_php_backup=""
case9_readiness_doc=""
case9_readiness_backup=""

restore_case9_docs() {
  if [[ -n "${case9_php_doc}" && -n "${case9_php_backup}" && -f "${case9_php_backup}" ]]; then
    cp "${case9_php_backup}" "${case9_php_doc}"
  fi
  if [[ -n "${case9_readiness_doc}" && -n "${case9_readiness_backup}" && -f "${case9_readiness_backup}" ]]; then
    cp "${case9_readiness_backup}" "${case9_readiness_doc}"
  fi
  return 0
}

# Restore the tracked docs BEFORE removing tmp_root (the backups live under it).
trap 'restore_case9_docs; rm -rf "${tmp_root}"' EXIT

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

assert_not_contains() {
  local needle="$1"
  local file="$2"
  local label="$3"
  if rg -q --fixed-strings "${needle}" "${file}"; then
    record_fail "${label} (must NOT find: ${needle})"
    cat "${file}" >&2
  else
    record_pass "${label}"
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

# run_verifier invokes the verifier under the SAME bash interpreter running
# this test ($BASH), not a bare PATH-resolved "bash".
run_verifier() {
  local root="$1"
  local out="$2"
  shift 2
  ESHU_DOCS_CONTRADICTION_REPO_ROOT="${root}" "${BASH:-bash}" "${verifier}" "$@" >"${out}" 2>&1
}

# Case 1: a shared-subject anchor contradiction (implemented in one place,
# not-yet-implemented in another, joined by a stopword-free n-gram anchor) is
# flagged.
test_shared_anchor_contradiction_flagged() {
  local root="${tmp_root}/case1"
  local out="${tmp_root}/case1.out"
  write_doc "${root}" "reference/example.md" \
    '# Example' \
    '' \
    'Widget batch resolution is implemented for literal paths today.' \
    '' \
    'Known limitation: widget batch resolution is not yet implemented for' \
    'dynamic paths.'
  ESHU_DOCS_CONTRADICTION_BASELINE_PATH="${root}/scripts/docs-contradiction-baseline.txt" \
    run_verifier "${root}" "${out}" || true
  assert_contains "polarity:widget-batch-resolution" "${out}" \
    "case1: shared n-gram anchor contradiction is flagged"
}

# Case 2: the Laravel-style false-positive guard. Two DIFFERENT subjects, one
# graded positively and the other graded with a real negative trigger word
# ("not yet implemented"), sharing no anchor (no common backtick span, no
# common capability-ID, no common 3-word n-gram) — must NOT be flagged. This
# is the actual anchor-requirement proof, not an accident of vocabulary: both
# sentences use genuine trigger words on both sides.
test_different_subjects_not_flagged() {
  local root="${tmp_root}/case2"
  local out="${tmp_root}/case2.out"
  write_doc "${root}" "reference/example.md" \
    '# Example' \
    '' \
    '`FooWidget::route()` is implemented for literal paths (widgets supported).' \
    '' \
    '`BarGadget::route()` value expansion is not yet implemented for gadgets.'
  ESHU_DOCS_CONTRADICTION_BASELINE_PATH="${root}/scripts/docs-contradiction-baseline.txt" \
    run_verifier "${root}" "${out}" || true
  assert_not_contains "polarity:" "${out}" \
    "case2: different subjects (no shared anchor) is NOT flagged"
  assert_contains "OK:" "${out}" "case2: exits with an OK summary"
}

# Case 2b: mirrors the REAL php.md Laravel capability row verbatim (Route::
# group implemented for one capability, Route::resource deferred for a
# different one, in the SAME line) to prove the actual production false
# positive this gate must not reintroduce stays clean.
test_laravel_row_not_flagged() {
  local root="${tmp_root}/case2b"
  local out="${tmp_root}/case2b.out"
  write_doc "${root}" "reference/example.md" \
    '# Example' \
    '' \
    '`Route::group()` prefix concatenation is implemented for literal `'"'"'prefix'"'"'` array keys (nested groups supported). `Route::resource()` expansion and non-literal group prefixes are deferred.'
  ESHU_DOCS_CONTRADICTION_BASELINE_PATH="${root}/scripts/docs-contradiction-baseline.txt" \
    run_verifier "${root}" "${out}" || true
  assert_not_contains "polarity:" "${out}" \
    "case2b: verbatim Laravel-row shape (implemented + deferred) is NOT flagged"
}

# Case 3: a duplicate first-column table-row key inside one contiguous table
# block is flagged.
test_duplicate_table_row_flagged() {
  local root="${tmp_root}/case3"
  local out="${tmp_root}/case3.out"
  write_doc "${root}" "reference/example.md" \
    '# Example' \
    '' \
    '| Source family | Current state |' \
    '| --- | --- |' \
    '| Kubernetes live | No hosted collector runtime or charted workload. |' \
    '| Something else | Fine. |' \
    '| Kubernetes live | Foundation plus chart exists. |'
  ESHU_DOCS_CONTRADICTION_BASELINE_PATH="${root}/scripts/docs-contradiction-baseline.txt" \
    run_verifier "${root}" "${out}" || true
  assert_contains "duplicate-row:kubernetes-live" "${out}" \
    "case3: duplicate table-row key is flagged"
}

# Case 4: the SAME row label in TWO DIFFERENT tables (separated by a blank
# line, which resets the duplicate-tracking block) must not be flagged.
test_same_label_different_tables_not_flagged() {
  local root="${tmp_root}/case4"
  local out="${tmp_root}/case4.out"
  write_doc "${root}" "reference/example.md" \
    '# Example' \
    '' \
    '| Name | Val |' \
    '| --- | --- |' \
    '| Foo | 1 |' \
    '' \
    'Prose between the two tables resets the block.' \
    '' \
    '| Name | Val |' \
    '| --- | --- |' \
    '| Foo | 2 |'
  ESHU_DOCS_CONTRADICTION_BASELINE_PATH="${root}/scripts/docs-contradiction-baseline.txt" \
    run_verifier "${root}" "${out}" || true
  assert_not_contains "duplicate-row:" "${out}" \
    "case4: same label in two different table blocks is NOT flagged"
  assert_contains "OK:" "${out}" "case4: exits with an OK summary"
}

# Case 5: a finding already listed in the baseline passes without being
# reported as new.
test_baselined_finding_passes() {
  local root="${tmp_root}/case5"
  local out="${tmp_root}/case5.out"
  write_doc "${root}" "reference/example.md" \
    '# Example' \
    '' \
    'Widget batch resolution is implemented for literal paths today.' \
    '' \
    'Known limitation: widget batch resolution is not yet implemented for' \
    'dynamic paths.'
  mkdir -p "${root}/scripts"
  printf '# baseline\nreference/example.md polarity:widget-batch-resolution\n' \
    >"${root}/scripts/docs-contradiction-baseline.txt"
  run_verifier "${root}" "${out}"
  local rc=$?
  if [[ "${rc}" -eq 0 ]]; then
    assert_contains "OK:" "${out}" "case5: baselined finding passes"
  else
    record_fail "case5: baselined finding passes (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 6: a generated/do-not-edit page is skipped entirely, even if its
# content would otherwise trip both checks.
test_generated_doc_skipped() {
  local root="${tmp_root}/case6"
  local out="${tmp_root}/case6.out"
  write_doc "${root}" "reference/generated.md" \
    '<!-- Generated from specs/example.yaml. Do not edit by hand. -->' \
    '' \
    '# Example' \
    '' \
    'Widget batch resolution is implemented for literal paths today.' \
    '' \
    'Known limitation: widget batch resolution is not yet implemented for' \
    'dynamic paths.' \
    '' \
    '| Source family | Current state |' \
    '| --- | --- |' \
    '| Kubernetes live | A. |' \
    '| Kubernetes live | B. |'
  ESHU_DOCS_CONTRADICTION_BASELINE_PATH="${root}/scripts/docs-contradiction-baseline.txt" \
    run_verifier "${root}" "${out}" || true
  assert_not_contains "generated.md" "${out}" "case6: generated doc is skipped entirely"
  assert_contains "OK:" "${out}" "case6: exits with an OK summary"
}

# Case 7: the REAL committed docs tree passes with the REAL committed
# baseline (no env override — this exercises actual repo state).
test_real_tree_passes_with_committed_baseline() {
  local out="${tmp_root}/case7.out"
  if "${BASH:-bash}" "${verifier}" >"${out}" 2>&1; then
    assert_contains "OK:" "${out}" "case7: real tree passes with committed baseline"
  else
    record_fail "case7: real tree passes with committed baseline (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 8: the committed baseline is byte-identical to a fresh regeneration
# off the real docs tree. ESHU_DOCS_CONTRADICTION_BASELINE_PATH redirects
# ONLY the baseline read/write location to a scratch file — repo_root/docs
# stay real — so this never mutates the committed baseline file.
test_real_baseline_matches_fresh_regeneration() {
  local regenerated="${tmp_root}/case8-regenerated-baseline.txt"
  local out="${tmp_root}/case8.out"
  ESHU_DOCS_CONTRADICTION_BASELINE_PATH="${regenerated}" "${BASH:-bash}" "${verifier}" -update \
    >"${out}" 2>&1
  local committed="${repo_root}/scripts/docs-contradiction-baseline.txt"
  if [[ -f "${committed}" ]] && cmp -s "${regenerated}" "${committed}"; then
    record_pass "case8: committed baseline matches a fresh regeneration"
  else
    record_fail "case8: committed baseline matches a fresh regeneration"
    diff "${regenerated}" "${committed}" >&2 || true
  fi
}

# Case 9: RED->GREEN proof against the REAL repo tree. Reverts each of the two
# #5340 doc fixes in place (php.md's Slim group-prefix contradiction and
# collector-reducer-readiness.md's duplicate Kubernetes-live row), asserts the
# gate flags the reintroduced contradiction under enforcement, then restores
# the fixed file byte-for-byte and asserts the gate is clean again. Each doc is
# backed up before mutation and registered with the EXIT trap (via the case9_*
# globals) so both are restored even if the suite crashes mid-case, never
# leaving the tracked tree reverted.
test_red_green_proof() {
  local php_doc="${repo_root}/docs/public/languages/php.md"
  local readiness_doc="${repo_root}/docs/public/reference/collector-reducer-readiness.md"
  local out_red="${tmp_root}/case9-red.out"
  local out_green="${tmp_root}/case9-green.out"

  if [[ ! -f "${php_doc}" || ! -f "${readiness_doc}" ]]; then
    record_fail "case9: RED->GREEN proof (expected doc files missing)"
    return
  fi

  local php_backup="${tmp_root}/php-fixed-backup.md"
  cp "${php_doc}" "${php_backup}"
  # Register with the EXIT trap so a crash between here and the explicit
  # restore below still puts the fixed doc back.
  case9_php_doc="${php_doc}"
  case9_php_backup="${php_backup}"

  # Revert php.md to the pre-#5340-fix shape: re-insert the stale Known
  # Limitations bullet and drop the group-prefix mention from the Supported
  # Today paragraph, matching the exact text the fix removed/added.
  awk '
    /prefix concatenation is implemented: inner routes registered$/ { skip_next = 1; next }
    skip_next && /under a group \(including nested groups\) emit the full prefixed path\.$/ { skip_next = 0; next }
    { print }
  ' "${php_backup}" >"${tmp_root}/php-reverted-step1.md"
  awk '
    /^- A PHP file larger than 1 MiB has its tree-sitter parse skipped entirely in$/ && !inserted {
      print "- Slim `$app->group()` prefix concatenation is not yet implemented: inner"
      print "  routes registered under a group emit their exact registered paths without"
      print "  the group prefix. A route `$group->get(\x27/tasks\x27, ...)` inside"
      print "  `$app->group(\x27/api\x27, ...)` currently emits `/tasks` rather than `/api/tasks`."
      inserted = 1
    }
    { print }
  ' "${tmp_root}/php-reverted-step1.md" >"${php_doc}"

  # DOCS_CONTRADICTION_ENFORCE=true turns the same finding into a non-zero
  # exit: the default advisory mode intentionally exits 0 even with new
  # findings (that is the whole point of "advisory"), so a real RED (gate
  # FAILS) proof needs enforcement on, not just a finding line in the log.
  if DOCS_CONTRADICTION_ENFORCE=true run_verifier "${repo_root}" "${out_red}"; then
    record_fail "case9: reverted php.md fails the gate under enforcement (verifier exited zero)"
    cat "${out_red}" >&2
  else
    record_pass "case9: reverted php.md fails the gate under DOCS_CONTRADICTION_ENFORCE=true"
  fi
  assert_contains "languages/php.md polarity:" "${out_red}" \
    "case9: reverted php.md produces a NEW (un-baselined) polarity finding"

  cp "${php_backup}" "${php_doc}"
  if run_verifier "${repo_root}" "${out_green}"; then
    assert_not_contains "languages/php.md polarity:" "${out_green}" \
      "case9: restored (fixed) php.md is GREEN again"
  else
    record_fail "case9: restored (fixed) php.md is GREEN again (verifier exited non-zero)"
    cat "${out_green}" >&2
  fi
  if cmp -s "${php_backup}" "${php_doc}"; then
    record_pass "case9: php.md restored byte-identical to the fixed version"
  else
    record_fail "case9: php.md restored byte-identical to the fixed version"
  fi

  local readiness_backup="${tmp_root}/readiness-fixed-backup.md"
  cp "${readiness_doc}" "${readiness_backup}"
  case9_readiness_doc="${readiness_doc}"
  case9_readiness_backup="${readiness_backup}"
  awk '
    /^\| Source family \| Current state \|$/ { header = 1; print; next }
    header == 1 && /^\| --- \| --- \|$/ {
      print
      print "| Kubernetes live | No hosted collector runtime or charted workload. |"
      header = 0
      next
    }
    { print }
  ' "${readiness_backup}" >"${readiness_doc}"

  local out_red2="${tmp_root}/case9-red2.out"
  local out_green2="${tmp_root}/case9-green2.out"
  if DOCS_CONTRADICTION_ENFORCE=true run_verifier "${repo_root}" "${out_red2}"; then
    record_fail "case9: reverted collector-reducer-readiness.md fails the gate under enforcement (verifier exited zero)"
    cat "${out_red2}" >&2
  else
    record_pass "case9: reverted collector-reducer-readiness.md fails the gate under DOCS_CONTRADICTION_ENFORCE=true"
  fi
  assert_contains "collector-reducer-readiness.md duplicate-row:kubernetes-live" "${out_red2}" \
    "case9: reverted collector-reducer-readiness.md produces a NEW duplicate-row finding"

  cp "${readiness_backup}" "${readiness_doc}"
  if run_verifier "${repo_root}" "${out_green2}"; then
    assert_not_contains "duplicate-row:kubernetes-live" "${out_green2}" \
      "case9: restored (fixed) collector-reducer-readiness.md is GREEN again"
  else
    record_fail "case9: restored (fixed) collector-reducer-readiness.md is GREEN again (verifier exited non-zero)"
    cat "${out_green2}" >&2
  fi
  if cmp -s "${readiness_backup}" "${readiness_doc}"; then
    record_pass "case9: collector-reducer-readiness.md restored byte-identical to the fixed version"
  else
    record_fail "case9: collector-reducer-readiness.md restored byte-identical to the fixed version"
  fi
}

test_shared_anchor_contradiction_flagged
test_different_subjects_not_flagged
test_laravel_row_not_flagged
test_duplicate_table_row_flagged
test_same_label_different_tables_not_flagged
test_baselined_finding_passes
test_generated_doc_skipped
test_real_tree_passes_with_committed_baseline
test_real_baseline_matches_fresh_regeneration
test_red_green_proof

if [[ "${FAIL}" -ne 0 ]]; then
  printf 'test-verify-docs-contradiction FAILED: %d/%d\n' "${FAIL}" "$((PASS + FAIL))" >&2
  exit 1
fi

printf 'test-verify-docs-contradiction passed: %d/%d\n' "${PASS}" "$((PASS + FAIL))"
