#!/usr/bin/env bash
# Base-commit cases for the raw-line debt authority (PR #10131 shape).
# Sourced by test-verify-doc-citations.sh after its shared helpers exist.
#
# On a pull request the gate runs on GitHub's merge ref. Its first parent is
# the base branch as GitHub combined it with the PR, and it does not move. The
# gate used to diff against origin/<base> fetched at job time, so a commit that
# landed on the base branch after the merge ref was built (one that removed a
# citation the PR still carries) read as debt the PR had added (#10131).

# build_merge_ref_fixture creates a repo whose HEAD is a pull-request merge
# commit: parent 1 is the base branch as it stood when the ref was built, parent
# 2 is the PR head. The base branch then moves: origin/main points at a later
# commit that drops the citation the merge ref still carries. Prints the
# first-parent commit id.
build_merge_ref_fixture() {
  local root="$1" pr_doc="${2:-}" base
  local baseline="${root}/scripts/docs-citations-baseline.txt"

  write_plain_go "${root}" "go/internal/example.go"
  write_plain_go "${root}" "go/internal/other.go"
  write_doc "${root}" "line.md" \
    '# Existing debt' '' \
    'Existing: `go/internal/example.go:3`.'
  mkdir -p "${root}/scripts"
  printf 'LINE docs/public/languages/line.md go/internal/example.go:3\n' >"${baseline}"
  git -C "${root}" init -q
  git -C "${root}" config user.name 'Doc Citation Test'
  git -C "${root}" config user.email 'doc-citation-test@example.invalid'
  git -C "${root}" add .
  git -C "${root}" commit -qm 'main when the merge ref was built'
  git -C "${root}" branch -M main
  base="$(git -C "${root}" rev-parse HEAD)"

  git -C "${root}" checkout -q -b pr
  if [[ -n "${pr_doc}" ]]; then
    write_doc "${root}" "added.md" '# Added by the PR' '' "${pr_doc}"
    printf 'LINE docs/public/languages/added.md go/internal/other.go:3\n' >>"${baseline}"
  else
    printf 'unrelated\n' >"${root}/docs/public/languages/unrelated.md"
  fi
  git -C "${root}" add .
  git -C "${root}" commit -qm 'pr head'

  git -C "${root}" checkout -q main
  write_doc "${root}" "line.md" '# Citation removed on main after the ref was built'
  : >"${baseline}"
  git -C "${root}" add .
  git -C "${root}" commit -qm 'main moves and drops the citation'
  git -C "${root}" update-ref refs/remotes/origin/main HEAD

  git -C "${root}" checkout -q -b mergeref "${base}"
  git -C "${root}" merge -q --no-ff -m 'merge ref' pr
  printf '%s\n' "${base}"
}

test_merge_ref_diffs_against_its_first_parent_not_the_moved_base_tip() {
  local root="${tmp_root}/line-merge-ref-moved-base"
  local out="${tmp_root}/line-merge-ref-moved-base.out" base
  base="$(build_merge_ref_fixture "${root}")"

  if GITHUB_EVENT_NAME=pull_request GITHUB_BASE_REF=main \
    run_verifier "${root}" "${out}"; then
    record_pass "base case: a base branch that moved after the merge ref was built adds no debt"
  else
    record_fail "base case: a base branch that moved after the merge ref was built adds no debt (verifier failed)"
    cat "${out}" >&2
  fi
  if rg -q --fixed-strings "branch-added LINE debt" "${out}"; then
    record_fail "base case: the moved base tip is not read as PR-added debt (base ${base})"
  else
    record_pass "base case: the moved base tip is not read as PR-added debt"
  fi
}

test_merge_ref_still_rejects_debt_the_pr_adds() {
  local root="${tmp_root}/line-merge-ref-added-debt"
  local out="${tmp_root}/line-merge-ref-added-debt.out" base
  base="$(build_merge_ref_fixture "${root}" 'Added: `go/internal/other.go:3`.')"

  if GITHUB_EVENT_NAME=pull_request GITHUB_BASE_REF=main \
    run_verifier "${root}" "${out}"; then
    record_fail "base case: a PR that adds raw-line debt still fails on its merge ref (verifier exited zero)"
    cat "${out}" >&2
  else
    record_pass "base case: a PR that adds raw-line debt still fails on its merge ref"
  fi
  assert_contains "branch-added LINE debt: docs/public/languages/added.md go/internal/other.go:3" \
    "${out}" "base case: the added citation is named as branch-added debt"
  assert_contains "relative to ${base}" "${out}" \
    "base case: the debt is measured against the merge ref's first parent"
}

# build_debt_repo creates a repo with one baselined citation on main
# and leaves HEAD on main with origin/main pointing at it. Prints nothing.
build_debt_repo() {
  local root="$1"
  local baseline="${root}/scripts/docs-citations-baseline.txt"

  write_plain_go "${root}" "go/internal/example.go"
  write_plain_go "${root}" "go/internal/other.go"
  write_doc "${root}" "line.md" \
    '# Existing debt' '' \
    'Existing: `go/internal/example.go:3`.'
  mkdir -p "${root}/scripts"
  printf 'LINE docs/public/languages/line.md go/internal/example.go:3\n' >"${baseline}"
  git -C "${root}" init -q
  git -C "${root}" config user.name 'Doc Citation Test'
  git -C "${root}" config user.email 'doc-citation-test@example.invalid'
  git -C "${root}" add .
  git -C "${root}" commit -qm 'main'
  git -C "${root}" branch -M main
  git -C "${root}" update-ref refs/remotes/origin/main HEAD
}

# add_debt_commit adds one raw-line citation to a new page and its baseline row.
add_debt_commit() {
  local root="$1" message="$2"
  write_doc "${root}" "added.md" '# Added by the branch' '' 'Added: `go/internal/other.go:3`.'
  printf 'LINE docs/public/languages/added.md go/internal/other.go:3\n' \
    >>"${root}/scripts/docs-citations-baseline.txt"
  git -C "${root}" add .
  git -C "${root}" commit -qm "${message}"
}

# expect_added_debt_caught runs the verifier with the given environment and
# requires it to fail naming the branch-added citation. A guard that wrongly
# trusts HEAD's first parent as the base hides this debt and the run passes.
expect_added_debt_caught() {
  local name="$1" root="$2" out="$3" status=0
  shift 3
  # A subshell, not env(1): run_verifier is a shell function. NAME=value sets
  # and "-u NAME" unsets, so no ambient CI variable leaks into the case.
  (
    while [[ $# -gt 0 ]]; do
      if [[ "$1" == "-u" ]]; then
        unset "$2"
        shift 2
      else
        # "$1" is a NAME=value assignment string, which export accepts.
        # shellcheck disable=SC2163
        export "$1"
        shift
      fi
    done
    run_verifier "${root}" "${out}"
  ) || status=$?
  if [[ ! -s "${out}" ]]; then
    record_fail "base case: ${name} (the verifier produced no output, so it did not run)"
    return 0
  fi
  if [[ "${status}" -eq 0 ]]; then
    record_fail "base case: ${name} (verifier exited zero, the added debt was hidden)"
    cat "${out}" >&2
  else
    record_pass "base case: ${name}"
  fi
  assert_contains "branch-added LINE debt: docs/public/languages/added.md go/internal/other.go:3" \
    "${out}" "base case: ${name}: the added citation is named"
}

# A developer who merged main into a local branch has a two-parent HEAD whose
# first parent is their own previous tip. Only a pull_request event may treat
# parent 1 as the base, so a run with no event or a push event must keep the
# resolver's order and still see the debt the branch added.
build_local_merge_fixture() {
  local root="$1"
  build_debt_repo "${root}"
  git -C "${root}" checkout -q -b feature
  add_debt_commit "${root}" 'feature adds debt'
  git -C "${root}" checkout -q main
  printf 'unrelated\n' >"${root}/docs/public/languages/unrelated.md"
  git -C "${root}" add .
  git -C "${root}" commit -qm 'main moves'
  git -C "${root}" update-ref refs/remotes/origin/main HEAD
  git -C "${root}" checkout -q feature
  git -C "${root}" merge -q --no-ff -m 'merge main into feature' main
}

test_local_merge_without_an_event_keeps_the_resolver_base() {
  local root="${tmp_root}/line-local-merge-no-event"
  build_local_merge_fixture "${root}"
  expect_added_debt_caught "a local merge with no event still sees the debt the branch added" \
    "${root}" "${root}.out" -u GITHUB_EVENT_NAME -u GITHUB_BASE_REF
}

test_push_event_keeps_the_resolver_base() {
  local root="${tmp_root}/line-push-event"
  build_local_merge_fixture "${root}"
  expect_added_debt_caught "a push event on a merge commit still sees the debt it added" \
    "${root}" "${root}.out" GITHUB_EVENT_NAME=push -u GITHUB_BASE_REF
}

# A pull_request run whose HEAD has one parent (a checkout that is not the
# merge ref) must not use that parent either: here it is the branch's own first
# commit, which already carries the debt.
test_single_parent_head_on_a_pull_request_keeps_the_resolver_base() {
  local root="${tmp_root}/line-single-parent"
  build_debt_repo "${root}"
  git -C "${root}" checkout -q -b feature
  add_debt_commit "${root}" 'feature adds debt'
  printf 'unrelated\n' >"${root}/docs/public/languages/unrelated.md"
  git -C "${root}" add .
  git -C "${root}" commit -qm 'feature follow-up'
  expect_added_debt_caught "a single-parent HEAD on a pull request still sees the debt" \
    "${root}" "${root}.out" GITHUB_EVENT_NAME=pull_request GITHUB_BASE_REF=main
}

# A two-parent HEAD whose parent objects are absent falls back instead of
# naming a base that is not there. git rev-list refuses such a HEAD, which
# the helper's own rev-list check catches before its cat-file check runs, so
# this case pins that outcome, not the cat-file line (a mutant that deletes
# the cat-file line survives, because that line is unreachable behind it).
test_missing_first_parent_object_keeps_the_resolver_base() {
  local root="${tmp_root}/line-missing-parent" tree commit
  build_debt_repo "${root}"
  git -C "${root}" checkout -q -b feature
  add_debt_commit "${root}" 'feature adds debt'
  tree="$(git -C "${root}" rev-parse 'HEAD^{tree}')"
  commit="$(printf 'tree %s\nparent %s\nparent %s\nauthor T <t@example.invalid> 0 +0000\ncommitter T <t@example.invalid> 0 +0000\n\nmerge ref with absent parents\n' \
    "${tree}" "$(printf '1%.0s' {1..40})" "$(printf '2%.0s' {1..40})" |
    git -C "${root}" hash-object -t commit -w --literally --stdin)"
  git -C "${root}" update-ref HEAD "${commit}"
  expect_added_debt_caught "an absent first parent on a pull request falls back to the resolver base" \
    "${root}" "${root}.out" GITHUB_EVENT_NAME=pull_request GITHUB_BASE_REF=main
}

run_line_citation_base_cases() {
  local test_case
  for test_case in \
    test_merge_ref_diffs_against_its_first_parent_not_the_moved_base_tip \
    test_merge_ref_still_rejects_debt_the_pr_adds \
    test_local_merge_without_an_event_keeps_the_resolver_base \
    test_push_event_keeps_the_resolver_base \
    test_single_parent_head_on_a_pull_request_keeps_the_resolver_base \
    test_missing_first_parent_object_keeps_the_resolver_base; do
    run_doc_citation_case "${test_case}"
  done
}
