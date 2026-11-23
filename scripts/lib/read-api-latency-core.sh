#!/usr/bin/env bash
#
# read-api-latency-core.sh — eshu-core-api leg helpers for the read-API
# latency gate (scripts/verify-read-api-latency-gate.sh, issue #11170).
# Extracted into a lib chunk so the orchestrator stays under the 500-line cap.
#
# Requires (set by the orchestrator before any of these are called): bin_dir,
# log_dir, work_dir, start_bg (golden-corpus-host-helpers.sh), the die()/log()
# functions, GATE_CORE_PORT, GATE_API_PORT, GATE_API_KEY, and the exported
# backend env (ESHU_GRAPH_BACKEND, NEO4J_*, ESHU_POSTGRES_DSN,
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

# require_findings_byte_identical <legacy> <winners> fails unless the two
# legs served byte-identical findings pages: the winners read model promises
# the same output as the legacy read-time dedup, and any drift means the
# legs measured different truth.
require_findings_byte_identical() {
	local legacy="$1" winners="$2"
	# Two identical empty pages pass cmp vacuously, so the proof is only worth
	# anything when the page it compares holds findings.
	require_findings_nonempty "${legacy}"
	if cmp -s "${legacy}" "${winners}"; then
		log "findings pages byte-identical across legacy/winners legs"
		return 0
	fi
	diff <(head -c 2000 "${legacy}") <(head -c 2000 "${winners}") >&2 || true
	die "legacy and winners findings pages differ (first 2kB diffed above); the legs did not serve the same truth"
}

# require_findings_nonempty <file> fails unless the findings page holds at least
# one finding (the envelope carries them in .findings), so a byte-identical
# comparison cannot pass on two empty pages.
require_findings_nonempty() {
	command -v jq >/dev/null 2>&1 || die "jq is required to check that the findings page is not empty"
	jq -e '(.findings | type == "array") and (.findings | length) > 0' "$1" >/dev/null 2>&1 \
		|| die "findings page $1 holds no findings; a byte-identical comparison of empty pages proves nothing"
}

# require_grouped_inventory <want_code> <dimension> asserts the grouped
# inventory dimension answers want_code on the current leg: 503 while the
# winners flag is off (the read model is empty by contract), 200 once the
# winners leg serves the seeded dimensions. A 200 call asks for
# profile=comprehensive. A 503 call sends no profile (the API default is precise),
# so on the winners leg it pins the default-profile 503 that a stack answers while
# its supports backfill has not completed (the supports read model is not ready).
# That pin goes red once the backfill completes; it is not a PR-B2 pin anymore.
require_grouped_inventory() {
	local want_code="$1" dimension="$2" code body="${work_dir}/grouped-${2}.json"
	local query="group_by=${dimension}"
	[[ "${want_code}" == "200" ]] && query="${query}&profile=comprehensive"
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

# start_satellite_api launches the frozen satellite eshu-api against the
# running core and waits for its /readyz to answer 200. The satellite serves
# nothing coreless (it exits 1 without --core-base-url), so a 240s timeout
# here with a start log showing the missing flag is the RED signature of
# issue #11170. In status-diagnostic mode the API runs under the diagnostic
# report wrapper and records the client report arg, mirroring the previous
# inline block this replaces. The satellite honors only -addr (it ignores the
# in-tree ESHU_API_ADDR and defaults to :8080), so the gate port travels on the
# flag; without it /readyz is probed on a port nothing listens on.
start_satellite_api() {
	if [[ -n "${GATE_STATUS_DIAGNOSTIC_DIR}" ]]; then
		start_bg api api_pid env ESHU_STATUS_DIAGNOSTIC_REPORT="${GATE_STATUS_DIAGNOSTIC_DIR}/server.json" "${bin_dir}/eshu-api" \
			-addr=":${GATE_API_PORT}" --core-base-url="http://127.0.0.1:${GATE_CORE_PORT}"
		gate_report_args+=(-diagnostic-client-report "${GATE_STATUS_DIAGNOSTIC_DIR}/client.json")
	else
		start_bg api api_pid "${bin_dir}/eshu-api" \
			-addr=":${GATE_API_PORT}" --core-base-url="http://127.0.0.1:${GATE_CORE_PORT}"
	fi
	if command -v lsof >/dev/null 2>&1; then
		sleep 0.2 # give the process a moment to finish exec() before lsof inspects it
		log "eshu-api binary provenance (lsof -p ${api_pid})"
		lsof -p "${api_pid}" 2>/dev/null | rg 'txt|TEXT' || echo "lsof reported no txt mapping for pid ${api_pid}"
	fi
	wait_for_satellite_readyz
	# A responder on GATE_API_PORT that is not our spawned process (a stale
	# service left on a shared/dev port) would pass the curl check above and
	# then measure something other than this run's eshu-api. Ports are now
	# unique to this gate (see the port comment above), but this is a cheap extra
	# check that the process we started is still the one alive.
	kill -0 "${api_pid}" 2>/dev/null || die "eshu-api (pid ${api_pid}) is not running even though /readyz answered — a different process is likely bound to port ${GATE_API_PORT}"
}

# wait_for_satellite_readyz polls the satellite /readyz until it answers
# 200. Split from start_satellite_api so the winners-leg core restart can
# re-verify the same satellite against the new core before the second sweep
# measures it.
wait_for_satellite_readyz() {
	local ready=false
	for _ in $(seq 1 240); do
		if curl -fsS "http://localhost:${GATE_API_PORT}/readyz" >/dev/null 2>&1; then
			ready=true
			break
		fi
		sleep 1
	done
	[[ "${ready}" == "true" ]] || { tail -40 "${log_dir}/api.log" >&2 || true; die "eshu-api /readyz never returned on port ${GATE_API_PORT}"; }
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
