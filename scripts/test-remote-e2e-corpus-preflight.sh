#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
preflight="${repo_root}/scripts/remote-e2e-corpus-preflight.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

make_corpus() {
  local root="$1"
  local count="$2"
  local n
  rm -rf "${root}"
  mkdir -p "${root}"
  for n in $(seq 1 "${count}"); do
    mkdir -p "${root}/repo-${n}/.git"
  done
}

run_preflight() {
  local root="$1"
  shift
  ESHU_REMOTE_E2E_MOUNTED_ROOT="${root}" \
    ESHU_FILESYSTEM_HOST_ROOT="/redacted/representative-corpus" \
    "$@" \
    /bin/sh "${preflight}" \
    >"${tmp_root}/preflight.out" 2>"${tmp_root}/preflight.err"
}

expect_pass() {
  local root="$1"
  shift
  if ! run_preflight "${root}" "$@"; then
    printf 'expected representative corpus preflight to pass\n' >&2
    sed -n '1,120p' "${tmp_root}/preflight.err" >&2
    exit 1
  fi
}

expect_fail_with() {
  local root="$1"
  local pattern="$2"
  shift 2
  if run_preflight "${root}" "$@"; then
    printf 'expected representative corpus preflight to fail with %s\n' "${pattern}" >&2
    sed -n '1,120p' "${tmp_root}/preflight.out" >&2
    exit 1
  fi
  if ! rg -q "${pattern}" "${tmp_root}/preflight.err"; then
    printf 'expected failure output to contain %s\n' "${pattern}" >&2
    sed -n '1,120p' "${tmp_root}/preflight.err" >&2
    exit 1
  fi
}

representative_root="${tmp_root}/representative"

make_corpus "${representative_root}" 19
expect_fail_with \
  "${representative_root}" \
  'representative-corpus mode requires at least 20 candidate repository roots' \
  env ESHU_REMOTE_E2E_CORPUS_MODE=representative

make_corpus "${representative_root}" 20
expect_pass "${representative_root}" env ESHU_REMOTE_E2E_CORPUS_MODE=representative
if ! rg -q 'mode=representative candidate_repository_roots=20 git_repository_roots=20' "${tmp_root}/preflight.out"; then
  printf 'representative preflight did not report the expected bounded corpus counts\n' >&2
  sed -n '1,120p' "${tmp_root}/preflight.out" >&2
  exit 1
fi

make_corpus "${representative_root}" 51
expect_fail_with \
  "${representative_root}" \
  'representative-corpus mode allows at most 50 candidate repository roots' \
  env ESHU_REMOTE_E2E_CORPUS_MODE=representative

make_corpus "${representative_root}" 20
expect_fail_with \
  "${representative_root}" \
  'ESHU_REMOTE_E2E_CORPUS_MODE must be one of smoke, representative, full' \
  env ESHU_REMOTE_E2E_CORPUS_MODE=wide-open

printf 'remote-e2e-corpus-preflight tests passed\n'
