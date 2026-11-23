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
	read -ra code_list <<<"${codes}"
	printf '%s\n' "${code_list[@]}" >"${work}/codes"
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

# --- helper guards (review of #11170) ---------------------------------------
# lib_call <function> [args...] runs one lib function in a subshell with stubbed
# log/die and a stubbed curl (a shell function the subshell inherits), and
# returns its status. The stub answers from STUB_CODE/STUB_BODY_FILE and records
# every URL it was asked for in STUB_URL_LOG; like curl -f it fails on >= 400.
STUB_URL_LOG="${work}/urls.log"
curl() {
	local out="" fmt="" url="" want_fail=0 arg
	while [[ $# -gt 0 ]]; do
		arg="$1"
		case "${arg}" in
			-o) out="$2"; shift 2; continue ;;
			-w) fmt="$2"; shift 2; continue ;;
			-H) shift 2; continue ;;
			-f* | -s | -S | -sS | -fsS) [[ "${arg}" == *f* ]] && want_fail=1 ;;
			http*) url="${arg}" ;;
		esac
		shift
	done
	printf '%s\n' "${url}" >>"${STUB_URL_LOG}"
	if [[ "${want_fail}" -eq 1 && "${STUB_CODE}" -ge 400 ]]; then
		return 22
	fi
	if [[ -n "${out}" ]]; then
		cat "${STUB_BODY_FILE}" >"${out}"
	fi
	if [[ -n "${fmt}" ]]; then
		printf '%s' "${STUB_CODE}"
	fi
	return 0
}
lib_call() {
	(
		bin_dir="${work}/bin"
		log_dir="${work}/logs"
		work_dir="${work}"
		mkdir -p "${log_dir}"
		GATE_ITERATIONS=1 GATE_RUNS=1 GATE_API_PORT=18097 GATE_API_KEY=k GATE_CORE_PORT=18096
		gate_common_args=()
		log() { :; }
		die() { printf 'die: %s\n' "$*" >&2; exit 1; }
		# shellcheck source=scripts/lib/read-api-latency-core.sh
		. "${repo_root}/scripts/lib/read-api-latency-core.sh"
		"$@"
	)
}
rc_of() { "$@" >/dev/null 2>&1 && echo 0 || echo $?; }

printf '{"count":2,"findings":[{"finding_id":"a"},{"finding_id":"b"}]}' >"${work}/page-ok.json"
printf '{"count":2,"findings":[{"finding_id":"a"},{"finding_id":"c"}]}' >"${work}/page-other.json"
printf '{"count":0,"findings":[]}' >"${work}/page-empty.json"
printf '{"count":0}' >"${work}/page-nofield.json"

# require_findings_byte_identical: identical non-empty pages pass; a differing
# page fails; two identical EMPTY pages must fail too (cmp alone passes them
# vacuously), as must a page with no findings field.
[[ "$(rc_of lib_call require_findings_byte_identical "${work}/page-ok.json" "${work}/page-ok.json")" == 0 ]] || fail "identical non-empty findings pages must pass"
[[ "$(rc_of lib_call require_findings_byte_identical "${work}/page-ok.json" "${work}/page-other.json")" != 0 ]] || fail "differing findings pages must fail"
[[ "$(rc_of lib_call require_findings_byte_identical "${work}/page-empty.json" "${work}/page-empty.json")" != 0 ]] || fail "two identical EMPTY findings pages must fail, not pass vacuously"
[[ "$(rc_of lib_call require_findings_byte_identical "${work}/page-nofield.json" "${work}/page-nofield.json")" != 0 ]] || fail "a findings page with no findings field must fail"

# snapshot_findings_page: asks the findings route with a selector and a limit
# (the route answers 400 without both), writes the body, and dies on a failure.
: >"${STUB_URL_LOG}"
STUB_CODE=200 STUB_BODY_FILE="${work}/page-ok.json" lib_call snapshot_findings_page "${work}/snap.json" >/dev/null 2>&1 || fail "snapshot_findings_page must pass on a 200"
cmp -s "${work}/snap.json" "${work}/page-ok.json" || fail "snapshot_findings_page must write the response body"
rg -qF '/api/v0/supply-chain/impact/findings?ecosystem=npm&limit=50' "${STUB_URL_LOG}" || fail "snapshot_findings_page must send the selector and the limit: $(cat "${STUB_URL_LOG}")"
[[ "$(STUB_CODE=400 STUB_BODY_FILE="${work}/page-ok.json" rc_of lib_call snapshot_findings_page "${work}/snap2.json")" != 0 ]] || fail "snapshot_findings_page must fail on a 400"

# require_grouped_inventory: the status code must match; the 200 positive
# control must also carry buckets, or an empty read model would pass it.
printf '{"count":1,"buckets":[{"key":"svc"}]}' >"${work}/groups-ok.json"
printf '{"count":0,"buckets":[]}' >"${work}/groups-empty.json"
printf '{"error":"unavailable"}' >"${work}/groups-503.json"
[[ "$(STUB_CODE=503 STUB_BODY_FILE="${work}/groups-503.json" rc_of lib_call require_grouped_inventory 503 service_id)" == 0 ]] || fail "a 503 where 503 is wanted must pass"
[[ "$(STUB_CODE=200 STUB_BODY_FILE="${work}/groups-ok.json" rc_of lib_call require_grouped_inventory 503 service_id)" != 0 ]] || fail "a 200 where 503 is wanted must fail"
[[ "$(STUB_CODE=200 STUB_BODY_FILE="${work}/groups-ok.json" rc_of lib_call require_grouped_inventory 200 service_id)" == 0 ]] || fail "a 200 with buckets where 200 is wanted must pass"
[[ "$(STUB_CODE=503 STUB_BODY_FILE="${work}/groups-503.json" rc_of lib_call require_grouped_inventory 200 service_id)" != 0 ]] || fail "a 503 where 200 is wanted must fail"
# A 200 call asks for the comprehensive profile; a 503 call sends none (the
# default precise profile), so it pins the default-profile 503 of a stack whose
# supports backfill has not completed.
: >"${STUB_URL_LOG}"
STUB_CODE=200 STUB_BODY_FILE="${work}/groups-ok.json" lib_call require_grouped_inventory 200 service_id >/dev/null 2>&1
rg -qF 'inventory?group_by=service_id&profile=comprehensive' "${STUB_URL_LOG}" || fail "a 200 grouped call must send profile=comprehensive: $(cat "${STUB_URL_LOG}")"
: >"${STUB_URL_LOG}"
STUB_CODE=503 STUB_BODY_FILE="${work}/groups-503.json" lib_call require_grouped_inventory 503 service_id >/dev/null 2>&1
rg -qF 'inventory?group_by=service_id' "${STUB_URL_LOG}" && ! rg -qF 'profile=' "${STUB_URL_LOG}" || fail "a 503 grouped call must send no profile: $(cat "${STUB_URL_LOG}")"
[[ "$(STUB_CODE=200 STUB_BODY_FILE="${work}/groups-empty.json" rc_of lib_call require_grouped_inventory 200 service_id)" != 0 ]] || fail "a 200 with no buckets must fail the positive control"

# winners_report_path: a leg report never merges into the legacy one.
[[ "$(lib_call winners_report_path "")" == "" ]] || fail "an empty report path stays empty"
[[ "$(lib_call winners_report_path "/t/r.json")" == "/t/r-winners.json" ]] || fail "r.json becomes r-winners.json"
[[ "$(lib_call winners_report_path "/t/r")" == "/t/r-winners" ]] || fail "a bare name gains the suffix"
[[ "$(lib_call winners_report_path "/t/a.json.json")" == "/t/a.json-winners.json" ]] || fail "only the last .json is replaced"

# stop_core_api: a core that ignores SIGTERM must fail the gate instead of
# letting the next leg measure a stale core; a core that exits must clear core_pid.
# The stub signals readiness through a file once its trap is installed, so the
# SIGTERM cannot arrive before it ignores it.
bash -c 'trap "" TERM; : >"$1"; while :; do sleep 1; done' _ "${work}/stubborn.ready" &
stubborn=$!
for _ in $(seq 1 50); do [[ -e "${work}/stubborn.ready" ]] && break; sleep 0.1; done
res="$( ( GATE_CORE_STOP_TIMEOUT=2; bin_dir="${work}/bin"; log_dir="${work}/logs"; work_dir="${work}"; log() { :; }; die() { printf 'die: %s\n' "$*" >&2; exit 1; }
  # shellcheck source=scripts/lib/read-api-latency-core.sh
  . "${repo_root}/scripts/lib/read-api-latency-core.sh"; core_pid="${stubborn}"; stop_core_api ) >/dev/null 2>&1 && echo 0 || echo 1)"
kill -9 "${stubborn}" 2>/dev/null || true
wait "${stubborn}" 2>/dev/null || true
[[ "${res}" == 1 ]] || fail "a core that ignores SIGTERM must make stop_core_api fail"
sleep 30 &
obedient=$!
res="$( ( GATE_CORE_STOP_TIMEOUT=5; bin_dir="${work}/bin"; log_dir="${work}/logs"; work_dir="${work}"; log() { :; }; die() { printf 'die: %s\n' "$*" >&2; exit 1; }
  # shellcheck source=scripts/lib/read-api-latency-core.sh
  . "${repo_root}/scripts/lib/read-api-latency-core.sh"; core_pid="${obedient}"; stop_core_api; [[ -z "${core_pid}" ]] && echo cleared ) 2>/dev/null || true)"
kill -9 "${obedient}" 2>/dev/null || true
wait "${obedient}" 2>/dev/null || true
[[ "${res}" == cleared ]] || fail "stop_core_api must clear core_pid once the core exits"

# start_core_api writes one log per leg, so the winners-leg restart cannot
# truncate the legacy core's log (stubbed start_bg and readyz).
start_bg() { printf '%s' "$1" >"${work}/started-name"; printf -v "$2" '%s' "$$"; }
for leg_case in "false:core-legacy" "true:core-winners"; do
	flag="${leg_case%%:*}"; want="${leg_case##*:}"
	STUB_CODE=200 STUB_BODY_FILE="${work}/page-ok.json" lib_call start_core_api "${flag}" >/dev/null 2>&1 || fail "start_core_api ${flag} failed against the stubs"
	[[ "$(cat "${work}/started-name")" == "${want}" ]] || fail "start_core_api ${flag} must log to ${want}, got: $(cat "${work}/started-name")"
done

printf 'test-read-api-latency-core: PASS\n'
