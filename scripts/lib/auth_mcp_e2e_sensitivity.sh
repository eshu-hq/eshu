#!/usr/bin/env bash
# Shared assertions for the MCP-identity E2E sensitivity gate (F-9, issue
# #5170). Factored out of scripts/verify-auth-mcp-e2e-sensitivity.sh so the
# report-parsing logic — the part that distinguishes "the negative module
# FAILED on the mutated gate" from "the runner CRASHED before it could test the
# gate" — is unit-testable without booting Docker
# (scripts/test-verify-auth-mcp-e2e-sensitivity.sh drives these against
# synthetic report fixtures).

auth_mcp_sensitivity_die() {
	printf 'auth-mcp-e2e-sensitivity: %s\n' "$*" >&2
	return 1
}

# auth_mcp_sensitivity_step_status echoes the status ("pass"/"fail") of the
# named step in the given report JSON, or empty string when the step is absent.
auth_mcp_sensitivity_step_status() {
	local report="$1"
	local step_id="$2"
	jq -r --arg id "${step_id}" \
		'(.results // []) | map(select(.id == $id)) | (.[0].status // "")' \
		"${report}"
}

# auth_mcp_sensitivity_assert_step_failed asserts the report EXISTS, is valid
# JSON, and CONTAINS the named step with status "fail" — i.e. the negative
# module ran and the mutated gate defeated it. A missing report or a missing
# step means the runner crashed before exercising the gate (an infra error, NOT
# the inverted-exit signal), which this rejects distinctly.
auth_mcp_sensitivity_assert_step_failed() {
	local report="$1"
	local step_id="$2"
	[[ -f "${report}" ]] || {
		auth_mcp_sensitivity_die "mutated run wrote no report (${report}); the runner crashed before testing the gate, it did not fail ON the gate"
		return 1
	}
	jq -e . "${report}" >/dev/null 2>&1 || {
		auth_mcp_sensitivity_die "mutated run report is not valid JSON (${report}); treated as a crash, not a gate failure"
		return 1
	}
	local status
	status="$(auth_mcp_sensitivity_step_status "${report}" "${step_id}")"
	case "${status}" in
	fail)
		return 0
		;;
	"")
		auth_mcp_sensitivity_die "mutated run report has no '${step_id}' step; the runner crashed before the negative probe, it did not fail ON the gate"
		return 1
		;;
	*)
		auth_mcp_sensitivity_die "mutated run '${step_id}' step status is '${status}', expected 'fail' — the mutated gate did NOT defeat the negative module (sensitivity not proven)"
		return 1
		;;
	esac
}

# auth_mcp_sensitivity_assert_step_passed asserts the named step exists with
# status "pass" — the baseline/restored expectation against the real gate.
auth_mcp_sensitivity_assert_step_passed() {
	local report="$1"
	local step_id="$2"
	[[ -f "${report}" ]] || {
		auth_mcp_sensitivity_die "run wrote no report (${report})"
		return 1
	}
	jq -e . "${report}" >/dev/null 2>&1 || {
		auth_mcp_sensitivity_die "run report is not valid JSON (${report})"
		return 1
	}
	local status
	status="$(auth_mcp_sensitivity_step_status "${report}" "${step_id}")"
	[[ "${status}" == "pass" ]] || {
		auth_mcp_sensitivity_die "expected '${step_id}' to pass against the real gate, got status '${status:-<absent>}'"
		return 1
	}
}
