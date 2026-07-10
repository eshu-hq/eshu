#!/usr/bin/env bash
#
# test-generate-env-registry-doc.sh - hermetic checks for the generated
# environment variable reference doc gate.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
generator="${repo_root}/scripts/generate-env-registry-doc.sh"
verifier="${repo_root}/scripts/verify-env-registry-doc.sh"
doc_path="${repo_root}/docs/public/reference/env-registry.md"

tmp_root="$(mktemp -d)"
backup="${tmp_root}/env-registry.md"
trap 'cp "${backup}" "${doc_path}" 2>/dev/null || true; rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0

record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

assert_contains() {
  local needle="$1"
  local file="$2"
  if ! rg -q --fixed-strings "${needle}" "${file}"; then
    printf 'expected %s to contain: %s\n' "${file}" "${needle}" >&2
    cat "${file}" >&2
    exit 1
  fi
}

cp "${doc_path}" "${backup}"

out="${tmp_root}/generator.out"
if "${generator}" >"${out}" 2>&1 && cmp -s "${doc_path}" "${backup}"; then
  record_pass "generator is idempotent on a clean committed doc"
else
  record_fail "generator changed the committed doc on a clean run"
fi

if rg -q --fixed-strings "Generated from go/internal/envregistry. Do not edit by hand" "${doc_path}"; then
  record_pass "generated doc carries the generated marker"
else
  record_fail "generated doc is missing the generated marker"
fi

verify_out="${tmp_root}/verify.out"
if "${verifier}" >"${verify_out}" 2>&1; then
  record_pass "verifier accepts the committed generated doc"
else
  record_fail "verifier rejected the committed generated doc"
fi

printf '\n<!-- stale edit -->\n' >>"${doc_path}"
stale_out="${tmp_root}/stale.out"
if "${verifier}" >"${stale_out}" 2>&1; then
  record_fail "verifier accepted a stale generated doc"
else
  assert_contains "env-registry.md is out of date" "${stale_out}"
  record_pass "verifier rejects a stale generated doc"
fi

cp "${backup}" "${doc_path}"

if [ "${FAIL}" -ne 0 ]; then
  printf 'generate-env-registry-doc tests FAILED: %d/%d\n' "${FAIL}" "$((PASS + FAIL))" >&2
  exit 1
fi

printf 'generate-env-registry-doc tests passed: %d/%d\n' "${PASS}" "$((PASS + FAIL))"
