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
# easy to write so that it passes on any failure at all, or on the right failure
# with a second one hiding behind it — either way a false green wearing the
# costume of a gate. Cases 3, 4 and 5 are what rule that out.
#
# Fast, credential-free, Docker-free, network-free.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gate="${repo_root}/scripts/verify-value-flow-conformance-expectation.sh"
corpus_source="${repo_root}/go/internal/backendconformance/corpus.go"
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

# ── 4. A second failure behind the documented one → the gate fails ─────────
#
# The documented shape has exactly one failure in it. A run can carry a second
# one in two ways, and they need different checks because neither sees the
# other.
#
# TestLiveBackendConformance calls t.Fatalf from two deferred closures, and a
# defer runs AFTER the read-corpus t.Fatalf has already recorded the documented
# failure, so both messages land in the same run. go test prints ONE "--- FAIL:"
# line per test however many messages that test recorded — checked against a
# real go test -v run of that exact shape — so counting failed tests is blind
# to this one, and the gate has to name the two co-occurring messages instead.

stub="$(make_stub nornicdb-and-cleanup-failure 1 \
	"${included_banner}" \
	"    live_test.go:101: run nornicdb live read corpus: ${expected_failure}" \
	'    live_test.go:78: cleanup live corpus fixture: execute write group: write tx failed' \
	'--- FAIL: TestLiveBackendConformance (3.23s)')"
run_gate nornicdb "${stub}"
expect_status "the documented failure plus a cleanup failure fails the gate" 1
# Match the gate's own verdict, not the marker. The gate echoes the lane log, so
# the marker is in the output either way and asserting on it would pass without
# the gate having noticed anything.
expect_output "the co-occurring cleanup failure is named in the verdict" \
	'recorded a second failure: "cleanup live corpus fixture:"'

stub="$(make_stub nornicdb-and-close-failure 1 \
	"${included_banner}" \
	"    live_test.go:101: run nornicdb live read corpus: ${expected_failure}" \
	'    live_test.go:63: close Bolt driver: context deadline exceeded' \
	'--- FAIL: TestLiveBackendConformance (3.23s)')"
run_gate nornicdb "${stub}"
expect_status "the documented failure plus a driver-close failure fails the gate" 1
expect_output "the co-occurring close failure is named in the verdict" \
	'recorded a second failure: "close Bolt driver:"'

# The other way is a SECOND FAILING TEST, whose failure text is its own and so
# cannot be enumerated ahead of time. Counting "--- FAIL:" lines is what catches
# that one.
#
# This is DEFENCE IN DEPTH, not a reachable shape today, and the difference is
# worth stating so the check is not read as proof the shape was observed.
# scripts/verify_backend_conformance_live.sh runs three `go test` invocations on
# the nornicdb lane, but under `set -euo pipefail` with none of them guarded, so
# a failing TestLiveBackendConformance exits the driver at the first one and the
# other two never run. A second "--- FAIL:" therefore cannot co-occur with the
# documented message unless that driver is restructured to run all three
# unconditionally -- which is exactly when this check starts earning its keep.

stub="$(make_stub nornicdb-second-failing-test 1 \
	"${included_banner}" \
	"    live_test.go:101: run nornicdb live read corpus: ${expected_failure}" \
	'--- FAIL: TestLiveBackendConformance (3.23s)' \
	'--- FAIL: TestLiveNornicDBRetryConflictClassificationContract (0.41s)')"
run_gate nornicdb "${stub}"
expect_status "a second failing test fails the gate" 1
expect_output "a second failing test is named as such" 'more than one test failed'

# And the shape the gate exists to accept still passes, so neither check above
# rejects a clean documented red. These lines are the nornicdb transcript
# recorded in go/internal/backendconformance/evidence-notes.md.

stub="$(make_stub nornicdb-real-transcript 1 \
	"${included_banner}" \
	"    live_test.go:101: run nornicdb live read corpus: ${expected_failure}" \
	'--- FAIL: TestLiveBackendConformance (3.23s)' \
	'FAIL' \
	'FAIL	github.com/eshu-hq/eshu/go/internal/backendconformance	3.512s' \
	'FAIL')"
run_gate nornicdb "${stub}"
expect_status "the real one-failure transcript still passes the gate" 0
expect_output "the real transcript is still reported as documented" 'failed as documented'

# ── 5. The pair never ran → the gate fails ─────────────────────────────────
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

# ── 6. nornicdb lane PASSES → upstream landed, and the gate says so ────────

stub="$(make_stub nornicdb-upstream-fixed 0 "${included_banner}" \
	'ok  github.com/eshu-hq/eshu/go/internal/backendconformance  3.9s')"
run_gate nornicdb "${stub}"
expect_status "a passing nornicdb lane fails the gate" 1
expect_output "the upstream-landed verdict names the one-step repair" \
	'valueFlowCasesEnabled'
expect_output "the upstream-landed verdict names the file to edit" \
	'go/internal/backendconformance/corpus_value_flow.go'

# ── 7. neo4j positive control ──────────────────────────────────────────────

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

# ── 8. Usage errors are distinguishable from gate verdicts ─────────────────

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

# ── 9. The needle still matches what the corpus actually emits ─────────────
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

# The gate names two failures that can land alongside the documented one, and it
# names them because they are the two live_test.go raises from a DEFERRED
# closure — a defer runs after the read-corpus failure is already recorded, so
# only those two can co-occur. That list was derived by reading the failures
# live_test.go can raise, so it goes stale the moment someone adds another one.
# Pin the set here. A new entry is not automatically a gate change; it is a
# prompt to work out whether the new failure can co-occur, and to extend
# cooccurring_failures in the gate if it can.
known_live_test_failures="$(
	LC_ALL=C rg --only-matching --no-line-number --replace '$1' \
		't\.Fatalf\("([^"]*)"' "${live_test_source}" | LC_ALL=C sort -u
)"
expected_live_test_failures="$(
	printf '%s\n' \
		'clean live corpus fixture: %v' \
		'cleanup live corpus fixture: %v' \
		'close Bolt driver: %v' \
		'load graph backend: %v' \
		'open Bolt driver: %v' \
		'run %s live read corpus: %v' \
		'run %s live write corpus attempt %d: %v'
)"
if [[ "${known_live_test_failures}" == "${expected_live_test_failures}" ]]; then
	record_pass "live_test.go still raises only the failures the gate's co-occurrence list was derived from"
else
	record_fail "live_test.go's t.Fatalf set changed. Work out whether any new one can be
recorded ALONGSIDE the read-corpus failure (a t.Fatalf in a deferred closure can;
one on the straight-line path cannot, because the first Fatalf ends the test),
then extend cooccurring_failures in the gate and this list together.
want:
${expected_live_test_failures}
got:
${known_live_test_failures}"
fi

# ── 10. Both lanes are wired into the same CI job ───────────────────────────
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
