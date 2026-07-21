#!/usr/bin/env bash
# Test mirror for scripts/lib/auth_mcp_e2e_manifest.sh (F-9, issue #5170).
# Drives the baseline-vs-report comparison against synthetic fixtures AND the
# real committed baseline, proving the manifest verifier catches step drift,
# status regressions, reordering, and runtime-bound breaches without booting
# the suite.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/lib/auth_mcp_e2e_manifest.sh
source "${repo_root}/scripts/lib/auth_mcp_e2e_manifest.sh"

baseline="${repo_root}/testdata/golden/auth-mcp-e2e-baseline.json"
tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

pass_count=0
fail_count=0
ok() { printf 'ok   - %s\n' "$1"; pass_count=$((pass_count + 1)); }
notok() { printf 'FAIL - %s\n' "$1"; fail_count=$((fail_count + 1)); }

# synthetic_report_from_baseline builds a report whose results mirror the
# committed baseline's steps (all pass, in order) with the given totalMs.
synthetic_report_from_baseline() {
	local total_ms="$1"
	jq -n --slurpfile b "${baseline}" --argjson total "${total_ms}" '
		{apiBase: "http://x", mcpBase: "http://y", module: "all", totalMs: $total,
		 results: ($b[0].steps | map({id: .id, status: .status, detail: "d", ms: 1}))}
	'
}

# --- A conforming report passes.
good="${tmpdir}/good.json"
synthetic_report_from_baseline 60000 >"${good}"
if validate_auth_mcp_e2e_manifest "${baseline}" "${good}" >/dev/null 2>&1; then
	ok "a conforming in-order all-pass report within the runtime bound validates"
else
	notok "conforming report should validate"
fi

# --- A failed step is rejected.
one_fail="${tmpdir}/one-fail.json"
jq '.results[3].status = "fail"' "${good}" >"${one_fail}"
if validate_auth_mcp_e2e_manifest "${baseline}" "${one_fail}" >/dev/null 2>&1; then
	notok "a report with a failed step must be rejected"
else
	ok "a report with a failed step is rejected (status mismatch)"
fi

# --- A missing step is rejected (step-id list differs).
missing_step="${tmpdir}/missing.json"
jq '.results |= .[1:]' "${good}" >"${missing_step}"
if validate_auth_mcp_e2e_manifest "${baseline}" "${missing_step}" >/dev/null 2>&1; then
	notok "a report missing a step must be rejected"
else
	ok "a report missing a step is rejected (step-id list mismatch)"
fi

# --- An extra step is rejected.
extra_step="${tmpdir}/extra.json"
jq '.results += [{id: "unexpected_new_step", status: "pass", detail: "d", ms: 1}]' "${good}" >"${extra_step}"
if validate_auth_mcp_e2e_manifest "${baseline}" "${extra_step}" >/dev/null 2>&1; then
	notok "a report with an extra step must be rejected"
else
	ok "a report with an extra step is rejected (step-id list mismatch)"
fi

# --- Reordering is rejected (order-sensitive).
reordered="${tmpdir}/reordered.json"
jq '.results |= [.[1], .[0]] + .[2:]' "${good}" >"${reordered}"
if validate_auth_mcp_e2e_manifest "${baseline}" "${reordered}" >/dev/null 2>&1; then
	notok "a reordered report must be rejected"
else
	ok "a reordered report is rejected (order-sensitive step-id list)"
fi

# --- Runtime-bound breach is rejected.
too_slow="${tmpdir}/slow.json"
synthetic_report_from_baseline 9999000 >"${too_slow}"
if validate_auth_mcp_e2e_manifest "${baseline}" "${too_slow}" >/dev/null 2>&1; then
	notok "a report exceeding max_total_seconds must be rejected"
else
	ok "a report exceeding the runtime bound is rejected"
fi

# --- A missing report file is rejected.
if validate_auth_mcp_e2e_manifest "${baseline}" "${tmpdir}/nope.json" >/dev/null 2>&1; then
	notok "a missing report must be rejected"
else
	ok "a missing report is rejected"
fi

# --- The committed baseline itself is a well-formed contract.
if auth_mcp_manifest_validate_baseline_shape "${baseline}" >/dev/null 2>&1; then
	ok "the committed baseline passes its own shape validation"
else
	notok "the committed baseline must be a well-formed contract"
fi

printf '\n%d passed, %d failed\n' "${pass_count}" "${fail_count}"
[[ "${fail_count}" -eq 0 ]]
