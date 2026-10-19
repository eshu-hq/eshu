#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154,SC2329
# Dynamic sources and indirect stub calls are the subject of these cases.
# Focused behavioral regressions for the uses_vault_policy
# fault-injection cells (#6228 slice A, kill-less pair per arbiter verdict
# Option B on #6228 -- no kill-worker cell, gap #8539). Split into its own
# module for the same reason
# test-ifa-fault-injection-documentation-cases.sh was: the top-level static
# verifier and the review-case module both sit near the repository's
# 500-line cap. Sourced by scripts/test-verify-ifa-fault-injection.sh, and
# shares that script's ${repo_root} and `fail` helper.
#
# These cases are hermetic: every process, database, and signal the cells
# would touch is replaced by a shell stub, so they run in `make pre-pr`
# without Docker, Postgres, or a live gate. They prove the one seam that
# would otherwise fail silently in CI -- a projection stage that claims
# before its read-model input exists -- not that the cells recover a real
# graph, which only the live fault matrix can show.

uses_vault_policy_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_uses_vault_policy_cells.sh"
[[ -f "${uses_vault_policy_cells_lib}" ]] || fail "missing ${uses_vault_policy_cells_lib}"
bash -n "${uses_vault_policy_cells_lib}" || fail "ifa_fault_injection_uses_vault_policy_cells.sh has a syntax error"

# ifa_uses_vault_policy_wait_for_read_models banks the projection
# stage's INPUT before the fact_records lock is taken: the projection handler
# loads reducer-owned read-model facts, so locking first lets it claim an
# empty generation, succeed vacuously, and never re-run. The helper resolves
# the scope off the trust-chain row and polls fact_records for the two
# read-model kinds. Fail-closed throughout: no trust-chain row, a failed
# query, or budget exhaustion all return non-zero.
test_ifa_uses_vault_policy_wait_for_read_models_cases() (
	source "${det_lib}"
	source "${uses_vault_policy_cells_lib}"

	local rc
	psql() { :; }
	sleep() { :; }
	ifa_det_pg() {
		case "$*" in
			*fact_records*) printf '%s\n' "${fact_count_output}" ;;
			*) printf '%s\n' "${scope_output}" ;;
		esac
	}

	use_compose=0
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	FAULT_COMPOSE_PROJECT="test"
	log_dir="$(mktemp -d -t ifa-uses-vault-policy-readmodels.XXXXXX)"
	trap 'rm -rf "${log_dir}"' EXIT

	# Read models already committed: returns success without consuming budget.
	scope_output="vault:eshu-fixture-uses-account"
	fact_count_output="3"
	rc=0
	ifa_uses_vault_policy_wait_for_read_models readycell 5 || rc=$?
	[[ "${rc}" -eq 0 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_read_models failed while read-model facts exist"

	# Read models never appear: exhausts the budget and fails.
	scope_output="vault:eshu-fixture-uses-account"
	fact_count_output="0"
	rc=0
	ifa_uses_vault_policy_wait_for_read_models unready 2 || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_read_models returned success with no read-model facts; the projection stage would claim before input and project vacuously"

	# No trust-chain row to resolve a scope from: fails instead of polling
	# for facts in a scope nobody named.
	scope_output=""
	fact_count_output="3"
	rc=0
	ifa_uses_vault_policy_wait_for_read_models norow 2 || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_read_models returned success with no trust-chain row to resolve"
)

run_ifa_fault_injection_uses_vault_policy_cases() {
	test_ifa_uses_vault_policy_wait_for_read_models_cases
}
