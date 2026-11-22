#!/usr/bin/env bash
# test-read-api-latency-core.sh - hermetic test for the sweep-leg bookkeeping in
# scripts/lib/read-api-latency-core.sh (issue #11170): both legs run even when
# the first one fails, and the gate is still red when either leg failed. Needs no
# Docker, Postgres, or Go build: the gate binary is a stub whose exit codes the
# test scripts leg by leg.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

fail() {
	printf 'test-read-api-latency-core: FAIL: %s\n' "$*" >&2
	exit 1
}

# A stub gate: appends its leg marker to a call log and exits with the next code
# from the codes file (one per call).
mkdir -p "${work}/bin"
cat >"${work}/bin/eshu-read-api-latency-gate" <<'STUB'
#!/usr/bin/env bash
code="$(head -n1 "${GATE_STUB_CODES}")"
tail -n +2 "${GATE_STUB_CODES}" >"${GATE_STUB_CODES}.next" && mv "${GATE_STUB_CODES}.next" "${GATE_STUB_CODES}"
printf 'call\n' >>"${GATE_STUB_CALLS}"
exit "${code}"
STUB
chmod +x "${work}/bin/eshu-read-api-latency-gate"

# Run the lib's leg helpers in a subshell with the given stub exit codes and
# print "<calls> <exit status of the final gate verdict>".
scenario() {
	local codes="$1"
	printf '%s\n' ${codes} >"${work}/codes"
	: >"${work}/calls"
	(
		export GATE_STUB_CODES="${work}/codes" GATE_STUB_CALLS="${work}/calls"
		bin_dir="${work}/bin"
		GATE_ITERATIONS=1 GATE_RUNS=1
		gate_common_args=()
		log() { :; }
		die() { printf 'die: %s\n' "$*" >&2; exit 1; }
		# shellcheck source=scripts/lib/read-api-latency-core.sh
		. "${repo_root}/scripts/lib/read-api-latency-core.sh"
		run_sweep_leg legacy budgets.txt work.txt 2>/dev/null
		run_sweep_leg winners budgets.txt work.txt 2>/dev/null
		fail_if_any_leg_failed
	) >/dev/null 2>&1 && status=0 || status=$?
	printf '%s %s' "$(wc -l <"${work}/calls" | tr -d ' ')" "${status}"
}

# RED shape: legacy fails, winners passes. Both legs must still run, and the
# gate must still fail.
[[ "$(scenario '1 0')" == "2 1" ]] || fail "a failed legacy leg must still run the winners leg and fail the gate, got: $(scenario '1 0')"
# Winners fails, legacy passes.
[[ "$(scenario '0 1')" == "2 1" ]] || fail "a failed winners leg must fail the gate, got: $(scenario '0 1')"
# Both fail.
[[ "$(scenario '1 1')" == "2 1" ]] || fail "two failed legs must fail the gate, got: $(scenario '1 1')"
# GREEN: both pass.
[[ "$(scenario '0 0')" == "2 0" ]] || fail "two passing legs must pass the gate, got: $(scenario '0 0')"

printf 'test-read-api-latency-core: PASS\n'
