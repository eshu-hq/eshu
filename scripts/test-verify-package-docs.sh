#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-package-docs.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

init_repo() {
  local name="$1"
  local dir="${tmp_root}/${name}"
  mkdir -p "${dir}"
  git -C "${dir}" init -q
  git -C "${dir}" config user.email "test@example.invalid"
  git -C "${dir}" config user.name "Eshu Test"
  mkdir -p "${dir}/go/internal/collector/base"
  printf 'package base\n' >"${dir}/go/internal/collector/base/doc.go"
  printf '# Base\n' >"${dir}/go/internal/collector/base/README.md"
  printf '# Base Agent Rules\n' >"${dir}/go/internal/collector/base/AGENTS.md"
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m initial
  printf '%s\n' "${dir}"
}

run_verifier() {
  local dir="$1"
  ESHU_PACKAGE_DOCS_REPO_ROOT="${dir}" \
    ESHU_PACKAGE_DOCS_BASE=HEAD~1 \
    "${verifier}" >/tmp/eshu-package-docs.out 2>/tmp/eshu-package-docs.err
}

expect_pass() {
  local dir="$1"
  if ! run_verifier "${dir}"; then
    printf 'expected verifier to pass in %s\n' "${dir}" >&2
    sed -n '1,120p' /tmp/eshu-package-docs.err >&2
    exit 1
  fi
}

expect_fail() {
  local dir="$1"
  if run_verifier "${dir}"; then
    printf 'expected verifier to fail in %s\n' "${dir}" >&2
    sed -n '1,120p' /tmp/eshu-package-docs.out >&2
    exit 1
  fi
}

missing_repo="$(init_repo missing)"
mkdir -p "${missing_repo}/go/internal/collector/terraformcloud"
printf 'package terraformcloud\n' >"${missing_repo}/go/internal/collector/terraformcloud/source.go"
git -C "${missing_repo}" add .
git -C "${missing_repo}" commit -q -m 'new package without docs'
expect_fail "${missing_repo}"

complete_repo="$(init_repo complete)"
mkdir -p "${complete_repo}/go/internal/collector/terraformcloud"
printf 'package terraformcloud\n' >"${complete_repo}/go/internal/collector/terraformcloud/source.go"
printf 'package terraformcloud\n' >"${complete_repo}/go/internal/collector/terraformcloud/doc.go"
printf '# Terraform Cloud Collector\n' >"${complete_repo}/go/internal/collector/terraformcloud/README.md"
printf '# Terraform Cloud Agent Rules\n' >"${complete_repo}/go/internal/collector/terraformcloud/AGENTS.md"
git -C "${complete_repo}" add .
git -C "${complete_repo}" commit -q -m 'new package with docs'
expect_pass "${complete_repo}"

test_only_repo="$(init_repo test-only)"
printf 'package base\n' >"${test_only_repo}/go/internal/collector/base/source_test.go"
git -C "${test_only_repo}" add .
git -C "${test_only_repo}" commit -q -m 'test-only change'
expect_pass "${test_only_repo}"

deleted_repo="$(init_repo deleted)"
mkdir -p "${deleted_repo}/go/internal/collector/removed"
printf 'package removed\n' >"${deleted_repo}/go/internal/collector/removed/source.go"
printf 'package removed\n' >"${deleted_repo}/go/internal/collector/removed/doc.go"
printf '# Removed Collector\n' >"${deleted_repo}/go/internal/collector/removed/README.md"
printf '# Removed Agent Rules\n' >"${deleted_repo}/go/internal/collector/removed/AGENTS.md"
git -C "${deleted_repo}" add .
git -C "${deleted_repo}" commit -q -m 'add package to delete'
git -C "${deleted_repo}" rm -q -r go/internal/collector/removed
git -C "${deleted_repo}" commit -q -m 'delete package'
expect_pass "${deleted_repo}"

# Regression (repo-root under GIT_DIR): the verifier must derive repo_root from
# its own location, not `git rev-parse --show-toplevel`. Git hooks export
# GIT_DIR, under which `git -C scripts rev-parse --show-toplevel` returns
# <repo>/scripts, so the per-package doc paths resolve under the wrong root and a
# package WITH docs is falsely reported missing. Run a COPY of the verifier from
# the fixture's scripts/ with GIT_DIR set and ESHU_PACKAGE_DOCS_REPO_ROOT unset;
# it must still resolve the fixture root and PASS.
gitdir_repo="$(init_repo gitdir)"
mkdir -p "${gitdir_repo}/scripts" "${gitdir_repo}/go/internal/collector/gitdircase"
cp "${verifier}" "${gitdir_repo}/scripts/verify-package-docs.sh"
printf 'package gitdircase\n' >"${gitdir_repo}/go/internal/collector/gitdircase/source.go"
printf 'package gitdircase\n' >"${gitdir_repo}/go/internal/collector/gitdircase/doc.go"
printf '# Gitdir Case\n' >"${gitdir_repo}/go/internal/collector/gitdircase/README.md"
printf '# Gitdir Agent Rules\n' >"${gitdir_repo}/go/internal/collector/gitdircase/AGENTS.md"
git -C "${gitdir_repo}" add .
git -C "${gitdir_repo}" commit -q -m 'new package with docs (gitdir fixture)'
if env -u ESHU_PACKAGE_DOCS_REPO_ROOT -u GITHUB_BASE_REF \
    GIT_DIR="${gitdir_repo}/.git" ESHU_PACKAGE_DOCS_BASE=HEAD~1 \
    "${gitdir_repo}/scripts/verify-package-docs.sh" \
    >/tmp/eshu-package-docs.out 2>/tmp/eshu-package-docs.err; then
  :
else
  printf 'expected verifier to resolve repo_root under GIT_DIR and pass\n' >&2
  sed -n '1,40p' /tmp/eshu-package-docs.err >&2
  exit 1
fi

# Regression (merge-base base): with no explicit base and no GITHUB_BASE_REF, the
# verifier must fall back to merge-base(origin/main, HEAD), not HEAD~1. On a
# branch with >1 commit past origin/main, a HEAD~1 base misses the first commit's
# changes; merge-base catches the whole branch. The fixture adds an undocumented
# package in commit B and an unrelated change in commit C (HEAD), with
# origin/main pinned at the initial commit: merge-base picks up B's package and
# fails; a HEAD~1 base would diff only C and wrongly pass.
mergebase_repo="$(init_repo mergebase)"
git -C "${mergebase_repo}" update-ref refs/remotes/origin/main HEAD
mkdir -p "${mergebase_repo}/go/internal/collector/branchpkg"
printf 'package branchpkg\n' >"${mergebase_repo}/go/internal/collector/branchpkg/source.go"
git -C "${mergebase_repo}" add .
git -C "${mergebase_repo}" commit -q -m 'B: undocumented package'
printf 'note\n' >>"${mergebase_repo}/go/internal/collector/base/README.md"
git -C "${mergebase_repo}" add .
git -C "${mergebase_repo}" commit -q -m 'C: unrelated doc change'
if env -u ESHU_PACKAGE_DOCS_BASE -u GITHUB_BASE_REF \
    ESHU_PACKAGE_DOCS_REPO_ROOT="${mergebase_repo}" \
    "${verifier}" >/tmp/eshu-package-docs.out 2>/tmp/eshu-package-docs.err; then
  printf 'expected merge-base fallback to flag branchpkg (added in commit B), but verifier passed\n' >&2
  sed -n '1,40p' /tmp/eshu-package-docs.out >&2
  exit 1
fi

printf 'verify-package-docs tests passed\n'
