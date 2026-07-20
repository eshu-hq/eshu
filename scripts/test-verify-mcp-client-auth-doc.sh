#!/usr/bin/env bash
#
# test-verify-mcp-client-auth-doc.sh - hermetic checks for the MCP
# client-auth doc lockstep guard (scripts/verify-mcp-client-auth-doc.sh,
# issue #5169, F-8).
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-mcp-client-auth-doc.sh"
doc_path="${repo_root}/docs/public/operate/mcp-client-auth.md"

tmp_root="$(mktemp -d)"
backup="${tmp_root}/mcp-client-auth.md"
trap 'cp "${backup}" "${doc_path}" 2>/dev/null || true; rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0

record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

assert_contains() {
  local needle="$1"
  local file="$2"
  local label="$3"
  if rg -q --fixed-strings -- "${needle}" "${file}"; then
    record_pass "${label}"
  else
    printf 'expected %s to contain: %s\n' "${file}" "${needle}" >&2
    cat "${file}" >&2
    record_fail "${label}"
  fi
}

cp "${doc_path}" "${backup}"

# Case 1: the committed doc, as-is, must pass.
verify_out="${tmp_root}/verify.out"
if "${verifier}" >"${verify_out}" 2>&1; then
  record_pass "verifier accepts the committed doc"
else
  cat "${verify_out}" >&2
  record_fail "verifier rejected the committed doc"
fi

assert_contains "OK: 4 literal(s) checked" "${verify_out}" "verifier reports the checked literal count"

# Case 2: stripping every line that names ESHU_MCP_TOKEN must fail the
# verifier and name the missing literal in its output.
rg -v --fixed-strings 'ESHU_MCP_TOKEN' "${doc_path}" >"${tmp_root}/stripped.md" || true
cp "${tmp_root}/stripped.md" "${doc_path}"

stale_out="${tmp_root}/stale.out"
if "${verifier}" >"${stale_out}" 2>&1; then
  record_fail "verifier accepted a doc missing a pinned literal"
else
  assert_contains "ESHU_MCP_TOKEN" "${stale_out}" "verifier rejects a doc missing a pinned literal and names it"
fi

cp "${backup}" "${doc_path}"

# Case 3: a missing doc file must fail closed, not silently pass.
rm -f "${doc_path}"
missing_out="${tmp_root}/missing.out"
if "${verifier}" >"${missing_out}" 2>&1; then
  record_fail "verifier accepted a missing doc file"
else
  record_pass "verifier rejects a missing doc file"
fi
cp "${backup}" "${doc_path}"

if [ "${FAIL}" -ne 0 ]; then
  printf 'verify-mcp-client-auth-doc tests FAILED: %d/%d\n' "${FAIL}" "$((PASS + FAIL))" >&2
  exit 1
fi

printf 'verify-mcp-client-auth-doc tests passed: %d/%d\n' "${PASS}" "$((PASS + FAIL))"
