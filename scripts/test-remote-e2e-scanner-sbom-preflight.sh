#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
preflight="${repo_root}/scripts/remote-e2e-scanner-sbom-preflight.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

run_preflight() {
  local root="$1"
  shift
  ESHU_SCANNER_WORKER_SBOM_MOUNTED_ROOT="${root}" \
    ESHU_SCANNER_WORKER_SBOM_HOST_ROOT="/redacted/scanner-sbom-root" \
    "$@" \
    /bin/sh "${preflight}" \
    >"${tmp_root}/preflight.out" 2>"${tmp_root}/preflight.err"
}

expect_pass() {
  local root="$1"
  shift
  if ! run_preflight "${root}" "$@"; then
    printf 'expected scanner SBOM preflight to pass\n' >&2
    sed -n '1,120p' "${tmp_root}/preflight.err" >&2
    exit 1
  fi
}

expect_fail_with() {
  local root="$1"
  local pattern="$2"
  shift 2
  if run_preflight "${root}" "$@"; then
    printf 'expected scanner SBOM preflight to fail with %s\n' "${pattern}" >&2
    sed -n '1,120p' "${tmp_root}/preflight.out" >&2
    exit 1
  fi
  if ! rg -q "${pattern}" "${tmp_root}/preflight.err"; then
    printf 'expected scanner SBOM preflight failure to contain %s\n' "${pattern}" >&2
    sed -n '1,120p' "${tmp_root}/preflight.err" >&2
    exit 1
  fi
  if rg -q 'scanner SBOM preflight passed' "${tmp_root}/preflight.out"; then
    printf 'scanner SBOM preflight emitted success output on failure\n' >&2
    sed -n '1,120p' "${tmp_root}/preflight.out" >&2
    exit 1
  fi
}

missing_root="${tmp_root}/missing"
expect_fail_with "${missing_root}" 'scanner SBOM root is not mounted'

empty_root="${tmp_root}/empty"
mkdir -p "${empty_root}"
expect_fail_with "${empty_root}" 'scanner SBOM root has no supported manifests'

unsupported_root="${tmp_root}/unsupported"
mkdir -p "${unsupported_root}/repo"
printf '{"name":"unsupported"}\n' >"${unsupported_root}/repo/package.json"
printf '[package]\nname = "unsupported"\n' >"${unsupported_root}/repo/Cargo.toml"
expect_fail_with "${unsupported_root}" 'scanner SBOM root has no supported manifests'

valid_root="${tmp_root}/valid"
mkdir -p "${valid_root}/go" "${valid_root}/js"
printf 'module example.test/preflight\n' >"${valid_root}/go/go.mod"
printf '{"lockfileVersion":3,"packages":{}}\n' >"${valid_root}/js/package-lock.json"
expect_fail_with \
  "${valid_root}" \
  'requires at least 3 supported manifest candidates' \
  env ESHU_SCANNER_WORKER_SBOM_MIN_CANDIDATES=3

expect_pass "${valid_root}" env ESHU_SCANNER_WORKER_SBOM_MIN_CANDIDATES=2
if ! rg -q 'supported_manifest_candidates=2' "${tmp_root}/preflight.out"; then
  printf 'scanner SBOM preflight did not report the expected candidate count\n' >&2
  sed -n '1,120p' "${tmp_root}/preflight.out" >&2
  exit 1
fi
if rg -q '/redacted/scanner-sbom-root' "${tmp_root}/preflight.out"; then
  printf 'scanner SBOM preflight leaked the host root value\n' >&2
  sed -n '1,120p' "${tmp_root}/preflight.out" >&2
  exit 1
fi

composer_root="${tmp_root}/composer"
mkdir -p "${composer_root}/php"
printf '{"packages":[]}\n' >"${composer_root}/php/composer.lock"
expect_pass "${composer_root}"

deep_root="${tmp_root}/deep"
mkdir -p "${deep_root}/apps/team/service"
printf 'module example.test/deep\n' >"${deep_root}/apps/team/service/go.mod"
expect_pass "${deep_root}"

skipped_root="${tmp_root}/skipped"
mkdir -p "${skipped_root}/vendor" "${skipped_root}/node_modules" "${skipped_root}/.git"
printf 'module example.test/skipped\n' >"${skipped_root}/vendor/go.mod"
printf '{"lockfileVersion":3,"packages":{}}\n' >"${skipped_root}/node_modules/package-lock.json"
printf 'module example.test/git\n' >"${skipped_root}/.git/go.mod"
expect_fail_with "${skipped_root}" 'scanner SBOM root has no supported manifests'

printf 'remote-e2e-scanner-sbom-preflight tests passed\n'
