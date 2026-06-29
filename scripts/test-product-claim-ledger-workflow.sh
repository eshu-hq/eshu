#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
workflow="${repo_root}/.github/workflows/product-claim-ledger.yml"

failures=0

record_pass() {
  printf 'ok - %s\n' "$1"
}

record_fail() {
  printf 'not ok - %s\n' "$1" >&2
  failures=$((failures + 1))
}

require_literal() {
  local description="$1"
  local literal="$2"

  if rg -q -F "${literal}" "${workflow}"; then
    record_pass "${description}"
  else
    record_fail "${description}"
  fi
}

require_regex() {
  local description="$1"
  local pattern="$2"

  if rg -q "${pattern}" "${workflow}"; then
    record_pass "${description}"
  else
    record_fail "${description}"
  fi
}

if [[ -f "${workflow}" ]]; then
  record_pass "product claim ledger workflow exists"
else
  record_fail "product claim ledger workflow exists"
  exit 1
fi

require_regex "workflow runs on pull requests" '^  pull_request:$'
require_regex "workflow runs on main pushes" '^  push:$'
require_regex "workflow supports manual dispatch" '^  workflow_dispatch:$'
require_regex "workflow has scheduled verification" '^  schedule:$'
require_literal "workflow grants read-only issue access" '  issues: read'
require_literal "workflow enables live issue verification" 'ESHU_VERIFY_PRODUCT_CLAIM_ISSUES_LIVE: "1"'
require_literal "workflow verifies product claims through docs mode" 'go run ./cmd/capability-inventory -mode docs'
require_literal "workflow uploads failure logs" 'name: product-claim-ledger-report'

token_count="$(rg -n -F 'GITHUB_TOKEN: ${{ github.token }}' "${workflow}" | wc -l | tr -d ' ')"
if [[ "${token_count}" -ge 2 ]]; then
  record_pass "live issue verification passes the Actions token on PR and trusted events"
else
  record_fail "live issue verification passes the Actions token on PR and trusted events"
fi

if [[ "${failures}" -ne 0 ]]; then
  printf 'product claim ledger workflow contract failed with %d finding(s)\n' "${failures}" >&2
  exit 1
fi

printf 'product claim ledger workflow contract passed\n'
