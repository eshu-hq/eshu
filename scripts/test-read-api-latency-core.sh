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
printf '{"count":0,"findings":[]}' >"${work}/page-empty.json"
printf '{"count":0}' >"${work}/page-nofield.json"

# findings_page_status: labels one leg's snapshot measured (non-empty) or
# vacuous (empty) for the gate log (#11644); labelling is measurement, never
# verdict, so both states exit 0, while a snapshot with no findings array is
# unreadable and fails. The label text is asserted through a printing log
# (lib_call's log is a no-op, so it cannot observe the wording).
status_log() {
	( bin_dir="${work}/bin"; log_dir="${work}/logs"; work_dir="${work}";
		GATE_API_PORT=18097 GATE_API_KEY=k
		log() { printf 'log: %s\n' "$*"; }; die() { printf 'die: %s\n' "$*" >&2; exit 1; }
		# shellcheck source=scripts/lib/read-api-latency-core.sh
		. "${repo_root}/scripts/lib/read-api-latency-core.sh"; "$@" ) 2>&1
}
[[ "$(status_log findings_page_status legacy "${work}/page-empty.json")" == *"vacuous"* ]] || fail "an empty legacy page must be labelled vacuous"
[[ "$(rc_of lib_call findings_page_status legacy "${work}/page-empty.json")" == 0 ]] || fail "labelling a vacuous page must not fail the gate"
[[ "$(status_log findings_page_status winners "${work}/page-ok.json")" == *"measured"* ]] || fail "a non-empty winners page must be labelled measured"
[[ "$(rc_of lib_call findings_page_status winners "${work}/page-ok.json")" == 0 ]] || fail "labelling a measured page must not fail the gate"
[[ "$(rc_of lib_call findings_page_status legacy "${work}/page-nofield.json")" != 0 ]] || fail "a page with no findings field must fail labelling, not pass as vacuous"

# require_winners_findings_oracle: proves the winners snapshot against the
# supports oracle through the gate binary (#11644 post-C2 truth proof), and
# fails the gate when the proof fails.
cat >"${work}/bin/eshu-read-api-latency-gate" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$@" >>"${GATE_STUB_ARGS}"
exit "${GATE_STUB_EXIT:-0}"
STUB
chmod +x "${work}/bin/eshu-read-api-latency-gate"
: >"${work}/oracle-args.log"
printf '{"count":1,"findings":[{"finding_id":"a"}],"limit":50,"truncated":true}' >"${work}/winners-snap.json"
export GATE_STUB_ARGS="${work}/oracle-args.log" GATE_STUB_EXIT=0
export ESHU_POSTGRES_DSN="postgresql://eshu:change-me@localhost:15537/eshu" GATE_WINNERS_FACT_COUNT=20000
lib_call require_winners_findings_oracle "${work}/winners-snap.json" >/dev/null 2>&1 || fail "a passing oracle proof must pass the gate"
rg -qF -- '-verify-winners-oracle' "${work}/oracle-args.log" || fail "the oracle proof must run the gate binary in verify mode: $(cat "${work}/oracle-args.log")"
rg -qF -- "${work}/winners-snap.json" "${work}/oracle-args.log" || fail "the oracle proof must name the winners snapshot: $(cat "${work}/oracle-args.log")"
rg -qF -- 'postgresql://eshu:change-me@localhost:15537/eshu' "${work}/oracle-args.log" || fail "the oracle proof must use the gate Postgres DSN: $(cat "${work}/oracle-args.log")"
export GATE_STUB_EXIT=1
[[ "$(rc_of lib_call require_winners_findings_oracle "${work}/winners-snap.json")" != 0 ]] || fail "a failing oracle proof must fail the gate"

# snapshot_and_label_findings_page fetches the snapshot and labels it in one
# call, so the orchestrator names each leg's scope without extra lines.
: >"${STUB_URL_LOG}"
STUB_CODE=200 STUB_BODY_FILE="${work}/page-ok.json" status_log snapshot_and_label_findings_page winners "${work}/labelled.json" >/dev/null 2>&1 || fail "snapshot-and-label must pass on a 200 with findings"
cmp -s "${work}/labelled.json" "${work}/page-ok.json" || fail "snapshot-and-label must write the response body"
STUB_CODE=200 STUB_BODY_FILE="${work}/page-empty.json"
[[ "$(status_log snapshot_and_label_findings_page legacy "${work}/labelled-empty.json")" == *"vacuous"* ]] || fail "snapshot-and-label must label an empty legacy page vacuous"

# The oracle proof labels the winners page measured before proving it, so the
# gate log names the scope even when the proof then fails.
export GATE_STUB_EXIT=0
[[ "$(status_log require_winners_findings_oracle "${work}/page-ok.json")" == *"measured"* ]] || fail "the oracle proof must label the winners page measured first"

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
[[ "$(STUB_CODE=200 STUB_BODY_FILE="${work}/groups-ok.json" rc_of lib_call require_grouped_inventory 200 service_id comprehensive)" == 0 ]] || fail "a 200 with buckets where 200 is wanted must pass"
[[ "$(STUB_CODE=503 STUB_BODY_FILE="${work}/groups-503.json" rc_of lib_call require_grouped_inventory 200 service_id comprehensive)" != 0 ]] || fail "a 503 where 200 is wanted must fail"
# A 200 call names its profile explicitly; a 503 call sends none (the
# default precise profile) and stays the legacy negative control.
: >"${STUB_URL_LOG}"
STUB_CODE=200 STUB_BODY_FILE="${work}/groups-ok.json" lib_call require_grouped_inventory 200 service_id comprehensive >/dev/null 2>&1
rg -qF 'inventory?group_by=service_id&profile=comprehensive' "${STUB_URL_LOG}" || fail "a 200 grouped call must send profile=comprehensive: $(cat "${STUB_URL_LOG}")"
: >"${STUB_URL_LOG}"
STUB_CODE=503 STUB_BODY_FILE="${work}/groups-503.json" lib_call require_grouped_inventory 503 service_id >/dev/null 2>&1
rg -qF 'inventory?group_by=service_id' "${STUB_URL_LOG}" && ! rg -qF 'profile=' "${STUB_URL_LOG}" || fail "a 503 grouped call must send no profile: $(cat "${STUB_URL_LOG}")"
[[ "$(STUB_CODE=200 STUB_BODY_FILE="${work}/groups-empty.json" rc_of lib_call require_grouped_inventory 200 service_id comprehensive)" != 0 ]] || fail "a 200 with no buckets must fail the positive control"

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

# start_satellite_api: the winners flag travels into the satellite env on both
# legs, and each leg logs to its own file so the winners-leg restart cannot
# truncate the legacy satellite's log (recording start_bg stub, stubbed readyz).
GATE_STATUS_DIAGNOSTIC_DIR=""
start_bg() { printf '%s\n' "$@" >"${work}/sat-startbg-args"; printf -v "$2" '%s' "$$"; }
for leg_case in "false:api-legacy" "true:api-winners"; do
	flag="${leg_case%%:*}"; want="${leg_case##*:}"
	STUB_CODE=200 STUB_BODY_FILE="${work}/page-ok.json" lib_call start_satellite_api "${flag}" >/dev/null 2>&1 || fail "start_satellite_api ${flag} failed against the stubs"
	rg -qF "ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ=${flag}" "${work}/sat-startbg-args" || fail "start_satellite_api ${flag} must pass the winners flag into the satellite env: $(cat "${work}/sat-startbg-args")"
	[[ "$(head -n1 "${work}/sat-startbg-args")" == "${want}" ]] || fail "start_satellite_api ${flag} must log to ${want}, got: $(head -n1 "${work}/sat-startbg-args")"
done
# The diagnostic branch carries the report env AND the winners flag.
gate_report_args=()
GATE_STATUS_DIAGNOSTIC_DIR="${work}/diag"
STUB_CODE=200 STUB_BODY_FILE="${work}/page-ok.json" lib_call start_satellite_api true >/dev/null 2>&1 || fail "start_satellite_api in diagnostic mode failed against the stubs"
rg -qF "ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ=true" "${work}/sat-startbg-args" || fail "diagnostic start_satellite_api must still pass the winners flag: $(cat "${work}/sat-startbg-args")"
rg -qF "ESHU_STATUS_DIAGNOSTIC_REPORT=${work}/diag/server.json" "${work}/sat-startbg-args" || fail "diagnostic start_satellite_api must keep the report env: $(cat "${work}/sat-startbg-args")"
GATE_STATUS_DIAGNOSTIC_DIR=""

# stop_satellite_api: a satellite that ignores SIGTERM must fail the gate
# instead of letting the next leg measure a stale satellite; one that exits
# must clear api_pid (mirrors the stop_core_api contract).
bash -c 'trap "" TERM; : >"$1"; while :; do sleep 1; done' _ "${work}/sat-stubborn.ready" &
sat_stubborn=$!
for _ in $(seq 1 50); do [[ -e "${work}/sat-stubborn.ready" ]] && break; sleep 0.1; done
res="$( ( GATE_API_STOP_TIMEOUT=2; bin_dir="${work}/bin"; log_dir="${work}/logs"; work_dir="${work}"; log() { :; }; die() { printf 'die: %s\n' "$*" >&2; exit 1; }
  # shellcheck source=scripts/lib/read-api-latency-core.sh
  . "${repo_root}/scripts/lib/read-api-latency-core.sh"; api_pid="${sat_stubborn}"; stop_satellite_api ) >/dev/null 2>&1 && echo 0 || echo 1)"
kill -9 "${sat_stubborn}" 2>/dev/null || true
wait "${sat_stubborn}" 2>/dev/null || true
[[ "${res}" == 1 ]] || fail "a satellite that ignores SIGTERM must make stop_satellite_api fail"
sleep 30 &
sat_obedient=$!
res="$( ( GATE_API_STOP_TIMEOUT=5; bin_dir="${work}/bin"; log_dir="${work}/logs"; work_dir="${work}"; log() { :; }; die() { printf 'die: %s\n' "$*" >&2; exit 1; }
  # shellcheck source=scripts/lib/read-api-latency-core.sh
  . "${repo_root}/scripts/lib/read-api-latency-core.sh"; api_pid="${sat_obedient}"; stop_satellite_api; [[ -z "${api_pid}" ]] && echo cleared ) 2>/dev/null || true)"
kill -9 "${sat_obedient}" 2>/dev/null || true
wait "${sat_obedient}" 2>/dev/null || true
[[ "${res}" == cleared ]] || fail "stop_satellite_api must clear api_pid once the satellite exits"

# require_grouped_inventory with an explicit profile: comprehensive 200s prove
# the rebuilt winners table, the precise (default-profile) 200 proves the
# backfilled supports path, and profile-less 503s stay the legacy negative
# control.
: >"${STUB_URL_LOG}"
STUB_CODE=200 STUB_BODY_FILE="${work}/groups-ok.json" lib_call require_grouped_inventory 200 service_id comprehensive >/dev/null 2>&1 || fail "a comprehensive 200 with buckets must pass"
rg -qF 'inventory?group_by=service_id&profile=comprehensive' "${STUB_URL_LOG}" || fail "a comprehensive grouped call must send profile=comprehensive: $(cat "${STUB_URL_LOG}")"
: >"${STUB_URL_LOG}"
STUB_CODE=200 STUB_BODY_FILE="${work}/groups-ok.json" lib_call require_grouped_inventory 200 service_id precise >/dev/null 2>&1 || fail "a precise 200 with buckets must pass"
rg -qF 'inventory?group_by=service_id&profile=precise' "${STUB_URL_LOG}" || fail "a precise grouped call must send profile=precise: $(cat "${STUB_URL_LOG}")"
[[ "$(STUB_CODE=200 STUB_BODY_FILE="${work}/groups-empty.json" rc_of lib_call require_grouped_inventory 200 service_id precise)" != 0 ]] || fail "a precise 200 with no buckets must fail the positive control"

printf 'test-read-api-latency-core: PASS\n'
