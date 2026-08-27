#!/usr/bin/env bash
# Test mirror for scripts/verify-value-flow-conformance-expectation.sh (#6192).
#
# The gate it mirrors needs two live Bolt backends, so nothing about it can be
# proven on a laptop or on a credential-free runner by running it for real.
# What CAN be proven, and is what actually decides whether the gate is worth
# having, is that every verdict is reachable and that none of them is reached
# for the wrong reason. Each case below drives the gate with a stub lane whose
# output and exit code are fixed, so the verdict is the only variable.
#
# The cases that matter most are the negative ones. An expected-fail gate is
# easy to write so that it passes on any failure at all, which makes it a false
# green wearing the costume of a gate; cases 3 and 4 are what rule that out.
#
# Fast, credential-free, Docker-free, network-free.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gate="${repo_root}/scripts/verify-value-flow-conformance-expectation.sh"
corpus_source="${repo_root}/go/internal/backendconformance/corpus.go"
corpus_value_flow="${repo_root}/go/internal/backendconformance/corpus_value_flow.go"
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

# The exact case name and message shape the gate matches on. Kept here as
# independent literals rather than read out of the gate, so a change to the
# gate's needle has to be made in two places and cannot pass this mirror by
# agreeing with itself.
case_name='value-flow cloud sink aggregation and subscript projection'
expected_failure="read case \"${case_name}\" returned 0 rows, want at least 1"
included_banner='  value-flow cloud sink pair: INCLUDED (ESHU_BACKEND_CONFORMANCE_VALUE_FLOW is set)'

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
		printf 'printf "%%s\\n" "value_flow=${ESHU_BACKEND_CONFORMANCE_VALUE_FLOW:-unset}"\n'
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

# ── 1. nornicdb lane fails as documented → the gate passes ─────────────────

stub="$(make_stub nornicdb-documented 1 "${included_banner}" "${expected_failure}")"
run_gate nornicdb "${stub}"
expect_status "nornicdb lane failing with the documented message passes the gate" 0
expect_output "the documented nornicdb failure is reported as such" \
	'failed as documented'
expect_output "the observed exit code is printed for the evidence note" \
	'nornicdb lane: observed exit code 1'

# ── 2. The gate sets the opt-in and the backend it claims to ───────────────

expect_output "the gate runs the lane it was asked for" 'backend=nornicdb'
expect_output "the gate sets the value-flow opt-in" 'value_flow=1'

# ── 3. nornicdb lane failing for a DIFFERENT reason → the gate fails ───────
#
# This is the case that separates a gate from a rubber stamp. A broken fixture,
# a failed seed, and a refused Bolt connection all exit non-zero; if any of them
# satisfied the expectation, the gate would go green while proving nothing.

stub="$(make_stub nornicdb-other-failure 1 \
	"${included_banner}" \
	'dial tcp 127.0.0.1:7687: connect: connection refused')"
run_gate nornicdb "${stub}"
expect_status "nornicdb lane failing for another reason fails the gate" 1
expect_output "an off-message failure says the message was missing" \
	'WITHOUT naming the value-flow read case'

# ── 4. The pair never ran → the gate fails ─────────────────────────────────
#
# With the opt-in unset the pair is ABSENT from the corpus rather than skipped,
# so a run without the INCLUDED banner proves nothing either way. Asserting the
# banner is what stops a green neo4j lane from being read as a positive control
# when the case was never in the corpus.

stub="$(make_stub neo4j-omitted 0 \
	'  value-flow cloud sink pair: OMITTED -- ESHU_BACKEND_CONFORMANCE_VALUE_FLOW is not set.')"
run_gate neo4j "${stub}"
expect_status "a lane that omitted the pair fails the gate" 1
expect_output "an omitted pair is named as such" 'never included the value-flow pair'

# ── 5. nornicdb lane PASSES → upstream landed, and the gate says so ────────

stub="$(make_stub nornicdb-upstream-fixed 0 "${included_banner}" \
	'ok  github.com/eshu-hq/eshu/go/internal/backendconformance  3.9s')"
run_gate nornicdb "${stub}"
expect_status "a passing nornicdb lane fails the gate" 1
expect_output "the upstream-landed verdict names the one-step repair" \
	'valueFlowCasesEnabled'
expect_output "the upstream-landed verdict names the file to edit" \
	'go/internal/backendconformance/corpus_value_flow.go'

# ── 6. neo4j positive control ──────────────────────────────────────────────

stub="$(make_stub neo4j-documented 0 "${included_banner}" \
	'ok  github.com/eshu-hq/eshu/go/internal/backendconformance  3.7s')"
run_gate neo4j "${stub}"
expect_status "neo4j lane passing passes the gate" 0
expect_output "the neo4j observed exit code is printed" 'neo4j lane: observed exit code 0'
expect_output "the gate runs the neo4j lane" 'backend=neo4j'

stub="$(make_stub neo4j-broken 1 "${included_banner}" "${expected_failure}")"
run_gate neo4j "${stub}"
expect_status "neo4j lane failing fails the gate" 1
expect_output "a failed control says the other lane proves nothing" \
	'positive control'

# ── 7. Usage errors are distinguishable from gate verdicts ─────────────────

set +e
"${gate}" > "${work_dir}/usage.txt" 2>&1
usage_status=$?
set -e
if [[ "${usage_status}" -eq 2 ]]; then
	record_pass "a missing lane argument exits 2, not 1"
else
	record_fail "a missing lane argument should exit 2, got ${usage_status}"
fi

set +e
"${gate}" postgres > "${work_dir}/usage.txt" 2>&1
usage_status=$?
set -e
if [[ "${usage_status}" -eq 2 ]]; then
	record_pass "an unknown lane exits 2, not 1"
else
	record_fail "an unknown lane should exit 2, got ${usage_status}"
fi

# ── 8. The needle still matches what the corpus actually emits ─────────────
#
# The gate rests entirely on one string. If corpus.go's format string or the
# case name changes and the gate's needle does not, every nornicdb run becomes
# an off-message failure and the gate fails loudly — but only once someone runs
# it against a live backend. These two checks move that discovery to here.

if rg --fixed-strings --quiet -- 'read case %q returned %d rows, want at least %d' "${corpus_source}"; then
	record_pass "corpus.go still emits the message shape the gate matches"
else
	record_fail "corpus.go no longer emits 'read case %q returned %d rows, want at least %d'; update the gate's needle"
fi
if rg --fixed-strings --quiet -- "valueFlowReadCaseName  = \"${case_name}\"" "${corpus_value_flow}"; then
	record_pass "the read case name the gate matches is still the corpus constant"
else
	record_fail "valueFlowReadCaseName no longer equals \"${case_name}\"; update the gate's needle"
fi

# ── 9. Both lanes are wired into the same CI job ───────────────────────────
#
# The positive control is only a control if it runs alongside the lane it
# controls for. Matched as live code, so commenting a lane out fails here
# rather than leaving the job green with half its evidence.

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
	record_pass "the expectation workflow exists"
	for lane in nornicdb neo4j; do
		if live_code_has "verify-value-flow-conformance-expectation.sh ${lane}" "${workflow}"; then
			record_pass "the workflow runs the ${lane} lane as live code"
		else
			record_fail "the workflow does not run the ${lane} lane as live code"
		fi
	done
	if [[ "$(rg --count '^  [a-z0-9_-]+:$' "${workflow}" || true)" != "" ]]; then
		record_pass "the workflow parses as a job map"
	else
		record_fail "the workflow declares no jobs"
	fi
else
	record_fail "missing ${workflow}"
fi

printf 'tests passed: %d/%d\n' "${passed}" "${total}"
[[ "${passed}" -eq "${total}" ]]
