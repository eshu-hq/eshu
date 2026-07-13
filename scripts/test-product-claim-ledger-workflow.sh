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

refuse_literal() {
  local description="$1"
  local literal="$2"

  if rg -q -F "${literal}" "${workflow}"; then
    record_fail "${description}"
  else
    record_pass "${description}"
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
require_literal "workflow verifies product claims through the narrower product-claims mode" 'go run ./cmd/capability-inventory -mode product-claims'
require_literal "workflow uploads failure logs" 'name: product-claim-ledger-report'

# #4073: the product-claim-ledger workflow must not repeat the full docs-tree
# scan that mcp-schema-drift.yml already runs on every PR via `-mode docs`. It
# uses the narrower `-mode product-claims` guard instead, checked above.
refuse_literal "workflow does not repeat the full docs-mode scan" 'go run ./cmd/capability-inventory -mode docs'

# #4073: one product-claim verify step now covers every trigger event
# (pull_request, push, schedule, workflow_dispatch), so the Actions token is
# passed exactly once instead of split across a PR-only and a trusted-event
# step.
token_count="$(rg -n -F 'GITHUB_TOKEN: ${{ github.token }}' "${workflow}" | wc -l | tr -d ' ')"
if [[ "${token_count}" -eq 1 ]]; then
  record_pass "live issue verification passes the Actions token exactly once for every triggering event"
else
  record_fail "live issue verification passes the Actions token exactly once for every triggering event"
fi

if [[ "${failures}" -ne 0 ]]; then
  printf 'product claim ledger workflow contract failed with %d finding(s)\n' "${failures}" >&2
  exit 1
fi

printf 'product claim ledger workflow contract passed\n'
