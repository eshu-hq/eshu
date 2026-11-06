#!/usr/bin/env bash

# Sourced by test-verify-parser-relationship-kit-base-resolution-regressions.sh
# after init_repo, fixture_git, fixture_add_commit, tmp_root and verifier exist.
#
# On a pull_request run the job checks out GitHub's merge ref at a shallow
# depth. Its first parent is the base branch as it stood when the ref was built
# and never moves. The gate used to diff against origin/$GITHUB_BASE_REF
# fetched at job time. In a shallow clone that tip has no merge base with HEAD,
# so `git diff base...HEAD` fails and the two-dot fallback diffs the moved tip
# against HEAD: every commit that landed on the base branch afterwards reads as
# part of the PR (#10222). These cases rebuild that checkout and pin the
# first-parent base, plus the guards that keep a local merge from trusting
# parent 1.
# shellcheck disable=SC2154 # Parent defines init_repo, fixture_git, tmp_root, verifier.

merge_ref_out="${tmp_root}/merge-ref.out"
merge_ref_err="${tmp_root}/merge-ref.err"
merge_ref_docs_message='parser source changed without language/support docs update'

# run_verifier_merge_ref DIR [NAME=value ...] runs the gate in DIR with every
# ambient base and event variable cleared, then the given assignments applied.
run_verifier_merge_ref() {
  local dir="$1"
  shift
  env -u ESHU_PARSER_RELATIONSHIP_KIT_BASE -u GITHUB_EVENT_NAME \
    -u GITHUB_BASE_REF ESHU_PARSER_RELATIONSHIP_KIT_REPO_ROOT="${dir}" "$@" \
    "${verifier}" >"${merge_ref_out}" 2>"${merge_ref_err}"
}

# add_undocumented_parser adds a parser source file and its test, with no docs.
add_undocumented_parser() {
  local dir="$1" name="$2"
  printf 'package parser\nfunc parse%s() {}\n' "${name}" \
    >"${dir}/go/internal/parser/${name}.go"
  printf 'package parser\nfunc Test%s(t interface{}) {}\n' "${name}" \
    >"${dir}/go/internal/parser/${name}_test.go"
}

expect_merge_ref_pass() {
  local name="$1" dir="$2"
  shift 2
  if ! run_verifier_merge_ref "${dir}" "$@"; then
    printf 'merge ref case %s: expected the gate to pass\n' "${name}" >&2
    sed -n '1,160p' "${merge_ref_err}" >&2
    exit 1
  fi
}

# expect_merge_ref_docs_failure requires the failure to be the undocumented
# parser the branch added, not some other finding.
expect_merge_ref_docs_failure() {
  local name="$1" dir="$2"
  shift 2
  if run_verifier_merge_ref "${dir}" "$@"; then
    printf 'merge ref case %s: expected the gate to fail\n' "${name}" >&2
    sed -n '1,160p' "${merge_ref_out}" >&2
    exit 1
  fi
  if ! rg -q --fixed-strings "${merge_ref_docs_message}" "${merge_ref_err}"; then
    printf 'merge ref case %s: failed, but not for the undocumented parser\n' "${name}" >&2
    sed -n '1,160p' "${merge_ref_err}" >&2
    exit 1
  fi
}

# build_shallow_merge_ref SRC_NAME PR_CHANGE MAIN_CHANGE builds the source repo
# and prints a depth-2 clone of its merge commit, the shape of the hosted
# checkout. PR_CHANGE is "parser" (undocumented parser) or "readme". MAIN_CHANGE
# is what lands on main after the ref was built: "parser" (a parser whose docs
# landed in an earlier main commit) or "docs" (a language page edit).
build_shallow_merge_ref() {
  local name="$1" pr_change="$2" main_change="$3" src base clone
  src="$(init_repo "merge-ref-src-${name}")"
  fixture_git "branch ${name}" -C "${src}" branch -M main
  base="$(fixture_git "rev-parse ${name}" -C "${src}" rev-parse HEAD)"

  fixture_git "checkout pr ${name}" -C "${src}" checkout -q -b pr
  if [ "${pr_change}" = "parser" ]; then
    add_undocumented_parser "${src}" new_language
  else
    printf '# pr readme\n' >"${src}/README.md"
  fi
  fixture_add_commit "${src}" 'pr head'

  fixture_git "checkout main ${name}" -C "${src}" checkout -q main
  if [ "${main_change}" = "parser" ]; then
    add_undocumented_parser "${src}" later_language
  else
    printf '\nMain edited this page after the merge ref was built.\n' \
      >>"${src}/docs/public/languages/terraform.md"
  fi
  fixture_add_commit "${src}" 'main moves after the merge ref'

  fixture_git "checkout mergeref ${name}" -C "${src}" checkout -q -b mergeref "${base}"
  fixture_git "merge ${name}" -C "${src}" merge -q --no-ff -m 'merge ref' pr
  clone="${tmp_root}/merge-ref-clone-${name}"
  fixture_git "clone ${name}" clone -q --depth=2 --branch mergeref "file://${src}" "${clone}"
  fixture_git "identity ${name}" -C "${clone}" config user.email "test@example.invalid"
  fixture_git "identity ${name}" -C "${clone}" config user.name "Eshu Test"
  printf '%s\n' "${clone}"
}

# 1. A parser landed on main after the ref was built; the PR only touches the
#    readme. Diffed against the moved tip the later parser reads as the PR's.
merge_ref_moved_repo="$(build_shallow_merge_ref moved readme parser)"
expect_merge_ref_pass "moved base adds no change" "${merge_ref_moved_repo}" \
  GITHUB_EVENT_NAME=pull_request GITHUB_BASE_REF=main

# 1b. Control: an explicit override still wins, and pointing it at the moved
#     tip brings the false failure back, so case 1 exercised a moved base.
fixture_git "fetch moved tip" -C "${merge_ref_moved_repo}" fetch -q --depth=1 origin \
  main:refs/remotes/origin/main
expect_merge_ref_docs_failure "explicit override wins" "${merge_ref_moved_repo}" \
  GITHUB_EVENT_NAME=pull_request GITHUB_BASE_REF=main \
  ESHU_PARSER_RELATIONSHIP_KIT_BASE=origin/main

# 2. The PR adds an undocumented parser while main edited a language page. The
#    moved tip would count that page edit as the PR's docs and hide the parser.
merge_ref_violation_repo="$(build_shallow_merge_ref violation parser docs)"
expect_merge_ref_docs_failure "pr parser without docs still fails" \
  "${merge_ref_violation_repo}" GITHUB_EVENT_NAME=pull_request GITHUB_BASE_REF=main

# 3. Guards. Parent 1 is the base only for a pull_request event on a two-parent
#    HEAD. A developer who merged main into a feature branch has a two-parent
#    HEAD whose first parent is their own tip, already carrying the parser;
#    trusting it would hide the undocumented parser.
merge_ref_local_repo="$(init_repo merge-ref-local)"
fixture_git "branch local" -C "${merge_ref_local_repo}" branch -M main
fixture_git "origin main local" -C "${merge_ref_local_repo}" update-ref refs/remotes/origin/main HEAD
fixture_git "checkout feature local" -C "${merge_ref_local_repo}" checkout -q -b feature
add_undocumented_parser "${merge_ref_local_repo}" new_language
fixture_add_commit "${merge_ref_local_repo}" 'feature adds an undocumented parser'
fixture_git "checkout main local" -C "${merge_ref_local_repo}" checkout -q main
printf '# main readme\n' >"${merge_ref_local_repo}/README.md"
fixture_add_commit "${merge_ref_local_repo}" 'main moves'
fixture_git "origin main moved local" -C "${merge_ref_local_repo}" update-ref refs/remotes/origin/main HEAD
fixture_git "checkout feature again" -C "${merge_ref_local_repo}" checkout -q feature
fixture_git "merge main into feature" -C "${merge_ref_local_repo}" merge -q --no-ff -m 'merge main into feature' main

expect_merge_ref_docs_failure "a local merge with no event still sees the parser" \
  "${merge_ref_local_repo}"
expect_merge_ref_docs_failure "a push event on a merge commit still sees the parser" \
  "${merge_ref_local_repo}" GITHUB_EVENT_NAME=push

# A pull_request run whose HEAD has one parent is not the merge ref. Its first
# parent is the branch's own previous commit, which already carries the parser.
merge_ref_single_repo="$(init_repo merge-ref-single)"
fixture_git "branch single" -C "${merge_ref_single_repo}" branch -M main
fixture_git "origin main single" -C "${merge_ref_single_repo}" update-ref refs/remotes/origin/main HEAD
fixture_git "checkout feature single" -C "${merge_ref_single_repo}" checkout -q -b feature
add_undocumented_parser "${merge_ref_single_repo}" new_language
fixture_add_commit "${merge_ref_single_repo}" 'feature adds an undocumented parser'
printf '# feature readme\n' >"${merge_ref_single_repo}/README.md"
fixture_add_commit "${merge_ref_single_repo}" 'feature follow-up'
expect_merge_ref_docs_failure "a single-parent HEAD on a pull request still sees the parser" \
  "${merge_ref_single_repo}" GITHUB_EVENT_NAME=pull_request GITHUB_BASE_REF=main
