#!/usr/bin/env bash
# #6545: exercise the real CLI outside the old languages/parity scan roots.
# Sourced by test-verify-doc-citations.sh, which owns scratch cleanup.
test_doc_citation_scope_matrix() {
  local scope kind root page out status
  for scope in internal/evidence public/reference .internal/evidence internal/.evidence; do
    for kind in TEST FIXTURE; do
      root="${tmp_root}/scope-${scope//\//-}-${kind}"
      page="docs/${scope}/scope-proof.md"
      out="${root}.out"
      mkdir -p "${root}/$(dirname "${page}")" "${root}/docs/public/languages"
      if [[ "${kind}" == TEST ]]; then
        printf '%s\n' 'Proof: `go/internal/scope_test.go::TestScopePhantom`.' >"${root}/${page}"
      else
        printf '%s\n' 'Fixture: `tests/fixtures/scope_missing/`.' >"${root}/${page}"
      fi
      git -C "${root}" init -q
      git -C "${root}" add -- "${page}"
      if run_verifier "${root}" "${out}"; then status=0; else status=$?; fi
      printf 'scope CLI: %s %s seeded exit=%d\n' "${scope}" "${kind}" "${status}"
      if [[ "${status}" -eq 1 ]]; then
        record_pass "${scope}: ${kind} violation fails"
      else
        record_fail "${scope}: ${kind} violation must exit 1 (got ${status})"
        cat "${out}" >&2
      fi
      assert_contains "${scope}/scope-proof.md" "${out}" "${scope}: ${kind} names page"
      if [[ "${kind}" == TEST ]]; then
        assert_contains 'missing test go/internal/scope_test.go::TestScopePhantom' "${out}" "${scope}: names phantom TEST"
      else
        assert_contains 'missing fixture tests/fixtures/scope_missing/' "${out}" "${scope}: names missing FIXTURE"
      fi
      git -C "${root}" rm -q -f -- "${page}"
      if run_verifier "${root}" "${out}"; then
        record_pass "${scope}: removing ${kind} violation restores green"
      else
        record_fail "${scope}: removing ${kind} violation restores green"
        cat "${out}" >&2
      fi
    done
    # A clean page must report citations checked, not a false-green empty scan.
    write_go_test "${root}" 'go/internal/scope_test.go' 'func TestScopeExists(t *testing.T) {}'
    write_fixture "${root}" 'tests/fixtures/scope_used/'
    write_usage "${root}" 'go/internal/usage_test.go' 'scope_used'
    mkdir -p "${root}/$(dirname "${page}")"
    printf '%s\n' 'Proof: `go/internal/scope_test.go::TestScopeExists`.' \
      'Fixture: `tests/fixtures/scope_used/`.' >"${root}/${page}"
    git -C "${root}" add -- "${page}"
    if run_verifier "${root}" "${out}"; then
      record_pass "${scope}: valid citations pass"
    else
      record_fail "${scope}: valid citations pass"
      cat "${out}" >&2
    fi
    assert_contains '1 test citation(s) checked' "${out}" "${scope}: test scan count is nonzero"
    assert_contains '1 fixture citation(s) checked' "${out}" "${scope}: fixture scan count is nonzero"
  done
}

# Tracked docs remain evidence even when an ignore file hides them from rg.
# Each ignore source gets an isolated repository, ordinary and hidden paths,
# and independent TEST/FIXTURE failures followed by a checked valid citation.
test_doc_citation_ignore_cases() {
  local ignore scope kind root page out status label
  for ignore in .gitignore .ignore .rgignore; do
    for scope in internal/evidence internal/.evidence; do
      for kind in TEST FIXTURE; do
        root="${tmp_root}/ignored-${ignore}-${scope//\//-}-${kind}"
        page="docs/${scope}/ignored-proof.md"
        out="${root}.out"
        label="${ignore} ${scope} ${kind}"
        mkdir -p "${root}/$(dirname "${page}")"
        git -C "${root}" init -q
        printf '/%s/\n' "docs/${scope}" >"${root}/${ignore}"
        write_go_test "${root}" 'go/internal/ignored_test.go' 'func TestIgnoredExists(t *testing.T) {}'
        write_fixture "${root}" 'tests/fixtures/ignored_used/'
        write_usage "${root}" 'go/internal/usage_test.go' 'ignored_used'
        if [[ "${kind}" == TEST ]]; then
          printf '%s\n' 'Proof: `go/internal/ignored_test.go::TestIgnoredPhantom`.' >"${root}/${page}"
        else
          printf '%s\n' 'Fixture: `tests/fixtures/ignored_missing/`.' >"${root}/${page}"
        fi
        git -C "${root}" add -f -- "${ignore}" "${page}"
        if git -C "${root}" ls-files --error-unmatch -- "${page}" >/dev/null 2>&1; then
          record_pass "${label}: ignored page is tracked"
        else
          record_fail "${label}: ignored page must be tracked"
        fi
        if run_verifier "${root}" "${out}"; then status=0; else status=$?; fi
        printf 'ignore CLI: %s seeded exit=%d\n' "${label}" "${status}"
        if [[ "${status}" -eq 1 ]]; then
          record_pass "${label}: ignored violation fails"
        else
          record_fail "${label}: ignored violation must exit 1 (got ${status})"
          cat "${out}" >&2
        fi
        assert_contains "${scope}/ignored-proof.md" "${out}" "${label}: failure names page"
        if [[ "${kind}" == TEST ]]; then
          assert_contains 'missing test go/internal/ignored_test.go::TestIgnoredPhantom' "${out}" "${label}: names missing test"
          printf '%s\n' 'Proof: `go/internal/ignored_test.go::TestIgnoredExists`.' >"${root}/${page}"
        else
          assert_contains 'missing fixture tests/fixtures/ignored_missing/' "${out}" "${label}: names missing fixture"
          printf '%s\n' 'Fixture: `tests/fixtures/ignored_used/`.' >"${root}/${page}"
        fi
        if run_verifier "${root}" "${out}"; then
          record_pass "${label}: valid replacement restores green"
        else
          record_fail "${label}: valid replacement restores green"
          cat "${out}" >&2
        fi
        if [[ "${kind}" == TEST ]]; then
          assert_contains '1 test citation(s) checked' "${out}" "${label}: valid citation was scanned"
        else
          assert_contains '1 fixture citation(s) checked' "${out}" "${label}: valid citation was scanned"
        fi
      done
    done
  done
}

run_doc_citation_scope_cases() {
  run_doc_citation_case test_doc_citation_ignore_cases
  run_doc_citation_case test_doc_citation_scope_matrix
}

# Real-tree scope and committed-ledger checks (cases 12-15). The driver keeps
# their invocation order, including reuse of case12 output by the floor check.
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
  cp "${repo_root}/scripts/docs-citations-baseline.txt" "${regenerated}"
  if ! ESHU_DOC_CITATIONS_BASELINE_PATH="${regenerated}" "${BASH:-bash}" "${verifier}" -update \
    >"${out}" 2>&1; then
    record_fail "case13: fresh baseline regeneration command completes"
    sed -n '1,20p' "${out}" >&2
    return
  fi
  record_pass "case13: fresh baseline regeneration command completes"
  local committed="${repo_root}/scripts/docs-citations-baseline.txt"
  if [[ -f "${committed}" ]] && cmp -s "${regenerated}" "${committed}"; then
    record_pass "case13: committed baseline matches a fresh regeneration"
  else
    record_fail "case13: committed baseline matches a fresh regeneration"
    diff "${regenerated}" "${committed}" >&2 || true
  fi
}

# Case 14: citation-floor guard against the gate silently breaking ITSELF.
# case12 proves the real tree PASSES, but a PASS is also what a regex
# regression that scanned zero citations would produce (0 dead -> OK), so a
# broken scan would leave the gate green while it quietly stopped policing
# drift -- with no signal. This case parses the real-tree OK summary line
# and asserts the scan actually FOUND a floor of citations. The floors are
# set well below the current counts (247 test / 63 fixture) so ordinary doc
# pruning does not trip them, but far enough above zero that a
# regex/glob/scan regression that decimates coverage fails loudly here. The
# `verify-doc-citations: OK: N test citation(s) checked (... M fixture
# citation(s) checked ...` summary is the parse target.
test_real_tree_meets_citation_floor() {
  local out="${tmp_root}/case12.out"
  local test_floor=200 fixture_floor=50
  if [[ ! -f "${out}" ]]; then
    record_fail "case14: case12 verifier output is required before the floor can be checked"
    return
  fi
  if ! rg -q --fixed-strings 'verify-doc-citations: OK' "${out}"; then
    record_fail "case14: case12 real-tree proof must pass before the floor can be checked"
    cat "${out}" >&2
    return
  fi
  local summary
  summary="$(rg -o 'OK: [0-9]+ test citation\(s\) checked .* [0-9]+ fixture citation\(s\) checked' "${out}" | head -1)"
  local test_n fixture_n
  test_n="$(printf '%s\n' "${summary}" | awk '{ print $2 }')"
  fixture_n="$(printf '%s\n' "${summary}" | rg -o '([0-9]+) fixture' -r '$1' | head -1)"
  if [[ -z "${test_n}" || -z "${fixture_n}" ]]; then
    record_fail "case14: could not parse citation counts from the OK summary"
    cat "${out}" >&2
    return
  fi
  if [[ "${test_n}" -ge "${test_floor}" ]]; then
    record_pass "case14: real-tree test-citation count (${test_n}) meets floor (>= ${test_floor})"
  else
    record_fail "case14: real-tree test-citation count (${test_n}) BELOW floor (>= ${test_floor}) -- the scan may be silently broken"
  fi
  if [[ "${fixture_n}" -ge "${fixture_floor}" ]]; then
    record_pass "case14: real-tree fixture-citation count (${fixture_n}) meets floor (>= ${fixture_floor})"
  else
    record_fail "case14: real-tree fixture-citation count (${fixture_n}) BELOW floor (>= ${fixture_floor}) -- the scan may be silently broken"
  fi
}

begin_real_tree_verifier_reuse_proof() {
  real_tree_proof_verifier="${verifier}"
  real_tree_proof_wrapper="${tmp_root}/count-real-tree-verifier.sh"
  real_tree_proof_count_file="${tmp_root}/real-tree-verifier.count"
  : >"${real_tree_proof_count_file}"
  {
    printf '%s\n' '#!/usr/bin/env bash'
    printf '%s\n' 'printf x >>"${REAL_TREE_VERIFIER_COUNT:?}"'
    printf '%s\n' 'exec "${REAL_TREE_VERIFIER:?}" "$@"'
  } >"${real_tree_proof_wrapper}"
  export REAL_TREE_VERIFIER="${real_tree_proof_verifier}"
  export REAL_TREE_VERIFIER_COUNT="${real_tree_proof_count_file}"
  verifier="${real_tree_proof_wrapper}"
}

end_real_tree_verifier_reuse_proof() {
  verifier="${real_tree_proof_verifier}"
  unset REAL_TREE_VERIFIER REAL_TREE_VERIFIER_COUNT
}

test_real_tree_verifier_is_invoked_once() {
  local count
  count="$(wc -c <"${real_tree_proof_count_file}" | tr -d ' ')"
  if [[ "${count}" -eq 1 ]]; then
    record_pass "case14: citation floor reuses the proven case12 verifier output"
  else
    record_fail "case14: citation floor reuses the proven case12 verifier output (got ${count} verifier runs)"
  fi
  unset real_tree_proof_verifier real_tree_proof_wrapper real_tree_proof_count_file
}

test_real_line_ledger_preserves_multiplicity() {
  local baseline="${repo_root}/scripts/docs-citations-baseline.txt"
  local occurrences unique_pairs
  occurrences="$(rg -c '^LINE ' "${baseline}")"
  unique_pairs="$(rg '^LINE ' "${baseline}" | LC_ALL=C sort -u | wc -l | tr -d ' ')"
  if [[ "${unique_pairs}" -ge 1 && "${occurrences}" -gt "${unique_pairs}" ]]; then
    record_pass "case15: real LINE ledger preserves ${occurrences} occurrences across ${unique_pairs} unique source/target pairs"
  else
    record_fail "case15: LINE ledger collapsed multiplicity (${occurrences} occurrences, ${unique_pairs} unique pairs)"
  fi
}

doc_citation_mode_partition_holds() {
  local full="$1" fixtures="$2" repository="$3" combined required
  combined="$(printf '%s\n%s\n' "${fixtures}" "${repository}" | LC_ALL=C sort)"
  [[ "$(printf '%s\n' "${full}" | LC_ALL=C sort)" == "${combined}" ]] || return 1
  for required in \
    test_real_tree_passes_with_committed_baseline \
    test_real_tree_meets_citation_floor \
    test_real_tree_verifier_is_invoked_once \
    test_real_baseline_matches_fresh_regeneration \
    test_real_line_ledger_preserves_multiplicity \
    test_public_gate_name_states_recurrence_scope; do
    printf '%s\n' "${repository}" | rg -qx "${required}" || return 1
    ! printf '%s\n' "${fixtures}" | rg -qx "${required}" || return 1
  done
  ! printf '%s\n' "${fixtures}" |
    rg -q '^(test_real_|test_public_gate_name_states_recurrence_scope$)'
}

copy_doc_citation_mode_helpers() {
  local source_dir="$1" destination="$2"
  mkdir -p "${destination}"
  cp \
    "${source_dir}/test-verify-doc-citations-line-cases.sh" \
    "${source_dir}/test-verify-doc-citations-review-cases.sh" \
    "${source_dir}/test-verify-doc-citations-binary-cases.sh" \
    "${source_dir}/test-verify-doc-citations-preparation-cases.sh" \
    "${source_dir}/test-verify-doc-citations-scope-cases.sh" \
    "${destination}/"
}

move_doc_citation_real_case_to_fixtures() {
  local source="$1" destination="$2"
  awk '
    /^run_basic_fixture_cases\(\)/ { in_fixture = 1 }
    in_fixture && /^  for test_case in \\/ {
      print
      print "    test_real_baseline_matches_fresh_regeneration \\"
      inserted = 1
      next
    }
    /^run_repository_cases\(\)/ { in_repository = 1 }
    in_repository && /test_real_baseline_matches_fresh_regeneration/ { next }
    in_fixture && /^}/ { in_fixture = 0 }
    in_repository && /^}/ { in_repository = 0 }
    { print }
    END { if (!inserted) exit 2 }
  ' "${source}" >"${destination}"
}

add_doc_citation_real_case_to_fixture_runner() {
  local source="$1" destination="$2"
  awk '
    /^run_line_citation_review_cases\(\)/ { inside = 1 }
    inside && /^}/ {
      print "  test_real_new_repository_guard"
      inserted = 1
      inside = 0
    }
    { print }
    END {
      if (!inserted) exit 2
      print ""
      print "test_real_new_repository_guard() { :; }"
    }
  ' "${source}" >"${destination}"
}

remove_doc_citation_repository_floor_case() {
  local source="$1" destination="$2"
  awk '
    /^run_repository_cases\(\)/ { inside = 1 }
    inside && /run_doc_citation_case test_real_tree_meets_citation_floor/ {
      removed = 1
      next
    }
    inside && /^}/ { inside = 0 }
    { print }
    END { if (!removed) exit 2 }
  ' "${source}" >"${destination}"
}

run_doc_citation_case() {
  "$1"
}

run_basic_fixture_cases() {
  local test_case
  for test_case in \
    test_update_is_idempotent \
    test_existing_test_citation_passes \
    test_phantom_test_citation_fails \
    test_missing_file_citation_fails \
    test_baselined_phantom_test_citation_passes \
    test_subtest_citation_always_fails \
    test_used_fixture_citation_passes \
    test_unused_fixture_citation_fails \
    test_missing_fixture_citation_fails \
    test_baselined_unused_fixture_shared_across_docs_passes \
    test_fails_closed_on_bad_baseline; do
    run_doc_citation_case "${test_case}"
  done
}

run_repository_cases() {
  begin_real_tree_verifier_reuse_proof
  run_doc_citation_case test_real_tree_passes_with_committed_baseline
  run_doc_citation_case test_real_tree_meets_citation_floor
  end_real_tree_verifier_reuse_proof
  run_doc_citation_case test_real_tree_verifier_is_invoked_once
  run_doc_citation_case test_real_baseline_matches_fresh_regeneration
  run_doc_citation_case test_real_line_ledger_preserves_multiplicity
  run_line_citation_repository_cases
}

run_line_fixture_cases() {
  run_line_citation_cases
  run_line_citation_review_cases
  run_line_citation_binary_cases
  run_line_citation_preparation_cases
}

run_doc_citation_test_mode() {
  case "$1" in
    full)
      run_doc_citation_scope_cases
      run_basic_fixture_cases
      run_repository_cases
      run_line_fixture_cases
      ;;
    repository) run_repository_cases ;;
    fixtures)
      run_doc_citation_scope_cases
      run_basic_fixture_cases
      run_line_fixture_cases
      ;;
    scope) run_doc_citation_scope_cases ;;
    *) return 2 ;;
  esac
}
