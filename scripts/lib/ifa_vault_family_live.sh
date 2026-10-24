#!/usr/bin/env bash
# shellcheck shell=bash
# Live-gate drive/assert callbacks for the three vault DIRECT-materialization
# families (#6228 slice A plus the grants_secret_read remainder):
# authenticates_vault_role, uses_vault_policy, and grants_secret_read.
# Split out of scripts/lib/ifa_direct_family_live.sh so
# that file stays under the tooling line cap; same four-line drive/assert
# shape, one pair per family.
#
# SOURCED BY scripts/verify-ifa-determinism.sh AND the fault gate through
# scripts/lib/ifa_fault_injection_sources.sh. The fault cells
# (scripts/lib/ifa_fault_injection_<family>_cells.sh) call the drive/assert
# callbacks below; the families' registry rows carry cell_kind=custom.
# Callers own strict mode and cleanup.
#
# WHY THESE ARE TWO-STAGE, and why that changes nothing here: the families'
# edges are written by the DomainSecretsIAMGraphProjection handler, which
# reads reducer-owned read-model facts -- so the live cells run the
# trust-chain stage first (trust-chain-scoped reducer, flag on) and only then
# the projection stage (projection-scoped reducer, flag on). The gate does
# not care -- it drives a cassette and asserts an exact edge set either way.
# The projection domain (secrets_iam_graph_projection) is what
# `eshu-ifa assert-edges -domain <family>` addresses and what the fault cells
# hold in flight (see rows/21_authenticates_vault_role.sh and its siblings).

# ifa_authenticates_vault_role_drive replays the authenticates-vault-role cassette.
ifa_authenticates_vault_role_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive authenticates-vault-role \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_authenticates_vault_role_assert pins the exact set named by its third
# argument: the two-edge file in per-cell (fault) consumers, the eight-edge
# combined live file in the shared N-cell loop (see
# IFA_FAMILY_SHARED_EXPECTED_VAR in rows/21). Called twice per cell for the
# reason recorded on the namespace assert in ifa_direct_family_live.sh:
# post-delta is the only place an identical-across-N generation-2 mutation
# shows up.
#
# The scope-local two edges are the exact-digest resolutions: the IRSA
# web-identity service account authenticating to its Vault role, and the EKS
# Pod Identity service account authenticating to its Vault role -- checked in
# both the pre-delta and post-delta phase of each N cell, so a generation-2
# SQL delta that touched an unrelated family's Vault-adjacent rows would
# still have to preserve this family's set to stay green.
ifa_authenticates_vault_role_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert authenticates_vault_role materialized edges (exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain authenticates_vault_role \
		-expected "${expected_edges}"
}

# ifa_uses_vault_policy_drive replays the uses-vault-policy cassette.
ifa_uses_vault_policy_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive uses-vault-policy \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_uses_vault_policy_assert pins the exact set named by its third
# argument: the three-edge file in per-cell (fault) consumers, the eight-edge
# combined live file in the shared N-cell loop (see
# IFA_FAMILY_SHARED_EXPECTED_VAR in rows/22). Called twice per cell for the
# reason recorded on the namespace assert in ifa_direct_family_live.sh:
# post-delta is the only place an identical-across-N generation-2 mutation
# shows up.
#
# The three edges are the exact-digest resolutions: the IRSA lane's Vault
# role using its two token policies, and the EKS Pod Identity lane's Vault
# role using its one token policy -- checked in both the pre-delta and
# post-delta phase of each N cell, so a generation-2 SQL delta that touched
# an unrelated family's Vault-adjacent rows would still have to preserve
# this family's set to stay green.
ifa_uses_vault_policy_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert uses_vault_policy materialized edges (exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain uses_vault_policy \
		-expected "${expected_edges}"
}

# ifa_grants_secret_read_drive replays the grants-secret-read cassette.
ifa_grants_secret_read_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive grants-secret-read \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_grants_secret_read_assert pins the three-edge exact set. Called
# twice per cell for the reason recorded on the namespace assert in
# ifa_direct_family_live.sh: post-delta is the only place an
# identical-across-N generation-2 mutation shows up.
#
# The three edges are the exact-digest resolutions: the IRSA lane's role
# carrying two token policies with one read rule each, and the EKS Pod
# Identity lane's role carrying one policy with one read rule -- checked in
# both the pre-delta and post-delta phase of each N cell, so a
# generation-2 SQL delta that touched an unrelated family's Vault-adjacent
# rows would still have to preserve this family's set to stay green.
ifa_grants_secret_read_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert grants_secret_read materialized edges (three-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain grants_secret_read \
		-expected "${expected_edges}"
}
