#!/usr/bin/env bash
# Ifá P3 (#4396) failure-path determinism prove-the-theory-first script (ADR
# step 3a, docs/internal/design/4389-ifa-conformance-platform.md). Mutates the
# demo-org GCP cassette with `ifa mutate-cassette`, drives it through a fresh
# isolated Postgres + NornicDB Compose stack, and asserts which failure path
# fires: a durable fact_work_items dead-letter row (status='dead_letter'), or a
# per-fact metric-only quarantine with no durable row.
#
# This is the empirical half of the prove-the-theory-first gate: the theory
# (go/internal/ifa/mutate.go's MutationKind doc comment) is that only a
# schema-version-major corruption reaches a durable dead-letter row, while a
# missing-required-field corruption is quarantined per fact and never
# dead-letters. -mutation selects which corruption this run injects so both
# can be proven independently.
#
# Ran against a real stack, -mutation schema-major dead-lettered the whole
# PROJECTOR work item (stage=projector, domain=source_local) with
# failure_class="projection_bug" — go/internal/projector/schema_version_admission.go's
# own admission-time gate rejects a core-registered fact kind's unsupported
# major before the reducer's typed-decode seam is ever reached, so the
# reducer's "input_invalid" label never appears for this specific corruption.
# Do not assert a literal failure_class string here; assert status='dead_letter'
# and let ifa.DeadLetterSetsEqual compare the whole row set instead.
#
# Deliberately does NOT reuse cmd/golden-corpus-gate's "-phase=drains" gate:
# that gate's factWorkItemsResidualSQL counts a 'dead_letter' row AS residual
# on purpose (a drained pipeline has no dead letters), so it would never reach
# zero on a deliberately-malformed cassette. This script polls its OWN
# terminal condition instead: status NOT IN ('succeeded','superseded',
# 'dead_letter') = 0 — the failure path's own terminal state.
#
# Usage:
#   scripts/verify-ifa-dead-letter-determinism.sh -mutation schema-major|missing-field [--no-compose] [--keep]

# Refuse to run under bash < 4.4 (or a non-bash shell). This gate relies on bash
# 4.4's fix for expanding an empty array under `set -u` (bash 4.0-4.3 and 3.2
# still raise "unbound variable"). Under a shell without that fix — notably
# macOS's bundled bash 3.2 — a nounset abort during an array expansion is masked
# by the EXIT trap below (whose `local status=$?` reads 0 from a prior success),
# turning a real mid-script crash into a clean exit 0 — a FALSE PASS that
# silently defeats this determinism gate. Fail loudly instead. This check runs
# before `set -euo pipefail` so it can never itself trip nounset; BASH_VERSINFO[0]
# and BASH_VERSINFO[1] are the running bash's major and minor version.
if (( BASH_VERSINFO[0] < 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] < 4) )); then
	printf '%s: requires bash >= 4.4 (running under %s).
' "${0##*/}" "${BASH_VERSION:-non-bash shell}" >&2
	printf '  On bash < 4.4 a nounset abort can be masked by the EXIT trap as a false PASS;
' >&2
	printf '  re-run under bash >= 4.4 (e.g. /opt/homebrew/bin/bash, or run `brew install bash`).
' >&2
	exit 3
fi
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

: "${DEADLETTER_COMPOSE_PROJECT:=eshu-ifa-deadletter-$$}"
# Non-default ports so this can run alongside verify-ifa-replay-drive.sh
# (15532/7788/7575) and verify-golden-corpus-gate.sh (15432/7687/7474)
# without colliding.
export ESHU_POSTGRES_PORT="${ESHU_POSTGRES_PORT:-15635}"
export NEO4J_BOLT_PORT="${NEO4J_BOLT_PORT:-7792}"
export NEO4J_HTTP_PORT="${NEO4J_HTTP_PORT:-7679}"
: "${ESHU_POSTGRES_PASSWORD:=change-me}"
: "${ESHU_NEO4J_PASSWORD:=change-me}"
: "${GATE_DRAIN_TIMEOUT_SECONDS:=180}"

compose_file="docker-compose.yaml"
source_cassette="${repo_root}/testdata/cassettes/gcpcloud/supply-chain-demo.json"

mutation=""
use_compose=1
keep=0
while [[ $# -gt 0 ]]; do
	case "$1" in
		-mutation) mutation="$2"; shift 2 ;;
		--no-compose) use_compose=0; shift ;;
		--keep) keep=1; shift ;;
		-h|--help) sed -n '2,26p' "${BASH_SOURCE[0]}"; exit 0 ;;
		*) echo "verify-ifa-dead-letter-determinism: unknown argument: $1" >&2; exit 2 ;;
	esac
done
case "${mutation}" in
	schema-major|missing-field) ;;
	*) echo "verify-ifa-dead-letter-determinism: -mutation must be schema-major or missing-field (got ${mutation:-<empty>})" >&2; exit 2 ;;
esac

[[ -f "${source_cassette}" ]] || { echo "verify-ifa-dead-letter-determinism: cassette not found: ${source_cassette}" >&2; exit 1; }

work_dir="$(mktemp -d -t ifa-deadletter.XXXXXX)"
bin_dir="${work_dir}/bin"
log_dir="${work_dir}/logs"
mkdir -p "${bin_dir}" "${log_dir}"

bg_pids=()

log() { printf '\n=== %s ===\n' "$*"; }
die() { printf 'verify-ifa-dead-letter-determinism: %s\n' "$*" >&2; exit 1; }

cleanup() {
	local status=$?
	if [[ "${status}" -ne 0 && -d "${log_dir}" ]]; then
		printf '\n=== host binary logs (failure) ===\n' >&2
		for logf in "${log_dir}"/*.log; do
			[[ -f "${logf}" ]] || continue
			printf '\n--- %s ---\n' "$(basename "${logf}")" >&2
			tail -40 "${logf}" >&2 || true
		done
	fi
	for pid in "${bg_pids[@]:-}"; do
		[[ -n "${pid}" ]] && kill "${pid}" >/dev/null 2>&1 || true
	done
	if [[ "${keep}" -eq 1 ]]; then
		printf '\n[--keep] work dir retained: %s\n' "${work_dir}" >&2
	else
		if [[ "${use_compose}" -eq 1 ]]; then
			docker compose -p "${DEADLETTER_COMPOSE_PROJECT}" -f "${compose_file}" down -v >/dev/null 2>&1 || true
		fi
		rm -rf "${work_dir}"
	fi
	exit "${status}"
}
trap cleanup EXIT

export ESHU_GRAPH_BACKEND=nornicdb
export NEO4J_URI="bolt://localhost:${NEO4J_BOLT_PORT}"
export NEO4J_USERNAME=neo4j
export NEO4J_PASSWORD="${ESHU_NEO4J_PASSWORD}"
export NEO4J_DATABASE=nornic
export ESHU_NEO4J_DATABASE=nornic
export DEFAULT_DATABASE=nornic
export ESHU_POSTGRES_DSN="postgresql://eshu:${ESHU_POSTGRES_PASSWORD}@localhost:${ESHU_POSTGRES_PORT}/eshu"
export ESHU_CONTENT_STORE_DSN="${ESHU_POSTGRES_DSN}"
export ESHU_LISTEN_ADDR="127.0.0.1:0"
export ESHU_METRICS_ADDR="127.0.0.1:0"
unset ESHU_PPROF_ADDR || true

build_bin() {
	local cmd="$1"
	CGO_ENABLED=1 go -C go build -o "${bin_dir}/eshu-${cmd}" "./cmd/${cmd}" \
		|| die "build ${cmd} failed"
}

start_bg() {
	local name="$1" pidvar="$2"; shift 2
	"$@" >"${log_dir}/${name}.log" 2>&1 &
	local pid=$!
	bg_pids+=("${pid}")
	printf -v "${pidvar}" '%s' "${pid}"
}

pg() {
	local sql="$1"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${DEADLETTER_COMPOSE_PROJECT}" -f "${compose_file}" exec -T postgres \
			psql -U eshu -d eshu -tA -c "${sql}" 2>/dev/null
	else
		command -v psql >/dev/null 2>&1 || die "psql client required in --no-compose mode"
		psql "${ESHU_POSTGRES_DSN}" -tA -c "${sql}" 2>/dev/null
	fi
}

log "build host binaries"
build_bin bootstrap-data-plane
build_bin ifa
build_bin projector
build_bin reducer

log "mutate demo-org GCP cassette (-mutation ${mutation})"
mutated_cassette="${work_dir}/mutated.json"
case "${mutation}" in
	schema-major)
		"${bin_dir}/eshu-ifa" mutate-cassette \
			-cassette "${source_cassette}" \
			-out "${mutated_cassette}" \
			-fact-kind gcp_cloud_resource \
			-kind schema-major \
			-schema-major 99.0.0 \
			-count 1 \
			| tee "${log_dir}/mutate-cassette.log"
		;;
	missing-field)
		"${bin_dir}/eshu-ifa" mutate-cassette \
			-cassette "${source_cassette}" \
			-out "${mutated_cassette}" \
			-fact-kind gcp_cloud_resource \
			-kind missing-field \
			-field asset_type \
			-count 1 \
			| tee "${log_dir}/mutate-cassette.log"
		;;
esac

if [[ "${use_compose}" -eq 1 ]]; then
	log "start Postgres + NornicDB (project ${DEADLETTER_COMPOSE_PROJECT})"
	docker compose -p "${DEADLETTER_COMPOSE_PROJECT}" -f "${compose_file}" up -d nornicdb postgres

	log "wait for backends"
	backends_ready=false
	for _ in $(seq 1 60); do
		if docker compose -p "${DEADLETTER_COMPOSE_PROJECT}" -f "${compose_file}" exec -T nornicdb \
			wget --spider -q http://localhost:7474/health >/dev/null 2>&1 && \
			docker compose -p "${DEADLETTER_COMPOSE_PROJECT}" -f "${compose_file}" exec -T postgres \
			pg_isready -U eshu -d eshu >/dev/null 2>&1; then
			backends_ready=true
			break
		fi
		sleep 2
	done
	[[ "${backends_ready}" == "true" ]] || die "Postgres + NornicDB did not become ready within budget"
fi

log "apply Postgres + graph schema (eshu-bootstrap-data-plane)"
"${bin_dir}/eshu-bootstrap-data-plane" >"${log_dir}/bootstrap-data-plane.log" 2>&1 \
	|| { tail -40 "${log_dir}/bootstrap-data-plane.log"; die "bootstrap-data-plane failed"; }

log "drive mutated cassette through eshu-ifa drive -workers 1"
if ! "${bin_dir}/eshu-ifa" drive -cassette "${mutated_cassette}" -workers 1 \
	>"${log_dir}/ifa-drive.log" 2>&1; then
	tail -40 "${log_dir}/ifa-drive.log" >&2 || true
	die "eshu-ifa drive failed"
fi
cat "${log_dir}/ifa-drive.log"

work_items_after_drive="$(pg 'SELECT count(*) FROM fact_work_items;' | tr -d '[:space:]')"
[[ -n "${work_items_after_drive}" && "${work_items_after_drive}" -gt 0 ]] \
	|| die "eshu-ifa drive committed but enqueued 0 fact_work_items rows"
printf 'fact_work_items enqueued by the drive: %s\n' "${work_items_after_drive}"

log "drain projector + reducer (background; polling this script's own terminal condition)"
start_bg projector projector_pid "${bin_dir}/eshu-projector"
start_bg reducer reducer_pid "${bin_dir}/eshu-reducer"

# This script's OWN terminal condition, distinct from
# cmd/golden-corpus-gate/drains.go's factWorkItemsResidualSQL: that query
# counts 'dead_letter' AS residual by design (a drained pipeline has none), so
# it would spin until GATE_DRAIN_TIMEOUT_SECONDS on a deliberately-malformed
# cassette. This is the failure path's OWN terminal state: every work item has
# reached a TERMINAL status, whether success or dead-letter.
terminal_condition_sql="SELECT count(*) FROM fact_work_items WHERE status NOT IN ('succeeded', 'superseded', 'dead_letter');"

drained=false
for _ in $(seq 1 "${GATE_DRAIN_TIMEOUT_SECONDS}"); do
	residual="$(pg "${terminal_condition_sql}" | tr -d '[:space:]')"
	if [[ "${residual}" == "0" ]]; then
		drained=true
		break
	fi
	sleep 1
done
[[ "${drained}" == "true" ]] || die "queue did not reach this script's own terminal condition (${terminal_condition_sql}) within ${GATE_DRAIN_TIMEOUT_SECONDS}s"
kill "${projector_pid}" "${reducer_pid}" >/dev/null 2>&1 || true

log "durable dead-letter evidence"
# Count EVERY dead_letter row, not one filtered to a specific failure_class:
# which failure_class label lands depends on which gate rejects the malformed
# fact (the projector's own admission-time schema-version gate versus the
# reducer's typed-decode seam — see go/internal/ifa/mutate.go's MutationKind
# doc comment), so a query pre-filtered to "input_invalid" can read 0 even when
# a real dead-letter row exists under a different label (empirically observed:
# "projection_bug" for a schema-major mutation on gcp_cloud_resource).
dead_letter_total_count="$(pg "SELECT count(*) FROM fact_work_items WHERE status = 'dead_letter';" | tr -d '[:space:]')"
printf 'fact_work_items dead_letter rows (any failure_class): %s\n' "${dead_letter_total_count}"
pg "SELECT work_item_id, stage, domain, failure_class FROM fact_work_items WHERE status = 'dead_letter' ORDER BY work_item_id;"

log "sample dead-letter rows (ifa dead-letters)"
"${bin_dir}/eshu-ifa" dead-letters | tee "${log_dir}/dead-letters.json"

case "${mutation}" in
	schema-major)
		[[ "${dead_letter_total_count}" -ge 1 ]] \
			|| die "PROVEN-FALSE: schema-major mutation produced 0 durable dead-letter rows; expected >= 1"
		log "PROVEN: schema-major mutation reaches a durable fact_work_items dead-letter row (count=${dead_letter_total_count}; see the row dump above for its actual stage/domain/failure_class)"
		;;
	missing-field)
		[[ "${dead_letter_total_count}" -eq 0 ]] \
			|| die "unexpected: missing-field mutation produced ${dead_letter_total_count} durable dead-letter row(s), expected 0 (per-fact quarantine only)"
		if rg -q 'reducer input_invalid fact quarantined' "${log_dir}/reducer.log"; then
			log "PROVEN: missing-field mutation is per-fact QUARANTINED (metric+log), NOT a durable dead-letter (0 dead_letter rows; quarantine log line present)"
		else
			die "missing-field mutation produced 0 dead-letter rows but the expected quarantine log line was not found in reducer.log — cannot confirm the fact was actually processed and quarantined rather than silently skipped upstream"
		fi
		;;
esac

log "PASS (project ${DEADLETTER_COMPOSE_PROJECT}, postgres:${ESHU_POSTGRES_PORT}, neo4j-bolt:${NEO4J_BOLT_PORT})"
