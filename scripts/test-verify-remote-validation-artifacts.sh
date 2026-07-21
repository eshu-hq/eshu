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

# write_frozen writes the immutable frozen-set file beside the baseline. The
# gate requires baseline ⊆ frozen and fails closed if the file is absent, so
# every case whose baseline is meant to pass must declare its frozen slugs.
write_frozen() {
	local specs_dir="$1"
	shift
	mkdir -p "${specs_dir}"
	{
		printf '# FROZEN — immutable audited-at-introduction set\n'
		local slug
		for slug in "$@"; do
			printf '%s\n' "${slug}"
		done
	} >"${specs_dir}/remote-validation-frozen.txt"
}

# Case 1: an unbaselined, artifact-less ref fails the gate.
case1_root="${tmp_root}/case1"
case1_specs="${case1_root}/specs"
write_matrix "${case1_specs}" "prod-case1-dangling"
write_frozen "${case1_specs}" "prod-case1-dangling"
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

# Case 2: the same ref passes once listed in the baseline (with a FROZEN_MAX
# ceiling that the single entry does not exceed).
case2_root="${tmp_root}/case2"
case2_specs="${case2_root}/specs"
write_matrix "${case2_specs}" "prod-case2-baselined"
write_frozen "${case2_specs}" "prod-case2-baselined"
printf '# FROZEN_MAX: 1\nprod-case2-baselined\n' >"${case2_specs}/remote-validation-baseline.txt"
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
write_frozen "${case3_specs}"
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
# -update never writes the frozen set; the regenerated ref must already be
# frozen for the subsequent check to pass.
write_frozen "${case4_specs}" "prod-case4-regen"
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

# Case 7: baseline GROWTH past FROZEN_MAX fails closed. The cited ref is
# baselined (no artifact finding), but a second entry pushes the count over a
# frozen ceiling of 1 — the anti-append-smuggling guard.
case7_root="${tmp_root}/case7"
case7_specs="${case7_root}/specs"
write_matrix "${case7_specs}" "prod-case7-baselined"
# Both baseline slugs are frozen, so this isolates the FROZEN_MAX ceiling
# violation rather than the frozen-membership guard.
write_frozen "${case7_specs}" "prod-case7-baselined" "prod-case7-smuggled"
printf '# FROZEN_MAX: 1\nprod-case7-baselined\nprod-case7-smuggled\n' \
	>"${case7_specs}/remote-validation-baseline.txt"
if "${verifier}" --specs "${case7_specs}" --root "${case7_root}" \
	--baseline "${case7_specs}/remote-validation-baseline.txt" \
	>"${tmp_root}/case7.out" 2>&1; then
	record_fail "baseline growth past FROZEN_MAX fails closed"
	cat "${tmp_root}/case7.out" >&2
else
	if rg -q --fixed-strings "EXCEEDS frozen ceiling" "${tmp_root}/case7.out"; then
		record_pass "baseline growth past FROZEN_MAX fails closed"
	else
		record_fail "baseline growth past FROZEN_MAX fails closed (missing ceiling message)"
		cat "${tmp_root}/case7.out" >&2
	fi
fi

# Case 8: the constant-count atomic swap fails the frozen-membership guard.
# The matrix cites a burned-down ref (A, now artifact-backed) plus two dangling
# refs (B, C). The frozen set is the original audited pair {A, B}. The swapped
# baseline drops A and adds the NEW unbacked claim C, keeping the count at the
# ceiling of 2 — so the FROZEN_MAX ceiling alone passes — but C is not in the
# frozen set, so baseline ⊄ frozen and the gate must fail naming C.
case8_root="${tmp_root}/case8"
case8_specs="${case8_root}/specs"
mkdir -p "${case8_specs}"
cat >"${case8_specs}/capability-matrix.v1.yaml" <<'YAML'
capabilities:
  - capability: cap.a
    tools: [find_code]
    profiles:
      production: {status: supported, max_truth_level: exact, verification: [{remote_validation: prod-case8-a}]}
  - capability: cap.b
    tools: [find_code]
    profiles:
      production: {status: supported, max_truth_level: exact, verification: [{remote_validation: prod-case8-b}]}
  - capability: cap.c
    tools: [find_code]
    profiles:
      production: {status: supported, max_truth_level: exact, verification: [{remote_validation: prod-case8-smuggled}]}
YAML
mkdir -p "${case8_root}/docs/internal/remote-validation"
printf '# evidence\n' >"${case8_root}/docs/internal/remote-validation/prod-case8-a.md"
write_frozen "${case8_specs}" "prod-case8-a" "prod-case8-b"
printf '# FROZEN_MAX: 2\nprod-case8-b\nprod-case8-smuggled\n' \
	>"${case8_specs}/remote-validation-baseline.txt"
if "${verifier}" --specs "${case8_specs}" --root "${case8_root}" \
	--baseline "${case8_specs}/remote-validation-baseline.txt" \
	>"${tmp_root}/case8.out" 2>&1; then
	record_fail "atomic swap (constant count) fails the frozen-membership guard"
	cat "${tmp_root}/case8.out" >&2
else
	if rg -q --fixed-strings "prod-case8-smuggled" "${tmp_root}/case8.out" &&
		rg -q --fixed-strings "not in frozen set" "${tmp_root}/case8.out"; then
		record_pass "atomic swap (constant count) fails the frozen-membership guard"
	else
		record_fail "atomic swap guard missing the smuggled ref or the frozen-set message"
		cat "${tmp_root}/case8.out" >&2
	fi
fi

# Case 9: an absent frozen file fails the check closed (the atomic-swap defense
# must never be silently absent). The baselined ref would otherwise pass.
case9_root="${tmp_root}/case9"
case9_specs="${case9_root}/specs"
write_matrix "${case9_specs}" "prod-case9-baselined"
printf '# FROZEN_MAX: 1\nprod-case9-baselined\n' >"${case9_specs}/remote-validation-baseline.txt"
if "${verifier}" --specs "${case9_specs}" --root "${case9_root}" \
	--baseline "${case9_specs}/remote-validation-baseline.txt" \
	>"${tmp_root}/case9.out" 2>&1; then
	record_fail "absent frozen file fails the gate closed"
	cat "${tmp_root}/case9.out" >&2
else
	record_pass "absent frozen file fails the gate closed"
fi

printf '\n%d passed, %d failed\n' "${PASS}" "${FAIL}"
if [[ "${FAIL}" -gt 0 ]]; then
	exit 1
fi
