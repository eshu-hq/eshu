#!/usr/bin/env bash

# Sourced by test-verify-parser-relationship-kit.sh after its fixture helpers
# (init_repo, tmp_root, verifier) are defined. Keep these two base-resolution
# regression cases outside the parent test driver so that driver stays below
# the repository's 500-line cap.
# shellcheck disable=SC2154 # Parent defines init_repo, tmp_root, verifier.

# Regression: with neither ESHU_PARSER_RELATIONSHIP_KIT_BASE nor
# GITHUB_BASE_REF set -- the shape of this gate's registry command, which every
# test above bypasses by pinning HEAD~1 -- the base must be the merge base with
# origin/main. A HEAD~1 default scopes the gate to the last commit, so a parser
# added in an earlier commit escapes whenever the tip commit is innocuous.
#
# Gives the fixture a real origin/main to resolve a merge base against: clone
# the repo at its initial commit into a bare origin, then branch away from it.
merge_base_repo="$(init_repo merge-base)"
git -C "${merge_base_repo}" branch -M main
merge_base_origin="${tmp_root}/merge-base-origin"
git clone -q --bare "${merge_base_repo}" "${merge_base_origin}"
git -C "${merge_base_repo}" remote add origin "${merge_base_origin}"
git -C "${merge_base_repo}" fetch -q origin
git -C "${merge_base_repo}" checkout -q -b feature

run_verifier_local_base() {
  local dir="$1"
  env -u ESHU_PARSER_RELATIONSHIP_KIT_BASE -u GITHUB_BASE_REF \
    ESHU_PARSER_RELATIONSHIP_KIT_REPO_ROOT="${dir}" \
    "${verifier}" >/tmp/eshu-parser-relationship-kit.out \
    2>/tmp/eshu-parser-relationship-kit.err
}

# Commit A: a parser with no docs -- the gate must reject this.
printf 'package parser\nfunc parseNewLanguage() {}\n' \
  >"${merge_base_repo}/go/internal/parser/new_language.go"
printf 'package parser\nfunc TestNewLanguage(t interface{}) {}\n' \
  >"${merge_base_repo}/go/internal/parser/new_language_test.go"
git -C "${merge_base_repo}" add .
git -C "${merge_base_repo}" commit -q -m 'branch commit A: parser without docs'
# Commit B: an innocuous tip commit that used to hide commit A from the gate.
printf '# readme\n' >"${merge_base_repo}/README.md"
git -C "${merge_base_repo}" add .
git -C "${merge_base_repo}" commit -q -m 'branch commit B: readme touch'

if run_verifier_local_base "${merge_base_repo}"; then
  printf 'expected the gate to FAIL: the branch adds an undocumented parser in\n' >&2
  printf 'an earlier commit and its tip commit is innocuous. A pass here means\n' >&2
  printf 'the base fell back to HEAD~1 and scoped the gate to the last commit.\n' >&2
  sed -n '1,160p' /tmp/eshu-parser-relationship-kit.out >&2
  exit 1
fi

# The widened window must not fire on a branch with no parser change at all.
merge_base_clean_repo="$(init_repo merge-base-clean)"
git -C "${merge_base_clean_repo}" branch -M main
merge_base_clean_origin="${tmp_root}/merge-base-clean-origin"
git clone -q --bare "${merge_base_clean_repo}" "${merge_base_clean_origin}"
git -C "${merge_base_clean_repo}" remote add origin "${merge_base_clean_origin}"
git -C "${merge_base_clean_repo}" fetch -q origin
git -C "${merge_base_clean_repo}" checkout -q -b feature
printf '# docs only\n' >"${merge_base_clean_repo}/README.md"
git -C "${merge_base_clean_repo}" add .
git -C "${merge_base_clean_repo}" commit -q -m 'branch commit A: docs only'
printf '# docs only, again\n' >"${merge_base_clean_repo}/README.md"
git -C "${merge_base_clean_repo}" add .
git -C "${merge_base_clean_repo}" commit -q -m 'branch commit B: docs only'
if ! run_verifier_local_base "${merge_base_clean_repo}"; then
  printf 'expected a docs-only branch to PASS under the merge-base window\n' >&2
  sed -n '1,160p' /tmp/eshu-parser-relationship-kit.err >&2
  exit 1
fi

# Regression: the CI base path (GITHUB_BASE_REF -> origin/$GITHUB_BASE_REF),
# which every test above bypasses -- they either pin the base env var or leave
# GITHUB_BASE_REF unset. `git fetch origin <branch>` with no `<src>:<dst>`
# destination refspec only ever updates FETCH_HEAD, never
# refs/remotes/origin/<branch>, so under the verify-contracts job's
# fetch-depth: 2 checkout origin/$GITHUB_BASE_REF failed to resolve, the
# merge-base branch found no origin/main either, and the gate ran against
# HEAD~1 -- the tip commit alone -- on every PR run.
#
# The fixture is that checkout: a real shallow clone, a narrow fetch refspec
# that never names the base branch, and no origin/main. A default `git clone`
# configures the wildcard refspec under which even the old bareword fetch
# creates origin/main, and the fixture would prove nothing.
ci_base_repo="${tmp_root}/ci-base"
ci_base_origin="$(init_repo ci-base-origin)"
git -C "${ci_base_origin}" branch -M main
git clone -q --depth=1 "file://${ci_base_origin}" "${ci_base_repo}"
git -C "${ci_base_repo}" config user.email "test@example.invalid"
git -C "${ci_base_repo}" config user.name "Eshu Test"
git -C "${ci_base_repo}" config --unset-all remote.origin.fetch
git -C "${ci_base_repo}" config --add remote.origin.fetch \
  '+refs/heads/unrelated-pr-branch:refs/remotes/origin/unrelated-pr-branch'
git -C "${ci_base_repo}" update-ref -d refs/remotes/origin/main
git -C "${ci_base_repo}" checkout -q -b feature
if git -C "${ci_base_repo}" rev-parse --verify origin/main >/dev/null 2>&1; then
  printf 'fixture is wrong: origin/main resolves before the gate ever runs\n' >&2
  exit 1
fi

# Commit A adds an undocumented parser; commit B is the innocuous tip commit
# that a HEAD~1 base would have scoped the gate to.
printf 'package parser\nfunc parseNewLanguage() {}\n' \
  >"${ci_base_repo}/go/internal/parser/new_language.go"
printf 'package parser\nfunc TestNewLanguage(t interface{}) {}\n' \
  >"${ci_base_repo}/go/internal/parser/new_language_test.go"
git -C "${ci_base_repo}" add .
git -C "${ci_base_repo}" commit -q -m 'PR commit A: parser without docs'
printf '# readme\n' >"${ci_base_repo}/README.md"
git -C "${ci_base_repo}" add .
git -C "${ci_base_repo}" commit -q -m 'PR commit B: readme touch'

if env -u ESHU_PARSER_RELATIONSHIP_KIT_BASE \
  ESHU_PARSER_RELATIONSHIP_KIT_REPO_ROOT="${ci_base_repo}" \
  GITHUB_BASE_REF=main \
  "${verifier}" >/tmp/eshu-parser-relationship-kit.out \
  2>/tmp/eshu-parser-relationship-kit.err; then
  printf 'expected the CI-shaped gate to FAIL: the branch adds an undocumented\n' >&2
  printf 'parser in commit A and ends on an innocuous commit. A pass means the\n' >&2
  printf 'fetch never created origin/main and the base fell back to HEAD~1.\n' >&2
  sed -n '1,160p' /tmp/eshu-parser-relationship-kit.out >&2
  exit 1
fi
# Exit status alone does not say WHICH base the gate used. These two do: only
# the verifier's own fetch could have created origin/main in this clone, and
# that message fires only when a parser source file is inside the diff window.
if ! git -C "${ci_base_repo}" rev-parse --verify origin/main >/dev/null 2>&1; then
  printf 'expected the verifier fetch to create origin/main (destination refspec)\n' >&2
  exit 1
fi
if ! rg -q 'parser source changed without language/support docs update' \
  /tmp/eshu-parser-relationship-kit.err; then
  printf 'the gate failed, but not for commit A -- the CI base was not used\n' >&2
  sed -n '1,160p' /tmp/eshu-parser-relationship-kit.err >&2
  exit 1
fi
