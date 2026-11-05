#!/usr/bin/env bash
# Token high-water handover for the claim-plane cutover (#9376, epic #8875).
#
# This script strings the EXISTING handover primitives together in the one
# safe order from the arbiter ruling on #9376 (seeded token-allocator
# handover) and refuses to move on when a precondition does not hold:
#
#   1. freeze    scale the in-tree coordinator to 0 so ClaimNext stops
#   2. drain     wait until no live unexpired claim or claimed item remains
#   3. snapshot  write items.tsv + fences.tsv + sha256sums into a new dir
#   4. seed      advance satellite allocator tokens to max(snapshot, live)
#   5. verify    re-run the seed file (idempotent no-op) and require every
#                snapshot item at or above its H with none missing
#   6. flip      NOT HERE: the flip is a reviewed deploy (authority switch),
#                taken only on a verified snapshot
#   abort        restore coordinator replicas, record the abort; pre-flip
#                satellite seeds are inert while the satellite stays dark
#
# It adds no new core command. A post-flip rollback must re-seed the
# re-activated authority to the max token the satellite issued; that is the
# cutover-rollback counterpart, not this script.
#
# Subcommands (all read-only except where noted):
#   plan [--snapshot DIR]  print the steps with exact commands; runs nothing
#   freeze                 scale coordinator to 0 (mutates deployment)
#   drain                  one drain check; exit 0 only when fully drained
#   snapshot --snapshot DIR
#                          freeze-check, drain-check, write DIR (mutates DIR)
#   seed --snapshot DIR --yes
#                          advance satellite tokens from DIR (mutates satellite)
#   verify --snapshot DIR  idempotent re-seed + full comparison (mutates
#                          nothing when already seeded: GREATEST is a no-op)
#   abort --snapshot DIR   restore replicas, record abort (mutates deployment)
#   run --snapshot DIR --yes
#                          freeze, drain (to deadline), snapshot, seed, verify
#
# Dependencies are injected through the environment so tests can stub them:
#   HANDOVER_KUBECTL_BIN (kubectl)  HANDOVER_PSQL_BIN (psql)
#   HANDOVER_SHA_BIN (sha256sum)    HANDOVER_SLEEP_BIN (sleep)
# Core psql reads its connection from the standard libpq environment (PGHOST,
# PGUSER, PGDATABASE, PGPASSFILE, ...). The satellite connection uses explicit
# flags from HANDOVER_SAT_PGHOST/HANDOVER_SAT_PGPORT/HANDOVER_SAT_PGUSER/
# HANDOVER_SAT_PGDATABASE; its password comes from PGPASSFILE only, which
# must be set for seed/verify/run. No secret is ever passed on a command
# line or printed. Hung calls are bounded: PGCONNECT_TIMEOUT (default 10s)
# for each database connection, HANDOVER_KUBECTL_TIMEOUT (default 30s) for
# each kubectl request, HANDOVER_FREEZE_SECONDS (default 300) for the
# scale-down wait, HANDOVER_DRAIN_SECONDS (default 600) for the drain
# wait in run mode. The freeze record defaults to /tmp/handover-replicas:
# set HANDOVER_STATE_FILE to keep it beside the snapshot dir instead, since
# abort trusts whatever well-formed record it finds there.
#
# A snapshot dir and its sidecars hold stable work-item IDs, so every file
# this script writes stays private to the operator.
umask 077

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
lib_dir="${script_dir}/lib"
sql_items="${lib_dir}/claim-plane-handover-snapshot-items.sql"
sql_fences="${lib_dir}/claim-plane-handover-snapshot-fences.sql"
sql_drain="${lib_dir}/claim-plane-handover-drain.sql"
sql_seed="${lib_dir}/claim-plane-handover-seed.sql"

KUBECTL="${HANDOVER_KUBECTL_BIN:-kubectl}"
PSQL="${HANDOVER_PSQL_BIN:-psql}"
SHA="${HANDOVER_SHA_BIN:-sha256sum}"
SLEEP_BIN="${HANDOVER_SLEEP_BIN:-sleep}"
KUBECTL_TIMEOUT="${HANDOVER_KUBECTL_TIMEOUT:-30s}"
DRAIN_SECONDS="${HANDOVER_DRAIN_SECONDS:-600}"
FREEZE_SECONDS="${HANDOVER_FREEZE_SECONDS:-300}"
NAMESPACE="${HANDOVER_NAMESPACE:-}"
DEPLOY="${HANDOVER_COORDINATOR_DEPLOY:-workflow-coordinator}"
SAT_HOST="${HANDOVER_SAT_PGHOST:-}"
SAT_PORT="${HANDOVER_SAT_PGPORT:-5432}"
SAT_USER="${HANDOVER_SAT_PGUSER:-}"
SAT_DB="${HANDOVER_SAT_PGDATABASE:-}"
export PGCONNECT_TIMEOUT="${PGCONNECT_TIMEOUT:-10}"

TMP_FILES=()
mk_tmp() { # varname
	local file
	file="$(mktemp "${TMPDIR:-/tmp}/handover.XXXXXX")"
	TMP_FILES+=("${file}")
	printf -v "$1" '%s' "${file}"
}
cleanup() { rm -f "${TMP_FILES[@]:-}"; }
trap cleanup EXIT

fail() { printf 'claim-plane-cutover-handover: %s\n' "$*" >&2; exit 1; }
need_bin() { command -v "$1" >/dev/null 2>&1 || fail "need $2 (set $3 to override)"; }

# psql_core runs one SQL file against CORE with deterministic output flags.
psql_core() { # file
	"${PSQL}" -X -q -v ON_ERROR_STOP=1 -A -t -F "$(printf '\t')" -f "$1"
}

# psql_sat runs argv against SATELLITE with explicit connection flags.
psql_sat() { # args...
	"${PSQL}" -X -q -v ON_ERROR_STOP=1 \
		-h "${SAT_HOST}" -p "${SAT_PORT}" -U "${SAT_USER}" -d "${SAT_DB}" \
		"$@"
}

require_sat() {
	[[ -n "${SAT_HOST}" && -n "${SAT_USER}" && -n "${SAT_DB}" ]] ||
		fail "satellite connection unset (HANDOVER_SAT_PGHOST/_PGUSER/_PGDATABASE)"
	[[ -n "${PGPASSFILE:-}" ]] ||
		fail "PGPASSFILE must be set: satellite password never travels on a command line"
}

require_namespace() {
	[[ -n "${NAMESPACE}" ]] || fail "HANDOVER_NAMESPACE is required for deployment steps"
}

# kubectl_get_counts prints "spec status" replica counts. A Deployment at
# zero replicas has no .status.replicas field, so status may be empty: callers
# must treat empty status as 0, never as an error.
kubectl_get_counts() {
	require_namespace
	"${KUBECTL}" --request-timeout="${KUBECTL_TIMEOUT}" -n "${NAMESPACE}" \
		get deployment "${DEPLOY}" -o 'jsonpath={.spec.replicas}{"|"}{.status.replicas}'
}

# frozen_gate succeeds only when no coordinator pod can still ClaimNext.
# It returns 1 (never exits) so wait loops can poll through it.
frozen_gate() {
	local counts spec status
	counts="$(kubectl_get_counts)" || return 1
	spec="${counts%%|*}"
	status="${counts#*|}"
	[[ "${spec}" =~ ^[0-9]+$ ]] || return 1
	[[ "${spec}" == "0" && ( -z "${status}" || "${status}" == "0" ) ]]
}

# wait_frozen polls until the scale-down completes.
wait_frozen() {
	local deadline=$((SECONDS + FREEZE_SECONDS))
	while ! frozen_gate; do
		[[ "${SECONDS}" -ge "${deadline}" ]] &&
			fail "freeze deadline (${FREEZE_SECONDS}s) with coordinator pods still present"
		"${SLEEP_BIN}" 5
	done
}

# cmd_freeze stops ClaimNext by scaling the coordinator to zero, waits for
# the pods to terminate, and records the prior spec count for abort.
cmd_freeze() {
	need_bin "${KUBECTL}" kubectl HANDOVER_KUBECTL_BIN
	require_namespace
	[[ $# -eq 0 ]] || fail "freeze takes no arguments"
	local counts prior
	counts="$(kubectl_get_counts)"
	prior="${counts%%|*}"
	[[ "${prior}" =~ ^[0-9]+$ ]] || fail "unreadable spec replicas: ${counts}"
	[[ "${prior}" -gt 0 ]] || fail "already frozen (spec 0); snapshot or abort"
	"${KUBECTL}" --request-timeout="${KUBECTL_TIMEOUT}" -n "${NAMESPACE}" \
		scale deployment "${DEPLOY}" --replicas=0 >/dev/null ||
		fail "scale to 0 failed"
	wait_frozen
	printf '%s\n' "${prior}" >"${HANDOVER_STATE_FILE:-/tmp/handover-replicas}"
	printf 'frozen: %s/%s scaled %s -> 0, pods terminated (prior in %s)\n' \
		"${NAMESPACE}" "${DEPLOY}" "${prior}" "${HANDOVER_STATE_FILE:-/tmp/handover-replicas}"
}

# drain_counts prints "claims items" from drain.sql (two result sets).
drain_counts() {
	psql_core "${sql_drain}" | tr '\n' ' '
}

# cmd_drain exits 0 only when no live unexpired claim or claimed item remains.
cmd_drain() {
	need_bin "${PSQL}" psql HANDOVER_PSQL_BIN
	[[ $# -eq 0 ]] || fail "drain takes no arguments"
	local counts claims items
	counts="$(drain_counts)"
	claims="$(printf '%s' "${counts}" | awk '{print $1}')"
	items="$(printf '%s' "${counts}" | awk '{print $2}')"
	[[ "${claims}" =~ ^[0-9]+$ && "${items}" =~ ^[0-9]+$ ]] ||
		fail "unreadable drain counts: ${counts}"
	if [[ "${claims}" -eq 0 && "${items}" -eq 0 ]]; then
		printf 'drained: 0 live claims, 0 claimed items\n'
		return 0
	fi
	printf 'draining: %s live claims, %s claimed items still held\n' "${claims}" "${items}" >&2
	return 1
}

# wait_drain polls drain until the deadline.
wait_drain() {
	local deadline=$((SECONDS + DRAIN_SECONDS))
	while true; do
		if cmd_drain; then return 0; fi
		[[ "${SECONDS}" -ge "${deadline}" ]] &&
			fail "drain deadline (${DRAIN_SECONDS}s) with live claims held"
		"${SLEEP_BIN}" 5
	done
}

# cmd_snapshot writes DIR/items.tsv, DIR/fences.tsv, DIR/sha256sums.
cmd_snapshot() {
	need_bin "${PSQL}" psql HANDOVER_PSQL_BIN
	need_bin "${SHA}" sha256sum HANDOVER_SHA_BIN
	local dir=""
	while [[ $# -gt 0 ]]; do
		case "$1" in
		--snapshot) dir="${2:?--snapshot needs a directory}"; shift 2 ;;
		*) fail "snapshot: unknown argument $1" ;;
		esac
	done
	[[ -n "${dir}" ]] || fail "snapshot needs --snapshot DIR"
	[[ -e "${dir}" ]] && fail "snapshot dir exists (refusing to overwrite): ${dir}"
	frozen_gate ||
		fail "not frozen ($(kubectl_get_counts)); freeze first so no ClaimNext lands mid-snapshot"
	cmd_drain >/dev/null ||
		fail "not drained; wait for leases to expire, then snapshot"
	[[ "${dir}" != *$'\n'* && "${dir}" != *"'"* ]] ||
		fail "snapshot dir must not contain a quote or newline"
	mkdir -p "${dir}"
	LC_ALL=C psql_core "${sql_items}" >"${dir}/items.tsv"
	LC_ALL=C psql_core "${sql_fences}" >"${dir}/fences.tsv"
	[[ -s "${dir}/items.tsv" ]] || fail "empty items snapshot; refusing an empty handover"
	(cd "${dir}" && "${SHA}" items.tsv fences.tsv >sha256sums)
	printf 'snapshot: %s (%s item rows, hash %s)\n' "${dir}" \
		"$(wc -l <"${dir}/items.tsv" | tr -d ' ')" \
		"$("${SHA}" "${dir}/items.tsv" | awk '{print $1}')"
}

# check_snapshot verifies DIR exists with matching hashes.
check_snapshot() { # dir
	local dir="$1"
	[[ -f "${dir}/items.tsv" && -f "${dir}/fences.tsv" && -f "${dir}/sha256sums" ]] ||
		fail "snapshot dir incomplete (want items.tsv, fences.tsv, sha256sums): ${dir}"
	(cd "${dir}" && "${SHA}" -c sha256sums >/dev/null) ||
		fail "snapshot hash mismatch: re-snapshot, never seed a edited snapshot"
}

# sat_session runs seed.sql against SATELLITE with DIR/items.tsv loaded as
# handover_snapshot, in one session. The session wrapper is mechanical; all
# seed logic lives in the committed seed.sql.
sat_session() { # dir
	local dir="$1"
	[[ "${dir}" != *$'\n'* && "${dir}" != *"'"* ]] ||
		fail "snapshot dir must not contain a quote or newline"
	local session
	mk_tmp session
	{
		printf 'CREATE TEMP TABLE handover_snapshot(work_item_id TEXT PRIMARY KEY, h BIGINT NOT NULL);\n'
		printf "\\copy handover_snapshot FROM '%s/items.tsv' WITH (FORMAT text, DELIMITER E'\\t');\n" "${dir}"
		cat "${sql_seed}"
	} >"${session}"
	psql_sat -A -t -f "${session}"
}

# cmd_seed advances satellite allocator tokens to max(snapshot, live).
cmd_seed() {
	need_bin "${PSQL}" psql HANDOVER_PSQL_BIN
	require_sat
	local dir="" yes=""
	while [[ $# -gt 0 ]]; do
		case "$1" in
		--snapshot) dir="${2:?--snapshot needs a directory}"; shift 2 ;;
		--yes) yes="1"; shift ;;
		*) fail "seed: unknown argument $1" ;;
		esac
	done
	[[ -n "${dir}" ]] || fail "seed needs --snapshot DIR"
	[[ "${yes}" == "1" ]] || fail "seed mutates the satellite: pass --yes"
	check_snapshot "${dir}"
	local out matched missing
	out="$(sat_session "${dir}")"
	matched="$(printf '%s' "${out}" | sed -n '1p')"
	missing="$(printf '%s' "${out}" | sed -n '2,$p' | grep -c . || true)"
	[[ "${matched}" =~ ^[0-9]+$ ]] ||
		fail "unreadable seed report: ${out}"
	printf 'seeded: %s items at or above snapshot H, %s snapshot items missing from satellite\n' \
		"${matched}" "${missing}"
	[[ "${missing}" -eq 0 ]] ||
		fail "unmigrated items present: resolve them before the flip, then re-snapshot"
}

# cmd_verify re-runs the seed file (a GREATEST no-op once seeded) and
# requires the full comparison: every snapshot item at/above H, none missing.
cmd_verify() {
	need_bin "${PSQL}" psql HANDOVER_PSQL_BIN
	require_sat
	local dir=""
	while [[ $# -gt 0 ]]; do
		case "$1" in
		--snapshot) dir="${2:?--snapshot needs a directory}"; shift 2 ;;
		*) fail "verify: unknown argument $1" ;;
		esac
	done
	[[ -n "${dir}" ]] || fail "verify needs --snapshot DIR"
	check_snapshot "${dir}"
	local want out matched missing
	want="$(wc -l <"${dir}/items.tsv" | tr -d ' ')"
	out="$(sat_session "${dir}")"
	matched="$(printf '%s' "${out}" | sed -n '1p')"
	missing="$(printf '%s' "${out}" | sed -n '2,$p' | grep -c . || true)"
	if [[ "${matched}" == "${want}" && "${missing}" -eq 0 ]]; then
		printf 'verified: %s/%s items at or above snapshot H, none missing\n' "${matched}" "${want}"
		return 0
	fi
	printf 'verify FAILED: %s/%s at H, %s missing\n' "${matched}" "${want}" "${missing}" >&2
	return 1
}

# cmd_abort restores coordinator replicas and records the abort. Pre-flip
# satellite seeds are inert while the satellite stays dark; a post-flip
# rollback must re-seed instead (cutover-rollback counterpart).
cmd_abort() {
	need_bin "${KUBECTL}" kubectl HANDOVER_KUBECTL_BIN
	require_namespace
	local dir=""
	while [[ $# -gt 0 ]]; do
		case "$1" in
		--snapshot) dir="${2:?--snapshot needs a directory}"; shift 2 ;;
		*) fail "abort: unknown argument $1" ;;
		esac
	done
	[[ -n "${dir}" ]] || fail "abort needs --snapshot DIR for the record"
	local state="${HANDOVER_STATE_FILE:-/tmp/handover-replicas}"
	[[ -f "${state}" ]] || fail "no freeze record at ${state}; refusing blind scale-up"
	local prior
	prior="$(cat "${state}")"
	[[ "${prior}" =~ ^[1-9][0-9]*$ ]] || fail "bad freeze record: ${prior}"
	"${KUBECTL}" --request-timeout="${KUBECTL_TIMEOUT}" -n "${NAMESPACE}" \
		scale deployment "${DEPLOY}" --replicas="${prior}" >/dev/null ||
		fail "scale back to ${prior} failed"
	printf 'aborted at %s (pre-flip; satellite seeds inert): %s/%s restored to %s replicas\n' \
		"$(date -u +%FT%TZ)" "${NAMESPACE}" "${DEPLOY}" "${prior}" | tee -a "${dir}/abort.log"
}

cmd_plan() {
	cat <<'PLAN'
freeze:   HANDOVER_NAMESPACE=.. scripts/claim-plane-cutover-handover.sh freeze
drain:    scripts/claim-plane-cutover-handover.sh drain            # exit 0 when empty
snapshot: scripts/claim-plane-cutover-handover.sh snapshot --snapshot DIR
seed:     scripts/claim-plane-cutover-handover.sh seed --snapshot DIR --yes
verify:   scripts/claim-plane-cutover-handover.sh verify --snapshot DIR
flip:     reviewed deploy only (authority switch on a verified snapshot)
abort:    scripts/claim-plane-cutover-handover.sh abort --snapshot DIR
PLAN
}

cmd_run() {
	local dir="" yes=""
	while [[ $# -gt 0 ]]; do
		case "$1" in
		--snapshot) dir="${2:?--snapshot needs a directory}"; shift 2 ;;
		--yes) yes="1"; shift ;;
		*) fail "run: unknown argument $1" ;;
		esac
	done
	[[ -n "${dir}" && "${yes}" == "1" ]] || fail "run needs --snapshot DIR --yes"
	local freeze_out
	if ! freeze_out="$(cmd_freeze 2>&1)"; then
		[[ "${freeze_out}" == *"already frozen (spec 0)"* ]] ||
			{ printf '%s\n' "${freeze_out}" >&2; return 1; }
	fi
	wait_drain
	cmd_snapshot --snapshot "${dir}"
	cmd_seed --snapshot "${dir}" --yes
	cmd_verify --snapshot "${dir}"
}

usage() {
	printf 'usage: %s {plan|freeze|drain|snapshot|seed|verify|abort|run} [--snapshot DIR] [--yes]\n' "$0" >&2
	exit 2
}

[[ $# -ge 1 ]] || usage
cmd="$1"
shift
case "${cmd}" in
plan) cmd_plan "$@" ;;
freeze) cmd_freeze "$@" ;;
drain) cmd_drain "$@" ;;
snapshot) cmd_snapshot "$@" ;;
seed) cmd_seed "$@" ;;
verify) cmd_verify "$@" ;;
abort) cmd_abort "$@" ;;
run) cmd_run "$@" ;;
*) usage ;;
esac
