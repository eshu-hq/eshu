#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
preflight="${repo_root}/scripts/remote-e2e-confluence-preflight.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

run_preflight() {
  "$@" /bin/sh "${preflight}" >"${tmp_root}/preflight.out" 2>"${tmp_root}/preflight.err"
}

expect_pass() {
  if ! run_preflight "$@"; then
    printf 'expected Confluence preflight to pass\n' >&2
    sed -n '1,120p' "${tmp_root}/preflight.err" >&2
    exit 1
  fi
}

expect_fail_with() {
  local pattern="$1"
  shift
  if run_preflight "$@"; then
    printf 'expected Confluence preflight to fail with %s\n' "${pattern}" >&2
    sed -n '1,120p' "${tmp_root}/preflight.out" >&2
    exit 1
  fi
  if ! rg -q "${pattern}" "${tmp_root}/preflight.err"; then
    printf 'expected failure output to contain %s\n' "${pattern}" >&2
    sed -n '1,120p' "${tmp_root}/preflight.err" >&2
    exit 1
  fi
}

base_auth=(
  env
  ESHU_CONFLUENCE_BASE_URL=https://confluence.example.invalid/wiki
  ESHU_CONFLUENCE_BEARER_TOKEN=redacted-token
)

expect_fail_with \
  'ESHU_CONFLUENCE_BASE_URL is required' \
  env ESHU_CONFLUENCE_SPACE_ID=100 ESHU_CONFLUENCE_BEARER_TOKEN=redacted-token

expect_fail_with \
  'exactly one bounded selector is required; set one of ESHU_CONFLUENCE_SPACE_ID, ESHU_CONFLUENCE_SPACE_IDS, or ESHU_CONFLUENCE_ROOT_PAGE_ID' \
  "${base_auth[@]}"

expect_fail_with \
  'configure only one of ESHU_CONFLUENCE_SPACE_ID, ESHU_CONFLUENCE_SPACE_IDS, or ESHU_CONFLUENCE_ROOT_PAGE_ID' \
  "${base_auth[@]}" ESHU_CONFLUENCE_SPACE_ID=100 ESHU_CONFLUENCE_ROOT_PAGE_ID=200

expect_fail_with \
  'read-only credentials are required; set ESHU_CONFLUENCE_BEARER_TOKEN or both ESHU_CONFLUENCE_EMAIL and ESHU_CONFLUENCE_API_TOKEN' \
  env ESHU_CONFLUENCE_BASE_URL=https://confluence.example.invalid/wiki ESHU_CONFLUENCE_SPACE_ID=100

expect_pass "${base_auth[@]}" ESHU_CONFLUENCE_SPACE_ID=100
if ! rg -q 'selector=ESHU_CONFLUENCE_SPACE_ID auth_mode=bearer' "${tmp_root}/preflight.out"; then
  printf 'expected single-space bearer proof output\n' >&2
  sed -n '1,120p' "${tmp_root}/preflight.out" >&2
  exit 1
fi

expect_pass "${base_auth[@]}" ESHU_CONFLUENCE_SPACE_IDS=100,200
if ! rg -q 'selector=ESHU_CONFLUENCE_SPACE_IDS auth_mode=bearer' "${tmp_root}/preflight.out"; then
  printf 'expected multi-space bearer proof output\n' >&2
  sed -n '1,120p' "${tmp_root}/preflight.out" >&2
  exit 1
fi

expect_pass \
  env \
  ESHU_CONFLUENCE_BASE_URL=https://confluence.example.invalid/wiki \
  ESHU_CONFLUENCE_ROOT_PAGE_ID=200 \
  ESHU_CONFLUENCE_EMAIL=reader@example.invalid \
  ESHU_CONFLUENCE_API_TOKEN=redacted-token
if ! rg -q 'selector=ESHU_CONFLUENCE_ROOT_PAGE_ID auth_mode=email_api_token' "${tmp_root}/preflight.out"; then
  printf 'expected root-page email token proof output\n' >&2
  sed -n '1,120p' "${tmp_root}/preflight.out" >&2
  exit 1
fi

printf 'remote-e2e-confluence-preflight tests passed\n'
