#!/usr/bin/env bash
# shellcheck shell=bash
# Live-gate drive/assert callbacks for the two DIRECT-materialization families
# (#6228): kubernetes_namespace_environment and iam_instance_profile_role.
#
# SOURCED BY scripts/verify-ifa-determinism.sh AND, since #6309, the fault
# gate through scripts/lib/ifa_fault_injection_sources.sh. The fault cells
# (scripts/lib/ifa_fault_injection_kubernetes_namespace_environment_cells.sh
# and scripts/lib/ifa_fault_injection_iam_instance_profile_role_cells.sh)
# call the drive/assert callbacks below; the families' registry rows carry
# cell_kind=custom. Callers own strict mode and cleanup.
#
# ONE FILE FOR TWO FAMILIES, unlike the shared-projection families' one file
# each. Their drive and assert bodies differ only in a cassette path, a domain
# name and a log filename, and both are four lines of real work; two files
# would be one contract in two places. The per-family REGISTRY ROWS stay
# separate, which is where the split that matters already is.
#
# WHY THESE ARE DIRECT, and why that changes nothing here: the reducer writes
# both families straight to a go/internal/storage/cypher writer rather than
# through a shared-projection intent row. The gate does not care -- it drives a
# cassette and asserts an exact edge set either way -- but it does mean the
# handler is scheduled by an ordinary fact_work_items domain
# (kubernetes_namespace_materialization / iam_instance_profile_role_materialization)
# rather than by a shared_followup fact the cassette has to carry.

# ifa_direct_family_drive replays one committed family cassette into a matrix
# cell. The caller performs the aggregate fact_work_items non-vacuity check.
#
# label/slug are separate arguments because the label carries the cell identity
# (n1, N=2, "post-delta N=4") and would make an unusable filename, while the
# slug is the stable per-family log name.
_ifa_direct_family_drive() {
	local slug="$1" label="$2" bin_dir="$3" cassette="$4" workers="$5" log_dir="$6"
	printf '\n=== %s: drive %s family cassette (-workers %s) ===\n' "${label}" "${slug}" "${workers}"
	if ! "${bin_dir}/eshu-ifa" drive -cassette "${cassette}" -workers "${workers}" \
		>"${log_dir}/ifa-drive-${slug}-${label}.log" 2>&1; then
		tail -40 "${log_dir}/ifa-drive-${slug}-${label}.log" >&2 || true
		return 1
	fi
	cat "${log_dir}/ifa-drive-${slug}-${label}.log"
}

# Each assert below spells its `-domain <family>` flag out literally rather
# than taking the domain as a parameter. That is deliberate: the domain is the
# one thing a shared helper must not abstract away, because it is what makes
# these two families' assertions distinguishable from each other, and
# scripts/lib/test-ifa-determinism-family-cases.sh greps each family's own flag
# out of this file to prove the wiring exists. A parameterized call would let
# one family's coverage stand in for the other's and satisfy that check with a
# single needle.

# ifa_kubernetes_namespace_environment_drive replays the namespace cassette.
ifa_kubernetes_namespace_environment_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive kubernetes-namespace-environment \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_kubernetes_namespace_environment_assert pins the two-edge exact set.
#
# CALLED TWICE PER CELL, pre-delta inside the registry loop and again after
# ifa_det_run_sql_delta_live. The second call is not belt-and-braces: the matrix
# compares one canonicalized digest per N, so a generation-2 regression that
# retracted or mutated these edges IDENTICALLY at N=1, 2 and 4 leaves all three
# digests equal and the gate green while the graph no longer matches the
# expected set. Only re-running the exact-set assertion after generation 2 can
# see that. Both direct families were asserted pre-delta only when they first
# landed (#6309).
#
# Two of the Odù's four namespaces bind an Environment and two deliberately do
# not, so this assertion is as much about the two edges that must NOT exist as
# the two that must. The targets are the CANONICAL environment names ("prod",
# "stage"), not the raw labels ("production", "staging"): the reducer
# canonicalizes through environment.Canonical, and asserting the raw values
# would pass on a build that had dropped that step.
#
# The target endpoints are name-keyed Environment nodes carrying no uid and no
# id, which `ifa assert-edges` resolves through endpointID's Environment-scoped
# "name" fallback. Before that fallback existed this family's edges materialized
# correctly and the gate still reported every one of them an unmaterialized
# endpoint.
ifa_kubernetes_namespace_environment_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert kubernetes_namespace_environment materialized edges (two-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain kubernetes_namespace_environment \
		-expected "${expected_edges}"
}

# ifa_iam_instance_profile_role_drive replays the instance-profile cassette.
ifa_iam_instance_profile_role_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive iam-instance-profile-role \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_fault_count_retry_attempts totals the EXCESS attempts
# (sum(attempt_count - 1)) for one materialization domain, and
# ifa_fault_assert_retry_attempts_above proves the fault ADDED attempts the
# fault-free baseline lacked by requiring a strict increase over it.
#
# Why a second retry signal next to ifa_fault_count_retried's row COUNT in
# ifa_fault_injection_common.sh: each direct-family cassette creates exactly
# ONE targeted work item, so the count form saturates at 1. A single natural
# counting-class retry in the fault-free baseline (a real NornicDB deadlock or
# a transient EntityNotFound under the concurrent projector+reducer this gate
# runs -- both documented on the count helper) makes the baseline 1; the
# forced kill then adds another attempt to the SAME row, the kill-run count
# stays 1, and 1 > 1 false-fails a correct recovery. The sum form has no
# ceiling: the kill always adds attempts the baseline lacked. Multi-row
# domains keep the count form -- saturating every row naturally is
# implausible there, and the count form's "measured inert" reasoning (a
# natural retry also appears in the identical baseline drive, so it cannot
# green the check while the decorator sits inert) carries over unchanged:
# only attempts the baseline lacked move this sum.
#
# Args (count): compose_project use_compose dsn compose_file [domain].
# Args (assert): compose_project use_compose dsn compose_file baseline [budget_seconds=15] [domain].
ifa_fault_count_retry_attempts() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local domain="${5:-kubernetes_namespace_materialization}"
	if [[ ! "${domain}" =~ ^[a-z0-9_]+$ ]]; then
		echo "ifa_fault_count_retry_attempts: domain must match ^[a-z0-9_]+$, got ${domain}" >&2
		return 1
	fi
	ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \
		"SELECT coalesce(sum(attempt_count - 1), 0) FROM fact_work_items WHERE stage = 'reducer' AND status = 'succeeded' AND attempt_count > 1 AND domain = '${domain}';" \
		"${compose_file}" | tr -d '[:space:]'
}

ifa_fault_assert_retry_attempts_above() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local baseline="$5" budget="${6:-15}" domain="${7:-kubernetes_namespace_materialization}"
	local i count
	for i in $(seq 1 "${budget}"); do
		count="$(ifa_fault_count_retry_attempts "${compose_project}" "${use_compose}" "${dsn}" "${compose_file}" "${domain}")"
		if [[ -n "${count}" && "${count}" -gt "${baseline}" ]]; then
			printf '%s' "${count}"
			return 0
		fi
		sleep 1
	done
	return 1
}

# ifa_iam_instance_profile_role_assert pins the two-edge exact set. Called twice
# per cell for the reason recorded on the namespace assert above: post-delta is
# the only place an identical-across-N generation-2 mutation shows up.
#
# Both edges come from ONE instance profile attaching two scanned roles, so a
# regression that emitted one edge per profile instead of one per attachment
# still produces "some edges" and fails only against an exact set. The Odù's
# other two profiles -- one naming a role ARN nothing scanned, one with an empty
# attachment list -- must contribute nothing; the extractor drops an unresolved
# target rather than inventing an endpoint, and this set is what holds it to
# that.
#
# The relationship type is HAS_ROLE, read off the writer's MERGE. It is NOT
# IAM_INSTANCE_PROFILE_HAS_ROLE, which is statement metadata carried beside the
# query and never reaches the graph.
ifa_iam_instance_profile_role_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert iam_instance_profile_role materialized edges (two-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain iam_instance_profile_role \
		-expected "${expected_edges}"
}
