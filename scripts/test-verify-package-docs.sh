#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-package-docs.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

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
  printf '# AGENTS\n' >"${dir}/go/internal/collector/base/AGENTS.md"
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
printf '# AGENTS\n' >"${complete_repo}/go/internal/collector/terraformcloud/AGENTS.md"
git -C "${complete_repo}" add .
git -C "${complete_repo}" commit -q -m 'new package with docs'
expect_pass "${complete_repo}"

test_only_repo="$(init_repo test-only)"
printf 'package base\n' >"${test_only_repo}/go/internal/collector/base/source_test.go"
git -C "${test_only_repo}" add .
git -C "${test_only_repo}" commit -q -m 'test-only change'
expect_pass "${test_only_repo}"

printf 'verify-package-docs tests passed\n'
