#!/usr/bin/env bash
# Test mirror for scripts/verify-value-flow-conformance-expectation.sh (#6192,
# #6690).
#
# The gate it mirrors needs a live Bolt backend per lane, so it cannot be
# proven on a credential-free runner by running it for real. What can be proven
# is that every verdict is reachable and that none is reached for the wrong
# reason. Each case drives the gate with a stub lane whose output and exit code
# are fixed, so the verdict is the only variable.
#
# The negative cases matter most: a positive gate is easy to write so that it
# passes on any exit 0, including a run that never executed the value-flow
# cases. Cases 3 and 4 rule that out.
#
# Fast, credential-free, Docker-free, network-free.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gate="${repo_root}/scripts/verify-value-flow-conformance-expectation.sh"
corpus_value_flow="${repo_root}/go/internal/backendconformance/corpus_value_flow.go"
live_test_source="${repo_root}/go/internal/backendconformance/live_test.go"
workflow="${repo_root}/.github/workflows/value-flow-conformance-expectation.yml"

passed=0
total=0

record_pass() {
	total=$((total + 1))
	passed=$((passed + 1))
	printf 'PASS: %s\n' "$1"
}

record_fail() {
	total=$((total + 1))
	printf 'FAIL: %s\n' "$1" >&2
}

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/test-value-flow-expectation.XXXXXX")"
trap 'rm -rf "${work_dir}"' EXIT

# The case names, kept here as independent literals rather than read out of the
# gate, so a change to the gate's markers has to be made in two places and
# cannot pass this mirror by agreeing with itself.
rows_case='value-flow cloud action workload rows'
targets_case='value-flow cloud sink targets by pair'
rows_marker="    live_test.go:96: read case passed: ${rows_case} (4 rows)"
targets_marker="    live_test.go:96: read case passed: ${targets_case} (1 rows)"

# make_stub <name> <exit-code> <line>... — write a stub live-conformance driver
# that prints the given lines and exits with the given code. Built with printf,
# never a heredoc: a heredoc body between 512 bytes and the pipe-buffer ceiling
# deadlocks under bash 5.1+ (#5019).
make_stub() {
	local name="$1" exit_code="$2"
	shift 2
	local path="${work_dir}/${name}"
	{
		printf '#!/usr/bin/env bash\n'
		printf 'printf "%%s\\n" "backend=${ESHU_GRAPH_BACKEND:-unset}"\n'
		local line
		for line in "$@"; do
			printf 'printf "%%s\\n" %q\n' "${line}"
		done
		printf 'exit %s\n' "${exit_code}"
	} > "${path}"
	chmod +x "${path}"
	printf '%s' "${path}"
}

# run_gate <lane> <stub> — run the gate against a stub and record its status
# and output. Status is captured directly, never through a pipe.
gate_status=0
gate_output=''
run_gate() {
	local lane="$1" stub="$2" out="${work_dir}/gate-out.txt"
	set +e
	"${gate}" "${lane}" "${stub}" > "${out}" 2>&1
	gate_status=$?
	set -e
	gate_output="$(cat "${out}")"
}

expect_status() {
	local label="$1" want="$2"
	if [[ "${gate_status}" -eq "${want}" ]]; then
		record_pass "${label} (exit ${gate_status})"
	else
		record_fail "${label}: want exit ${want}, got ${gate_status}
${gate_output}"
	fi
}

expect_output() {
	local label="$1" needle="$2"
	if [[ "${gate_output}" == *"${needle}"* ]]; then
		record_pass "${label}"
	else
		record_fail "${label}: output does not mention \"${needle}\"
${gate_output}"
	fi
}

# ── 0. Structural checks ───────────────────────────────────────────────────

if [[ -x "${gate}" ]]; then
	record_pass "gate script exists and is executable"
else
	record_fail "missing or non-executable gate script: ${gate}"
fi
if bash -n "${gate}" 2> "${work_dir}/syntax.txt"; then
	record_pass "gate script parses"
else
	record_fail "gate script has a syntax error: $(cat "${work_dir}/syntax.txt")"
fi
if rg --fixed-strings --quiet -- 'set -euo pipefail' "${gate}"; then
	record_pass "gate script runs under strict mode"
else
	record_fail "gate script is missing 'set -euo pipefail'"
fi

# ── 1. A passing lane that ran both cases → the gate passes ────────────────

for lane in nornicdb neo4j; do
	stub="$(make_stub "pass-${lane}" 0 \
		"${rows_marker}" "${targets_marker}" "--- PASS: TestLiveBackendConformance (0.11s)" "ok")"
	run_gate "${lane}" "${stub}"
	expect_status "${lane} lane passing with both value-flow cases → gate passes" 0
	expect_output "${lane} lane: the gate says both cases ran" "both value-flow cases ran"
	# ── 2. The gate hands the driver the backend it claims to test ─────────
	expect_output "${lane} lane: the gate sets ESHU_GRAPH_BACKEND" "backend=${lane}"
done

# ── 3. A failing lane → the gate fails, for either backend ─────────────────

for lane in nornicdb neo4j; do
	stub="$(make_stub "fail-${lane}" 1 \
		"    corpus: read case \"${rows_case}\": returned rows differ from the exact expected rows" \
		"--- FAIL: TestLiveBackendConformance (0.11s)")"
	run_gate "${lane}" "${stub}"
	expect_status "${lane} lane red → gate fails" 1
	expect_output "${lane} lane red: the gate names the lane" "${lane} lane FAILED"
done

# ── 4. A green run that never ran the value-flow cases → the gate fails ────
#
# This is the case that separates the gate from a rubber stamp: exit 0 with the
# cases missing must not count.

stub="$(make_stub "pass-no-markers" 0 "--- PASS: TestLiveBackendConformance (0.11s)" "ok")"
run_gate nornicdb "${stub}"
expect_status "green run with neither value-flow case → gate fails" 1
expect_output "the gate names the missing case" "WITHOUT running a value-flow case"

stub="$(make_stub "pass-one-marker" 0 "${rows_marker}" "--- PASS: TestLiveBackendConformance (0.11s)")"
run_gate neo4j "${stub}"
expect_status "green run with only one value-flow case → gate fails" 1
expect_output "the gate names the case that did not run" "${targets_case}"

# ── 5. Usage errors are distinguishable from gate verdicts ─────────────────

set +e
"${gate}" > /dev/null 2>&1
usage_status=$?
"${gate}" sqlite > /dev/null 2>&1
bad_lane_status=$?
"${gate}" nornicdb "${work_dir}/does-not-exist" > /dev/null 2>&1
bad_driver_status=$?
set -e
if [[ "${usage_status}" -eq 2 && "${bad_lane_status}" -eq 2 ]]; then
	record_pass "missing or unknown lane is a usage error (exit 2)"
else
	record_fail "usage errors: want exit 2/2, got ${usage_status}/${bad_lane_status}"
fi
if [[ "${bad_driver_status}" -eq 1 ]]; then
	record_pass "a non-executable driver fails the gate (exit 1)"
else
	record_fail "non-executable driver: want exit 1, got ${bad_driver_status}"
fi

# ── 6. The markers still match what the corpus and live test emit ──────────
#
# The gate rests on two case names and one log format. If either changes and
# the gate's markers do not, every live run becomes a "never ran" failure, but
# only once someone runs it against a backend. These checks move that here.

if rg --fixed-strings --quiet -- "valueFlowWorkloadRowsCaseName = \"${rows_case}\"" "${corpus_value_flow}"; then
	record_pass "valueFlowWorkloadRowsCaseName still matches the gate's marker"
else
	record_fail "valueFlowWorkloadRowsCaseName no longer equals \"${rows_case}\"; update the gate"
fi
if rg --fixed-strings --quiet -- "valueFlowTargetsCaseName      = \"${targets_case}\"" "${corpus_value_flow}"; then
	record_pass "valueFlowTargetsCaseName still matches the gate's marker"
else
	record_fail "valueFlowTargetsCaseName no longer equals \"${targets_case}\"; update the gate"
fi
if rg --fixed-strings --quiet -- 't.Logf("read case passed: %s (%d rows)", result.Name, result.Rows)' "${live_test_source}"; then
	record_pass "live_test.go still logs the marker format the gate matches"
else
	record_fail "live_test.go no longer logs 'read case passed: <name> (<n> rows)'; update the gate"
fi
for case_name in "${rows_case}" "${targets_case}"; do
	if rg --fixed-strings --quiet -- "read case passed: ${case_name}" "${gate}"; then
		record_pass "the gate requires the \"${case_name}\" marker"
	else
		record_fail "the gate does not require the \"${case_name}\" marker"
	fi
done

# ── 7. Both lanes are wired into the same CI job ───────────────────────────
#
# Matched as live code, so commenting a lane out fails here rather than leaving
# the job green with half its evidence.

live_code_has() {
	local needle="$1" file="$2" line stripped
	while IFS= read -r line || [[ -n "${line}" ]]; do
		stripped="${line#"${line%%[![:space:]]*}"}"
		[[ "${stripped}" == "#"* ]] && continue
		[[ "${line}" == *"${needle}"* ]] && return 0
	done < "${file}"
	return 1
}

if [[ -f "${workflow}" ]]; then
	record_pass "the value-flow workflow exists"
	for lane in nornicdb neo4j; do
		if live_code_has "verify-value-flow-conformance-expectation.sh ${lane}" "${workflow}"; then
			record_pass "the workflow runs the ${lane} lane as live code"
		else
			record_fail "the workflow does not run the ${lane} lane as live code"
		fi
	done
	if live_code_has "ESHU_BACKEND_CONFORMANCE_VALUE_FLOW" "${workflow}"; then
		record_fail "the workflow still sets the retired ESHU_BACKEND_CONFORMANCE_VALUE_FLOW opt-in"
	else
		record_pass "the workflow no longer sets the retired opt-in"
	fi
else
	record_fail "missing ${workflow}"
fi

printf 'tests passed: %d/%d\n' "${passed}" "${total}"
[[ "${passed}" -eq "${total}" ]]
