#!/usr/bin/env bash
#
# read-api-latency-core.sh — eshu-core-api leg helpers for the read-API
# latency gate (scripts/verify-read-api-latency-gate.sh, issue #11170).
# Extracted into a lib chunk so the orchestrator stays under the 500-line cap.
#
# Requires (set by the orchestrator before any of these are called): bin_dir,
# log_dir, work_dir, start_bg (golden-corpus-host-helpers.sh), the die()/log()
# functions, GATE_CORE_PORT, GATE_API_PORT, GATE_API_KEY, GATE_WINNERS_FACT_COUNT,
# and the exported backend env (ESHU_GRAPH_BACKEND, NEO4J_*, ESHU_POSTGRES_DSN,
# ESHU_CONTENT_STORE_DSN, ESHU_API_KEY, ESHU_COMPONENT_HOME,
# ESHU_AUTH_SECRET_ENC_KEY). Bodies resolve these lazily at call time, so
# sourcing this file before those globals are set is safe as long as they
# exist by the time a given helper actually runs.
#
# core_pid is assigned by start_core_api and cleared by stop_core_api. The
# cleanup trap owns final teardown via bg_pids; stop_core_api exists for the
# deliberate legacy -> winners restart between sweep legs.
core_pid=""

# leg_failures collects "<leg> (exit N)" for each sweep leg that failed; see
# run_sweep_leg and fail_if_any_leg_failed.
leg_failures=()

# require_neo4j_backend_for_core fails the gate before any expensive Compose
# work when the backend cannot host eshu-core-api. The core binary fails
# startup on anything but the pinned Neo4j backend (go/cmd/core-api README,
# coreRuntimeConfig), so proceeding on nornicdb would only move the failure
# into a 240s /readyz timeout with a misleading message.
require_neo4j_backend_for_core() {
	[[ "${ESHU_GRAPH_BACKEND}" == "neo4j" ]] || die "eshu-core-api requires ESHU_GRAPH_BACKEND=neo4j (got ${ESHU_GRAPH_BACKEND:-unset}); the satellite cannot serve without a core on any other backend (issue #11170)"
}

# start_core_api <winners> launches the in-tree eshu-core-api on
# GATE_CORE_PORT, waits for its backend-gated /readyz to answer 200, and
# verifies the spawned process is still the one alive. winners is "true" or
# "false" for ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ; the flag is read once at
# mux build, so each leg needs its own process (the orchestrator restarts
# between legs via stop_core_api).
start_core_api() {
	local winners="$1"
	# One log per leg: the winners-leg restart must not truncate the legacy
	# core's log, which is evidence for the legacy sweep.
	local leg="legacy"
	[[ "${winners}" == "true" ]] && leg="winners"
	log "start eshu-core-api (winners-read=${winners})"
	start_bg "core-${leg}" core_pid env "ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ=${winners}" \
		"${bin_dir}/eshu-core-api" --addr="127.0.0.1:${GATE_CORE_PORT}"
	local ready=false
	for _ in $(seq 1 240); do
		if curl -fsS "http://127.0.0.1:${GATE_CORE_PORT}/readyz" >/dev/null 2>&1; then
			ready=true
			break
		fi
		sleep 1
	done
	[[ "${ready}" == "true" ]] || { tail -40 "${log_dir}/core-${leg}.log" >&2 || true; die "eshu-core-api /readyz never returned on port ${GATE_CORE_PORT}"; }
	kill -0 "${core_pid}" 2>/dev/null || die "eshu-core-api (pid ${core_pid}) is not running even though /readyz answered — a different process is likely bound to port ${GATE_CORE_PORT}"
}

# stop_core_api terminates the running core leg and waits for the port to
# release before the next leg binds it. A missing process is not an error
# (the cleanup trap reaps every bg_pid on all exits); a process that refuses
# to die fails loudly instead of letting the next leg measure a stale core.
stop_core_api() {
	[[ -n "${core_pid}" ]] || return 0
	kill "${core_pid}" >/dev/null 2>&1 || { core_pid=""; return 0; }
	local timeout="${GATE_CORE_STOP_TIMEOUT:-30}" deadline
	deadline=$((SECONDS + timeout))
	while kill -0 "${core_pid}" 2>/dev/null; do
		if ((SECONDS >= deadline)); then
			die "eshu-core-api (pid ${core_pid}) did not stop within ${timeout}s; refusing to start the next leg against a stale core"
		fi
		sleep 1
	done
	core_pid=""
}

# snapshot_findings_page <out> fetches the satellite-relayed impact findings
# first page with the gate key. Both legs fetch the same URL so the
# byte-identical proof compares the same serialization. The route answers 400
# without a selector and without limit; every seeded finding is npm, so
# ecosystem=npm selects the whole seeded set and limit=50 bounds the page.
snapshot_findings_page() {
	local out="$1"
	curl -fsS -H "Authorization: Bearer ${GATE_API_KEY}" \
		"http://127.0.0.1:${GATE_API_PORT}/api/v0/supply-chain/impact/findings?ecosystem=npm&limit=50" \
		-o "${out}" || die "findings snapshot failed (see the curl error above)"
}

# findings_page_status <leg> <file> labels one leg's findings snapshot as
# measured (non-empty) or vacuous (empty) for the gate log (issue #11644: the
# legacy leg reads fact_records, which holds no finding facts since the #10880
# PR-C2 write-stop, so its page is vacuous by contract while its sweep
# latencies still measure the legacy path). Labelling is measurement, not
# verdict, so both states exit 0; a snapshot with no findings array is
# unreadable and fails, since the gate cannot label what it cannot parse.
findings_page_status() {
	local leg="$1" file="$2" count
	command -v jq >/dev/null 2>&1 || die "jq is required to label the ${leg} findings page as measured or vacuous"
	count="$(jq -r 'if (.findings | type) == "array" then (.findings | length | tostring) else "unreadable" end' "${file}")" \
		|| die "cannot read the ${leg} findings page ${file}"
	if [[ "${count}" == "unreadable" ]]; then
		die "cannot label the ${leg} findings page ${file}: it holds no findings array"
	elif [[ "${count}" == "0" ]]; then
		log "${leg} findings page: vacuous (0 findings)"
	else
		log "${leg} findings page: measured (${count} findings)"
	fi
}

# snapshot_and_label_findings_page <leg> <out> fetches one leg's findings
# snapshot and labels it measured or vacuous for the gate log (issue #11644:
# the gate output names the post-C2 scope each leg actually covers).
snapshot_and_label_findings_page() {
	snapshot_findings_page "$2"
	findings_page_status "$1" "$2"
}

# require_winners_findings_oracle <winners-page> proves the winners leg served
# the seeded truth from an oracle independent of the read API (issue #11644):
# the gate binary counts supports rows and canonical groups straight from
# supply_chain_impact_finding_supports and checks the relayed snapshot serves
# that depth. The page is labelled measured first, so the gate log names the
# scope even when the proof then fails. A failing proof fails the gate; see
# the reason it prints.
require_winners_findings_oracle() {
	local page="$1"
	findings_page_status winners "${page}"
	"${bin_dir}/eshu-read-api-latency-gate" -verify-winners-oracle \
		-postgres-dsn "${ESHU_POSTGRES_DSN}" \
		-winners-fact-count "${GATE_WINNERS_FACT_COUNT}" \
		-oracle-page "${page}" \
		|| die "winners findings oracle proof failed (see the reason printed above)"
}

# require_grouped_inventory <want_code> <dimension> [profile] asserts the
# grouped inventory dimension answers want_code on the current leg: 503 while
# the winners flag is off (the read model is empty by contract), 200 once the
# winners leg serves the seeded dimensions. A 200 call names its profile
# explicitly: comprehensive for the seeded dimensions, precise for the
# default-profile pin (the API default is precise, so an explicit precise
# names the same path the default takes). A 503 call sends no profile and
# stays the legacy negative control.
require_grouped_inventory() {
	local want_code="$1" dimension="$2" profile="${3:-}" code body="${work_dir}/grouped-${2}.json"
	local query="group_by=${dimension}"
	[[ -n "${profile}" ]] && query="${query}&profile=${profile}"
	code="$(curl -sS -o "${body}" -w '%{http_code}' -H "Authorization: Bearer ${GATE_API_KEY}" \
		"http://127.0.0.1:${GATE_API_PORT}/api/v0/supply-chain/impact/inventory?${query}")" \
		|| die "grouped inventory request failed for dimension ${dimension}"
	[[ "${code}" == "${want_code}" ]] || die "grouped inventory group_by=${dimension} answered ${code}, want ${want_code}"
	# The positive control must prove the read model holds rows, not only that
	# it answered: an empty winners table would otherwise pass it.
	if [[ "${want_code}" == "200" ]]; then
		command -v jq >/dev/null 2>&1 || die "jq is required to check the grouped inventory buckets"
		jq -e '(.buckets | type == "array") and (.buckets | length) > 0' "${body}" >/dev/null 2>&1 \
			|| die "grouped inventory group_by=${dimension} answered 200 with no buckets: the winners table is empty"
	fi
}

# winners_report_path <base> derives the winners-leg report path from a
# legacy-leg one so the two legs never merge into one file: report.json
# becomes report-winners.json, a bare name gains the suffix. Empty stays
# empty (no report asked, no report written).
winners_report_path() {
	local base="$1"
	[[ -n "${base}" ]] || { printf ''; return 0; }
	if [[ "${base}" == *.json ]]; then
		printf '%s-winners.json' "${base%.json}"
	else
		printf '%s-winners' "${base}"
	fi
}

# start_satellite_api <winners> launches the frozen satellite eshu-api with
# ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ=<winners> and waits for its /readyz
# to answer 200. The satellite serves the findings list itself from the read
# model the flag selects at mux build (not via the core), so each leg needs
# its own satellite process, like the core. One log per leg: the winners-leg
# restart must not truncate the legacy satellite's log, which is evidence for
# the legacy sweep. The satellite serves nothing coreless (it exits 1 without
# --core-base-url), so a 240s timeout here with a start log showing the
# missing flag is the RED signature of issue #11170. In status-diagnostic
# mode the API runs under the diagnostic report wrapper and records the
# client report arg, mirroring the previous inline block this replaces. The
# satellite honors only -addr (it ignores the in-tree ESHU_API_ADDR and
# defaults to :8080), so the gate port travels on the flag; without it
# /readyz is probed on a port nothing listens on.
start_satellite_api() {
	local winners="$1"
	# One log per leg: the winners-leg restart must not truncate the legacy
	# satellite's log, which is evidence for the legacy sweep.
	local leg="legacy"
	[[ "${winners}" == "true" ]] && leg="winners"
	log "start eshu-api (winners-read=${winners})"
	if [[ -n "${GATE_STATUS_DIAGNOSTIC_DIR}" ]]; then
		start_bg "api-${leg}" api_pid env ESHU_STATUS_DIAGNOSTIC_REPORT="${GATE_STATUS_DIAGNOSTIC_DIR}/server.json" \
			"ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ=${winners}" "${bin_dir}/eshu-api" \
			-addr=":${GATE_API_PORT}" --core-base-url="http://127.0.0.1:${GATE_CORE_PORT}"
		gate_report_args+=(-diagnostic-client-report "${GATE_STATUS_DIAGNOSTIC_DIR}/client.json")
	else
		start_bg "api-${leg}" api_pid env "ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ=${winners}" "${bin_dir}/eshu-api" \
			-addr=":${GATE_API_PORT}" --core-base-url="http://127.0.0.1:${GATE_CORE_PORT}"
	fi
	if command -v lsof >/dev/null 2>&1; then
		sleep 0.2 # give the process a moment to finish exec() before lsof inspects it
		log "eshu-api binary provenance (lsof -p ${api_pid})"
		lsof -p "${api_pid}" 2>/dev/null | rg 'txt|TEXT' || echo "lsof reported no txt mapping for pid ${api_pid}"
	fi
	wait_for_satellite_readyz "${leg}"
	# A responder on GATE_API_PORT that is not our spawned process (a stale
	# service left on a shared/dev port) would pass the curl check above and
	# then measure something other than this run's eshu-api. Ports are now
	# unique to this gate (see the port comment above), but this is a cheap extra
	# check that the process we started is still the one alive.
	kill -0 "${api_pid}" 2>/dev/null || die "eshu-api (pid ${api_pid}) is not running even though /readyz answered — a different process is likely bound to port ${GATE_API_PORT}"
}

# stop_satellite_api terminates the running satellite leg and waits for the
# port to release before the next leg binds it. A missing process is not an
# error (the cleanup trap reaps every bg_pid on all exits); a process that
# refuses to die fails loudly instead of letting the next leg measure a stale
# satellite.
stop_satellite_api() {
	[[ -n "${api_pid}" ]] || return 0
	kill "${api_pid}" >/dev/null 2>&1 || { api_pid=""; return 0; }
	local timeout="${GATE_API_STOP_TIMEOUT:-30}" deadline
	deadline=$((SECONDS + timeout))
	while kill -0 "${api_pid}" 2>/dev/null; do
		if ((SECONDS >= deadline)); then
			die "eshu-api (pid ${api_pid}) did not stop within ${timeout}s; refusing to start the next leg against a stale satellite"
		fi
		sleep 1
	done
	api_pid=""
}

# wait_for_satellite_readyz [leg] polls the satellite /readyz until it
# answers 200, tailing that leg's satellite log on failure. Split from
# start_satellite_api so each leg's start waits on its own log.
wait_for_satellite_readyz() {
	local leg="${1:-legacy}" ready=false
	for _ in $(seq 1 240); do
		if curl -fsS "http://localhost:${GATE_API_PORT}/readyz" >/dev/null 2>&1; then
			ready=true
			break
		fi
		sleep 1
	done
	[[ "${ready}" == "true" ]] || { tail -40 "${log_dir}/api-${leg}.log" >&2 || true; die "eshu-api /readyz never returned on port ${GATE_API_PORT}"; }
}

# start_mcp_server launches the satellite eshu-mcp HTTP transport against the
# satellite API and waits for its /health. Unchanged from the previous inline
# block except for living here.
start_mcp_server() {
	start_bg mcp-server mcp_pid "${bin_dir}/eshu-mcp-server" \
		-addr="127.0.0.1:${GATE_MCP_PORT}" -core-base-url="http://127.0.0.1:${GATE_API_PORT}"
	local ready=false
	for _ in $(seq 1 240); do
		if curl -fsS "http://127.0.0.1:${GATE_MCP_PORT}/health" >/dev/null 2>&1; then
			ready=true
			break
		fi
		sleep 1
	done
	[[ "${ready}" == "true" ]] || { tail -40 "${log_dir}/mcp-server.log" >&2 || true; die "MCP /health never returned on port ${GATE_MCP_PORT}"; }
	kill -0 "${mcp_pid}" 2>/dev/null || die "MCP server (pid ${mcp_pid}) exited while /health answered"
}

# run_sweep_leg <leg> <budgets> <work_budgets> [<report_arg>...] runs one
# counted sweep leg and fails the gate on any breach. leg names the leg in
# the log and the failure message so a red winners leg cannot be mistaken
# for a red legacy leg.
run_sweep_leg() {
	local leg="$1" budgets="$2" work_budgets="$3"
	shift 3
	# Include the repeat count when more than one counted pass is requested.
	if [[ "${GATE_RUNS}" -eq 1 ]]; then
		log "sweep (${leg}, ${GATE_ITERATIONS} requests/route)"
	else
		log "sweep (${leg}, ${GATE_ITERATIONS} requests/route x ${GATE_RUNS} run(s))"
	fi
	local status=0
	"${bin_dir}/eshu-read-api-latency-gate" "${gate_common_args[@]}" -skip-seed \
		-budgets "${budgets}" \
		-work-budgets "${work_budgets}" \
		"$@" || status=$?
	if [[ "${status}" -ne 0 ]]; then
		# main.go already printed the specific reason (budget breach, coverage
		# floor, or a required route not exercised) to stderr above; this
		# message stays neutral rather than naming one specific cause for every
		# nonzero exit.
		# Record, don't die: the other leg still has to run so one gate run
		# reports both legs; fail_if_any_leg_failed turns the record into the
		# gate's exit at the end.
		printf 'verify-read-api-latency-gate: read-api-latency-gate %s leg failed with exit %s (see the reason printed above)\n' "${leg}" "${status}" >&2
		leg_failures+=("${leg} (exit ${status})")
	fi
}

# fail_if_any_leg_failed fails the gate when any sweep leg recorded a failure.
# Legs run to completion first so a breach in one leg never hides the other
# leg's numbers; the gate is still red when either leg is.
fail_if_any_leg_failed() {
	if [[ "${#leg_failures[@]}" -gt 0 ]]; then
		die "sweep leg(s) failed: ${leg_failures[*]}"
	fi
}
