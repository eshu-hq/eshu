#!/usr/bin/env bash
# Tests that the ESLint flat configuration at the repo root is wired up to
# the rules the O3 acceptance contract requires (issue #3763):
#
#   * no-console
#   * @typescript-eslint/no-unused-vars
#   * import/order
#
# Approach: invoke eslint with the test config in tests/lint-fixture/ against
# three fixtures in the same directory. The bad fixture MUST fail with
# non-zero exit and MUST surface no-console + no-unused-vars in the report.
# The clean fixture MUST pass with zero exit. The import-order fixture MUST
# fail and surface the import/order rule id. Together, the three cases prove
# the gate fails on violations and passes on clean code.
#
# Exits non-zero on any assertion failure. Designed to be run from the repo
# root after `npm ci` (so node_modules/.bin/eslint is available) and to be
# invoked from CI as well.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

eslint_bin="${repo_root}/node_modules/.bin/eslint"
if [ ! -x "${eslint_bin}" ]; then
  printf 'test-eslint-config: eslint binary not installed at %s; run npm ci first\n' "${eslint_bin}" >&2
  exit 1
fi

config_path="${repo_root}/tests/lint-fixture/eslint.test.config.mjs"
if [ ! -f "${config_path}" ]; then
  printf 'test-eslint-config: missing test config at %s\n' "${config_path}" >&2
  exit 1
fi

fixture_dir="${repo_root}/tests/lint-fixture"

assert_fail_with_rule() {
  local fixture_name="$1"
  local rule_id="$2"
  local fixture_path="${fixture_dir}/${fixture_name}"
  local out_path
  out_path="$(mktemp)"
  local status

  printf 'test-eslint-config: running eslint on %s (expect non-zero exit with %s)\n' "${fixture_name}" "${rule_id}"
  set +e
  "${eslint_bin}" --config "${config_path}" "${fixture_path}" >"${out_path}" 2>&1
  status=$?
  set -e

  if [ "${status}" -eq 0 ]; then
    printf 'test-eslint-config: expected %s to FAIL eslint but it passed\n' "${fixture_name}" >&2
    sed -n '1,200p' "${out_path}" >&2
    rm -f "${out_path}"
    exit 1
  fi
  if ! grep -q "${rule_id}" "${out_path}"; then
    printf 'test-eslint-config: expected rule "%s" in %s report, got:\n' "${rule_id}" "${fixture_name}" >&2
    sed -n '1,200p' "${out_path}" >&2
    rm -f "${out_path}"
    exit 1
  fi
  printf 'test-eslint-config: %s correctly failed eslint (exit %d, rule %s reported)\n' "${fixture_name}" "${status}" "${rule_id}"
  rm -f "${out_path}"
}

assert_pass() {
  local fixture_name="$1"
  local fixture_path="${fixture_dir}/${fixture_name}"
  local out_path
  out_path="$(mktemp)"
  local status

  printf 'test-eslint-config: running eslint on %s (expect zero exit)\n' "${fixture_name}"
  set +e
  "${eslint_bin}" --config "${config_path}" "${fixture_path}" >"${out_path}" 2>&1
  status=$?
  set -e

  if [ "${status}" -ne 0 ]; then
    printf 'test-eslint-config: expected %s to PASS eslint but it failed with exit %d\n' "${fixture_name}" "${status}" >&2
    sed -n '1,200p' "${out_path}" >&2
    rm -f "${out_path}"
    exit 1
  fi
  printf 'test-eslint-config: %s correctly passed eslint\n' "${fixture_name}"
  rm -f "${out_path}"
}

# Case 1: bad fixture — no-console + no-unused-vars.
assert_fail_with_rule "bad.ts" "no-console"
assert_fail_with_rule "bad.ts" "no-unused-vars"

# Case 2: clean fixture — no violations.
assert_pass "clean.ts"

# Case 3: import/order — groups out of order (react before node:fs).
assert_fail_with_rule "order-bad.ts" "import/order"

printf 'test-eslint-config: ok\n'
