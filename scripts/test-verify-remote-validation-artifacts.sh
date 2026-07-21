#!/usr/bin/env bash
#
# test-verify-remote-validation-artifacts.sh - hermetic tests for
# scripts/verify-remote-validation-artifacts.sh (#5407, PR 2 of #5336).
#
# The RED/GREEN classification logic (CheckRemoteValidationArtifacts,
# LoadRemoteValidationBaseline, RenderRemoteValidationBaseline) already has
# full unit coverage in go/internal/capabilitycatalog/remote_validation_test.go
# and CLI-level coverage in
# go/cmd/capability-inventory/remote_validation_mode_test.go. This script
# proves the bash entrypoint itself: argument parsing, exit codes, and that
# -update actually clears a subsequent check — an integration proof of the
# wrapper, not a re-implementation of the Go logic it wraps.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-remote-validation-artifacts.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

PASS=0
FAIL=0
record_pass() {
	PASS=$((PASS + 1))
	printf 'ok - %s\n' "$1"
}
record_fail() {
	FAIL=$((FAIL + 1))
	printf 'not ok - %s\n' "$1" >&2
}

# write_matrix writes a scratch capability matrix citing one remote_validation
# ref under dir/specs.
write_matrix() {
	local specs_dir="$1" ref="$2"
	mkdir -p "${specs_dir}"
	cat >"${specs_dir}/capability-matrix.v1.yaml" <<YAML
capabilities:
  - capability: code_search.exact_symbol
    tools: [find_code]
    profiles:
      production: {status: supported, max_truth_level: exact, verification: [{remote_validation: ${ref}}]}
YAML
}

# Case 1: an unbaselined, artifact-less ref fails the gate.
case1_root="${tmp_root}/case1"
case1_specs="${case1_root}/specs"
write_matrix "${case1_specs}" "prod-case1-dangling"
if "${verifier}" --specs "${case1_specs}" --root "${case1_root}" \
	--baseline "${case1_specs}/remote-validation-baseline.txt" \
	>"${tmp_root}/case1.out" 2>&1; then
	record_fail "unbaselined dangling ref fails the gate"
	cat "${tmp_root}/case1.out" >&2
else
	if rg -q --fixed-strings "prod-case1-dangling" "${tmp_root}/case1.out"; then
		record_pass "unbaselined dangling ref fails the gate"
	else
		record_fail "unbaselined dangling ref fails the gate (missing ref in output)"
		cat "${tmp_root}/case1.out" >&2
	fi
fi

# Case 2: the same ref passes once listed in the baseline.
case2_root="${tmp_root}/case2"
case2_specs="${case2_root}/specs"
write_matrix "${case2_specs}" "prod-case2-baselined"
printf 'prod-case2-baselined\n' >"${case2_specs}/remote-validation-baseline.txt"
if "${verifier}" --specs "${case2_specs}" --root "${case2_root}" \
	--baseline "${case2_specs}/remote-validation-baseline.txt" \
	>"${tmp_root}/case2.out" 2>&1; then
	record_pass "baselined dangling ref passes the gate"
else
	record_fail "baselined dangling ref passes the gate"
	cat "${tmp_root}/case2.out" >&2
fi

# Case 3: the same ref passes once a committed artifact exists, no baseline
# entry needed.
case3_root="${tmp_root}/case3"
case3_specs="${case3_root}/specs"
write_matrix "${case3_specs}" "prod-case3-has-artifact"
mkdir -p "${case3_root}/docs/internal/remote-validation"
printf '# evidence\n' >"${case3_root}/docs/internal/remote-validation/prod-case3-has-artifact.md"
if "${verifier}" --specs "${case3_specs}" --root "${case3_root}" \
	--baseline "${case3_specs}/remote-validation-baseline.txt" \
	>"${tmp_root}/case3.out" 2>&1; then
	record_pass "ref with a committed artifact passes without baselining"
else
	record_fail "ref with a committed artifact passes without baselining"
	cat "${tmp_root}/case3.out" >&2
fi

# Case 4: -update regenerates the baseline to exactly the current dangling
# set, and a subsequent check against that regenerated baseline passes.
case4_root="${tmp_root}/case4"
case4_specs="${case4_root}/specs"
write_matrix "${case4_specs}" "prod-case4-regen"
case4_baseline="${case4_specs}/remote-validation-baseline.txt"
"${verifier}" -update --specs "${case4_specs}" --root "${case4_root}" \
	--baseline "${case4_baseline}" >"${tmp_root}/case4-update.out" 2>&1
if rg -q --fixed-strings "prod-case4-regen" "${case4_baseline}"; then
	record_pass "-update writes the current dangling ref to the baseline"
else
	record_fail "-update writes the current dangling ref to the baseline"
	cat "${case4_baseline}" >&2
fi
if "${verifier}" --specs "${case4_specs}" --root "${case4_root}" \
	--baseline "${case4_baseline}" >"${tmp_root}/case4-check.out" 2>&1; then
	record_pass "check passes immediately after -update"
else
	record_fail "check passes immediately after -update"
	cat "${tmp_root}/case4-check.out" >&2
fi

# Case 5: a malformed baseline line fails closed (never silently skipped).
case5_root="${tmp_root}/case5"
case5_specs="${case5_root}/specs"
write_matrix "${case5_specs}" "prod-case5-dangling"
printf 'Not A Valid Slug\n' >"${case5_specs}/remote-validation-baseline.txt"
if "${verifier}" --specs "${case5_specs}" --root "${case5_root}" \
	--baseline "${case5_specs}/remote-validation-baseline.txt" \
	>"${tmp_root}/case5.out" 2>&1; then
	record_fail "malformed baseline line fails closed"
	cat "${tmp_root}/case5.out" >&2
else
	record_pass "malformed baseline line fails closed"
fi

# Case 6: the real repo tree passes against the committed baseline (the
# actual gate this script protects in CI).
if "${verifier}" >"${tmp_root}/case6.out" 2>&1; then
	record_pass "real repo tree passes against the committed baseline"
else
	record_fail "real repo tree passes against the committed baseline"
	cat "${tmp_root}/case6.out" >&2
fi

printf '\n%d passed, %d failed\n' "${PASS}" "${FAIL}"
if [[ "${FAIL}" -gt 0 ]]; then
	exit 1
fi
