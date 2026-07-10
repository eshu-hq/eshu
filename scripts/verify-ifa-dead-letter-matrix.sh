#!/usr/bin/env bash
# Ifá P3 (#4396) slice 6 failure-path determinism leg. Runs the SAME
# schema-major-mutated demo-org GCP cassette (mutated via `ifa mutate-cassette`,
# exactly as scripts/verify-ifa-dead-letter-determinism.sh's slice-4 proof
# already does) through `eshu-ifa drive -workers N` for N in {1, 2, 4}, each
# against an INDEPENDENT, FRESH Postgres + NornicDB Compose stack (`docker
# compose down -v` between every cell), drains to this script's OWN
# terminal condition (status NOT IN ('succeeded', 'superseded',
# 'dead_letter') = 0 — deliberately NOT cmd/golden-corpus-gate's
# `-phase=drains`, whose factWorkItemsResidualSQL counts a dead_letter row AS
# residual by design and would spin to its own timeout on a deliberately
# malformed cassette), then asserts the durable fact_work_items dead-letter
# SET — (work_item_id, stage, domain, failure_class), ORDER BY work_item_id —
# is byte-identical across all three cells.
#
# work_item_id is content-derived, not a random UUID or auto-increment
# serial: reducerWorkItemID (go/internal/storage/postgres/reducer_queue_
# helpers.go) composes it from scope_id + generation_id + domain +
# entity_key, and projectorWorkItemID (go/internal/storage/postgres/
# projector_queue.go) from scope_id + generation_id. Both scope_id and
# generation_id are themselves derived from the cassette's own content, not
# a clock or a random value. So the SAME malformed cassette produces the SAME
# work_item_id values on every independent fresh-database run regardless of
# N — this script's assertion is that the full dead-letter ROW SET (address
# and content together) is identical across N, not merely that the same
# addresses exist with possibly different content.
#
# This is #4396's failure-path determinism leg, alongside slice 5's
# graph-truth leg (scripts/verify-ifa-determinism.sh) and that same script's
# own --teeth mode (the acceptance clause's non-idempotent-write-is-caught
# proof). Together: clean-path graph determinism green, failure-path
# dead-letter-set determinism green, and a deliberately non-idempotent write
# proven caught. This script deliberately reuses
# scripts/verify-ifa-dead-letter-determinism.sh's own proven mutation and
# terminal-condition SQL (slice 4) rather than reimplementing it, run here
# across N instead of that script's single fixed N=1 — it does not replace
# or duplicate that script, which stays the single-N failure-classification
# proof (schema-major vs. missing-field) this leg does not re-litigate.
#
# Usage:
#   scripts/verify-ifa-dead-letter-matrix.sh [--no-compose] [--keep]
#     --no-compose  assume Postgres + NornicDB are already running on the
#                   configured ports; skip compose up/down here. Because
#                   each cell still needs a FRESH database, --no-compose
#                   pairs only with a caller that resets both backends
#                   between cells itself; this script cannot do that for you.
#     --keep        leave the last cell's work dir (mutated cassette + all
#                   three dead-letter set dumps, for a mismatch diff) in
#                   place on exit.

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

# shellcheck source=scripts/lib/ifa_determinism_common.sh
source "${repo_root}/scripts/lib/ifa_determinism_common.sh"

# ----------------------------------------------------------------------------
# Configuration (override via environment). One Compose project + one port
# triple reused across all three cells, distinct from every sibling
# verify-ifa-*.sh / verify-golden-corpus-gate.sh script's own defaults
# (15432/7687/7474, 15532/7788/7575, 15635/7792/7679, 15636/7793/7680).
# ----------------------------------------------------------------------------
: "${DEADLETTER_MATRIX_COMPOSE_PROJECT:=eshu-ifa-deadletter-matrix-$$}"
export ESHU_POSTGRES_PORT="${ESHU_POSTGRES_PORT:-15637}"
export NEO4J_BOLT_PORT="${NEO4J_BOLT_PORT:-7794}"
export NEO4J_HTTP_PORT="${NEO4J_HTTP_PORT:-7681}"
: "${ESHU_POSTGRES_PASSWORD:=change-me}"
: "${ESHU_NEO4J_PASSWORD:=change-me}"
: "${DEADLETTER_MATRIX_DRAIN_TIMEOUT_SECONDS:=180}"

compose_file="docker-compose.yaml"
source_cassette="${repo_root}/testdata/cassettes/gcpcloud/supply-chain-demo.json"
worker_counts=(1 2 4)

use_compose=1
keep=0
for arg in "$@"; do
	case "${arg}" in
	--no-compose) use_compose=0 ;;
	--keep) keep=1 ;;
	-h | --help)
		sed -n '2,40p' "${BASH_SOURCE[0]}"
		exit 0
		;;
	*)
		echo "verify-ifa-dead-letter-matrix: unknown argument: ${arg}" >&2
		exit 2
		;;
	esac
done

[[ -f "${source_cassette}" ]] || { echo "verify-ifa-dead-letter-matrix: cassette not found: ${source_cassette}" >&2; exit 1; }

work_dir="$(mktemp -d -t ifa-deadletter-matrix.XXXXXX)"
bin_dir="${work_dir}/bin"
log_dir="${work_dir}/logs"
mkdir -p "${bin_dir}" "${log_dir}"

bg_pids=()

log() { printf '\n=== %s ===\n' "$*"; }
die() { printf 'verify-ifa-dead-letter-matrix: %s\n' "$*" >&2; exit 1; }

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
			docker compose -p "${DEADLETTER_MATRIX_COMPOSE_PROJECT}" -f "${compose_file}" down -v >/dev/null 2>&1 || true
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

log "build host binaries"
ifa_det_build_bin "${bin_dir}" bootstrap-data-plane || die "build bootstrap-data-plane failed"
ifa_det_build_bin "${bin_dir}" ifa || die "build ifa failed"
ifa_det_build_bin "${bin_dir}" projector || die "build projector failed"
ifa_det_build_bin "${bin_dir}" reducer || die "build reducer failed"

log "mutate demo-org GCP cassette (-mutation schema-major, once, reused across every cell)"
mutated_cassette="${work_dir}/mutated.json"
"${bin_dir}/eshu-ifa" mutate-cassette \
	-cassette "${source_cassette}" \
	-out "${mutated_cassette}" \
	-fact-kind gcp_cloud_resource \
	-kind schema-major \
	-schema-major 99.0.0 \
	-count 1 \
	| tee "${log_dir}/mutate-cassette.log"

# This script's OWN terminal condition, distinct from
# cmd/golden-corpus-gate's factWorkItemsResidualSQL (which counts a
# dead_letter row AS residual by design, so it would spin to timeout on a
# deliberately malformed cassette) — mirrors
# verify-ifa-dead-letter-determinism.sh's own terminal condition exactly.
terminal_condition_sql="SELECT count(*) FROM fact_work_items WHERE status NOT IN ('succeeded', 'superseded', 'dead_letter');"
dead_letter_set_sql="SELECT work_item_id, stage, domain, failure_class FROM fact_work_items WHERE status = 'dead_letter' ORDER BY work_item_id;"

declare -A dead_letter_sets
declare -A dead_letter_counts

for n in "${worker_counts[@]}"; do
	log "cell N=${n}: fresh stack"

	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose -p "${DEADLETTER_MATRIX_COMPOSE_PROJECT}" -f "${compose_file}" up -d nornicdb postgres
		log "N=${n}: wait for backends"
		ifa_det_wait_for_backends "${DEADLETTER_MATRIX_COMPOSE_PROJECT}" "${compose_file}" \
			|| die "N=${n}: Postgres + NornicDB did not become ready within budget"
	fi

	log "N=${n}: apply Postgres + graph schema (eshu-bootstrap-data-plane)"
	"${bin_dir}/eshu-bootstrap-data-plane" >"${log_dir}/bootstrap-data-plane-n${n}.log" 2>&1 \
		|| { tail -40 "${log_dir}/bootstrap-data-plane-n${n}.log"; die "N=${n}: bootstrap-data-plane failed"; }

	log "N=${n}: drive mutated cassette through eshu-ifa drive -workers ${n}"
	if ! "${bin_dir}/eshu-ifa" drive -cassette "${mutated_cassette}" -workers "${n}" \
		>"${log_dir}/ifa-drive-n${n}.log" 2>&1; then
		tail -40 "${log_dir}/ifa-drive-n${n}.log" >&2 || true
		die "N=${n}: eshu-ifa drive failed"
	fi
	cat "${log_dir}/ifa-drive-n${n}.log"

	# Same vacuous-drain guard verify-ifa-determinism.sh uses: a residual=0
	# reading over a queue nothing was ever put in would be a vacuous proof.
	work_items="$(ifa_det_pg "${DEADLETTER_MATRIX_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		'SELECT count(*) FROM fact_work_items;' "${compose_file}" | tr -d '[:space:]')"
	[[ -n "${work_items}" && "${work_items}" -gt 0 ]] \
		|| die "N=${n}: eshu-ifa drive committed but enqueued 0 fact_work_items rows (vacuous drain proof)"
	printf 'N=%s fact_work_items enqueued: %s\n' "${n}" "${work_items}"

	log "N=${n}: drain projector + reducer (polling this script's own terminal condition)"
	bg_pids=()
	ifa_det_start_bg "${log_dir}" "projector-n${n}" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-n${n}" reducer_pid "${bin_dir}/eshu-reducer"

	drained=false
	for _ in $(seq 1 "${DEADLETTER_MATRIX_DRAIN_TIMEOUT_SECONDS}"); do
		residual="$(ifa_det_pg "${DEADLETTER_MATRIX_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
			"${terminal_condition_sql}" "${compose_file}" | tr -d '[:space:]')"
		if [[ "${residual}" == "0" ]]; then
			drained=true
			break
		fi
		sleep 1
	done
	[[ "${drained}" == "true" ]] \
		|| die "N=${n}: queue did not reach this script's own terminal condition (${terminal_condition_sql}) within ${DEADLETTER_MATRIX_DRAIN_TIMEOUT_SECONDS}s"
	kill "${projector_pid}" "${reducer_pid}" >/dev/null 2>&1 || true

	log "N=${n}: durable dead-letter set"
	dead_letter_set="$(ifa_det_pg "${DEADLETTER_MATRIX_COMPOSE_PROJECT}" "${use_compose}" "${ESHU_POSTGRES_DSN}" \
		"${dead_letter_set_sql}" "${compose_file}")"
	dead_letter_sets[${n}]="${dead_letter_set}"
	if [[ -z "${dead_letter_set}" ]]; then
		dead_letter_counts[${n}]=0
	else
		dead_letter_counts[${n}]="$(printf '%s\n' "${dead_letter_set}" | wc -l | tr -d '[:space:]')"
	fi
	printf 'N=%s dead_letter rows (%s):\n%s\n' "${n}" "${dead_letter_counts[${n}]}" "${dead_letter_set}"

	[[ "${dead_letter_counts[${n}]}" -ge 1 ]] \
		|| die "N=${n}: schema-major mutation produced 0 durable dead-letter rows; expected >= 1 (see verify-ifa-dead-letter-determinism.sh's slice-4 proof)"

	if [[ "${use_compose}" -eq 1 ]]; then
		log "N=${n}: tear down cell (fresh stack for the next cell)"
		docker compose -p "${DEADLETTER_MATRIX_COMPOSE_PROJECT}" -f "${compose_file}" down -v >/dev/null 2>&1 || true
	fi
done

log "compare dead-letter sets across N=${worker_counts[*]}"
first_n="${worker_counts[0]}"
mismatch=0
for n in "${worker_counts[@]}"; do
	[[ "${n}" == "${first_n}" ]] && continue
	if [[ "${dead_letter_sets[${n}]}" != "${dead_letter_sets[${first_n}]}" ]]; then
		mismatch=1
		printf 'MISMATCH: N=%s dead-letter set != N=%s dead-letter set\n' "${n}" "${first_n}" >&2
		printf '\n=== dead-letter set diff: N=%s vs N=%s (failure artifact) ===\n' "${first_n}" "${n}" >&2
		diff -u <(printf '%s\n' "${dead_letter_sets[${first_n}]}") <(printf '%s\n' "${dead_letter_sets[${n}]}") >&2 || true
	fi
done

[[ "${mismatch}" -eq 0 ]] || die "dead-letter-set determinism FAILED: the durable fact_work_items dead-letter set (work_item_id, stage, domain, failure_class) diverged across worker counts (see the diff above) — this is a real concurrency defect in the failure path; do NOT lower N, retry, or otherwise normalize this away"

log "PASS: dead-letter-set determinism green across N=${worker_counts[*]} (project ${DEADLETTER_MATRIX_COMPOSE_PROJECT}, postgres:${ESHU_POSTGRES_PORT}, neo4j-bolt:${NEO4J_BOLT_PORT})"
for n in "${worker_counts[@]}"; do
	printf '  N=%s dead_letter_rows=%s\n' "${n}" "${dead_letter_counts[${n}]}"
done
