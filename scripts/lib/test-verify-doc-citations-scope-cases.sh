#!/usr/bin/env bash
# #6545: exercise the real CLI outside the old languages/parity scan roots.
# Sourced by test-verify-doc-citations.sh, which owns scratch cleanup.
run_doc_citation_scope_cases() {
  run_doc_citation_ignore_cases
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
run_doc_citation_ignore_cases() {
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
