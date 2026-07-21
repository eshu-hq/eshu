#!/usr/bin/env bash
# Test mirror for scripts/lib/auth_mcp_e2e_sensitivity.sh (F-9, issue #5170).
# Drives the report-parsing assertions — the part of the sensitivity gate that
# distinguishes "the negative module FAILED on the mutated gate" from "the
# runner CRASHED before testing the gate" — against synthetic report fixtures,
# so that load-bearing logic is proven without booting Docker.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/lib/auth_mcp_e2e_sensitivity.sh
source "${repo_root}/scripts/lib/auth_mcp_e2e_sensitivity.sh"

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

pass_count=0
fail_count=0

ok() {
	printf 'ok   - %s\n' "$1"
	pass_count=$((pass_count + 1))
}
notok() {
	printf 'FAIL - %s\n' "$1"
	fail_count=$((fail_count + 1))
}

STEP="leakage_credentialless_probes_do_not_leak"

write_report() {
	local path="$1"
	local status="$2"
	cat >"${path}" <<EOF
{"apiBase":"http://x","mcpBase":"http://y","module":"credentialless","totalMs":10,
 "results":[{"id":"${STEP}","status":"${status}","detail":"d","ms":5}]}
EOF
}

# --- assert_step_failed: passes only when the step is present AND status=fail.
mutated_fail="${tmpdir}/mutated-fail.json"
write_report "${mutated_fail}" "fail"
if auth_mcp_sensitivity_assert_step_failed "${mutated_fail}" "${STEP}" 2>/dev/null; then
	ok "assert_step_failed accepts a report whose step failed on the gate"
else
	notok "assert_step_failed should accept status=fail"
fi

# A step that PASSED under mutation means the gate was NOT detected -> reject.
mutated_pass="${tmpdir}/mutated-pass.json"
write_report "${mutated_pass}" "pass"
if auth_mcp_sensitivity_assert_step_failed "${mutated_pass}" "${STEP}" 2>/dev/null; then
	notok "assert_step_failed must reject status=pass (gate not detected)"
else
	ok "assert_step_failed rejects status=pass (gate not detected)"
fi

# A missing report = the runner crashed before writing anything -> reject as a
# crash, distinctly from a gate failure.
if auth_mcp_sensitivity_assert_step_failed "${tmpdir}/does-not-exist.json" "${STEP}" 2>/dev/null; then
	notok "assert_step_failed must reject a missing report (crash, not gate failure)"
else
	ok "assert_step_failed rejects a missing report (crash, not gate failure)"
fi

# A report missing the step = the runner crashed before the negative probe.
no_step="${tmpdir}/no-step.json"
cat >"${no_step}" <<EOF
{"results":[{"id":"some_other_step","status":"pass","detail":"d","ms":1}]}
EOF
if auth_mcp_sensitivity_assert_step_failed "${no_step}" "${STEP}" 2>/dev/null; then
	notok "assert_step_failed must reject a report missing the step (crash)"
else
	ok "assert_step_failed rejects a report missing the step (crash)"
fi

# Invalid JSON = crash/corruption -> reject.
bad_json="${tmpdir}/bad.json"
printf 'not json {{{' >"${bad_json}"
if auth_mcp_sensitivity_assert_step_failed "${bad_json}" "${STEP}" 2>/dev/null; then
	notok "assert_step_failed must reject invalid JSON"
else
	ok "assert_step_failed rejects invalid JSON"
fi

# --- assert_step_passed: passes only on status=pass.
baseline_pass="${tmpdir}/baseline-pass.json"
write_report "${baseline_pass}" "pass"
if auth_mcp_sensitivity_assert_step_passed "${baseline_pass}" "${STEP}" 2>/dev/null; then
	ok "assert_step_passed accepts status=pass against the real gate"
else
	notok "assert_step_passed should accept status=pass"
fi

if auth_mcp_sensitivity_assert_step_passed "${mutated_fail}" "${STEP}" 2>/dev/null; then
	notok "assert_step_passed must reject status=fail"
else
	ok "assert_step_passed rejects status=fail"
fi

# --- step_status echoes the raw status / empty for absent.
if [[ "$(auth_mcp_sensitivity_step_status "${baseline_pass}" "${STEP}")" == "pass" ]]; then
	ok "step_status echoes 'pass'"
else
	notok "step_status should echo 'pass'"
fi
if [[ -z "$(auth_mcp_sensitivity_step_status "${no_step}" "${STEP}")" ]]; then
	ok "step_status echoes empty for an absent step"
else
	notok "step_status should echo empty for an absent step"
fi

printf '\n%d passed, %d failed\n' "${pass_count}" "${fail_count}"
[[ "${fail_count}" -eq 0 ]]
