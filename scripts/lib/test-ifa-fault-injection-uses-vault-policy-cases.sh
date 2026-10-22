#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154,SC2329
# Dynamic sources and indirect stub calls are the subject of these cases.
# Focused behavioral regressions for the uses_vault_policy
# fault-injection cells (#6228 slice A + #8539 seam: baseline,
# failgraphwrite, and the per-statement hold kill cell). Split into its own
# module for the same reason
# test-ifa-fault-injection-documentation-cases.sh was: the top-level static
# verifier and the review-case module both sit near the repository's
# 500-line cap. Sourced by scripts/test-verify-ifa-fault-injection.sh, and
# shares that script's ${repo_root} and `fail` helper.
#
# These cases are hermetic: every process, database, and signal the cells
# would touch is replaced by a shell stub, so they run in `make pre-pr`
# without Docker, Postgres, or a live gate. They prove the two seams that
# would otherwise fail silently in CI -- a projection stage that claims
# before its read-model input exists, and a kill that lands on an unheld
# worker because the hold marker never appeared -- not that the cells
# recover a real graph, which only the live fault matrix can show.

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

# ifa_uses_vault_policy_wait_for_endpoint_domains banks the graph
# endpoints the projection readiness gate (#1380) needs before the hold
# is armed: the kubernetes workload UID nodes are committed by the
# kubernetes_workload_materialization domain, whose rows sit unclaimed
# while the kill cell's reducers are scoped elsewhere. Without this
# wait the projection handler defers on secrets_iam_endpoint_not_ready
# forever, the hold never engages, and the marker wait dies at budget
# (CI fault-injection shard 4/4 on #8737: 120s timeout with exactly
# the workload UIDs missing). Polls fact_work_items by status and
# classifies in shell so the queue-lifecycle mapping is pinned here,
# not hidden in SQL: succeeded counts as done; pending/claimed/running/
# retrying count as open (retrying is transient -- the reclaim path
# re-drives stale rows through it); superseded is ignored
# (benign-terminal: the drain contract defines residual as NOT IN
# (succeeded, superseded)); failed/dead_letter -- plus any unknown
# future status -- fail fast. Zero committal rows is never a pass.
test_ifa_uses_vault_policy_wait_for_endpoint_domains_cases() (
	source "${det_lib}"
	source "${uses_vault_policy_cells_lib}"

	local rc
	psql() { :; }
	sleep() { :; }
	# Command substitution runs the stub in a subshell, so the poll
	# sequence advances through a counter file, not a variable.
	ifa_det_pg() {
		local idx
		local idx
		idx="$(cat "${poll_index_file}")"
		printf '%s' "$((idx + 1))" >"${poll_index_file}"
		printf '%s\n' "${endpoint_poll_outputs[${idx}]}"
	}

	use_compose=0
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	FAULT_COMPOSE_PROJECT="test"
	log_dir="$(mktemp -d -t ifa-uses-vault-policy-endpoints.XXXXXX)"
	poll_index_file="$(mktemp -t ifa-uses-vault-policy-pollidx.XXXXXX)"
	trap 'rm -rf "${log_dir}" "${poll_index_file}"' EXIT

	# Endpoints committed: one poll, success, no waiting.
	endpoint_poll_outputs=("succeeded|3")
	printf '%s' 0 >"${poll_index_file}"
		rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains readycell 5 || rc=$?
	[[ "${rc}" -eq 0 && "$(cat "${poll_index_file}")" -eq 1 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains failed while the endpoint domain drained with succeeded rows (rc=${rc} calls=$(cat "${poll_index_file}"))"

	# A retrying row is waited on, not failed: first poll open, second
	# poll drained. This is the P1: retrying is transient, and failing
	# fast on it would false-red the cell the first time a workload
	# row retries.
	endpoint_poll_outputs=("retrying|1" "succeeded|2")
	printf '%s' 0 >"${poll_index_file}"
		rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains retrycell 5 || rc=$?
	[[ "${rc}" -eq 0 && "$(cat "${poll_index_file}")" -eq 2 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains failed fast on a retrying endpoint row instead of waiting for it (rc=${rc} calls=$(cat "${poll_index_file}"))"

	# Superseded beside succeeded is ignored, not blocking and not bad.
	endpoint_poll_outputs=("superseded|1
succeeded|1")
	printf '%s' 0 >"${poll_index_file}"
	rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains mixedcell 5 || rc=$?
	[[ "${rc}" -eq 0 && "$(cat "${poll_index_file}")" -eq 1 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains blocked or failed on a superseded row beside succeeded rows (rc=${rc})"

	# A superseded row is ignored: it neither blocks nor passes alone.
	endpoint_poll_outputs=("superseded|1")
	printf '%s' 0 >"${poll_index_file}"
		rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains supersededcell 2 || rc=$?
	[[ "${rc}" -ne 0 && "$(cat "${poll_index_file}")" -eq 2 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains passed on superseded rows alone, or failed without exhausting the budget (rc=${rc} calls=$(cat "${poll_index_file}"))"

	# A dead-letter row fails fast: one poll, no budget burn.
	endpoint_poll_outputs=("dead_letter|1")
	printf '%s' 0 >"${poll_index_file}"
		rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains deadcell 5 || rc=$?
	[[ "${rc}" -ne 0 && "$(cat "${poll_index_file}")" -eq 1 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains did not fail fast on a dead-letter endpoint row (rc=${rc} calls=$(cat "${poll_index_file}"))"

	# An unknown future status fails fast in the safe direction.
	endpoint_poll_outputs=("bogus|1")
	printf '%s' 0 >"${poll_index_file}"
		rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains unknowncell 5 || rc=$?
	[[ "${rc}" -ne 0 && "$(cat "${poll_index_file}")" -eq 1 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains did not fail fast on an unknown endpoint row status (rc=${rc} calls=$(cat "${poll_index_file}"))"

	# Open rows that never drain: exhausts the budget and fails instead
	# of arming a hold the projection can never reach.
	endpoint_poll_outputs=("pending|2")
	printf '%s' 0 >"${poll_index_file}"
		rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains unready 2 || rc=$?
	[[ "${rc}" -ne 0 && "$(cat "${poll_index_file}")" -eq 2 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains returned success with endpoint rows still open (rc=${rc} calls=$(cat "${poll_index_file}"))"

	# A non-numeric count fails instead of classifying garbage.
	endpoint_poll_outputs=("succeeded|lots")
	printf '%s' 0 >"${poll_index_file}"
		rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains nonnumeric 2 || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains returned success on a non-numeric endpoint count"

	# A bad budget fails before any poll.
	endpoint_poll_outputs=("succeeded|3")
	printf '%s' 0 >"${poll_index_file}"
		rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains badbudget 0 || rc=$?
	[[ "${rc}" -ne 0 && "$(cat "${poll_index_file}")" -eq 0 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains polled on a bad budget (rc=${rc} calls=$(cat "${poll_index_file}"))"
)

run_ifa_fault_injection_uses_vault_policy_cases() {
	test_ifa_uses_vault_policy_wait_for_read_models_cases
	test_ifa_uses_vault_policy_wait_for_endpoint_domains_cases
	test_ifa_uses_vault_policy_wait_for_hold_marker_cases
	test_ifa_uses_vault_policy_kill_cell_is_defined
}

# ifa_uses_vault_policy_wait_for_hold_marker is the kill cell's rendezvous:
# kill -9 lands only once the hold decorator's marker proves the worker is
# parked inside the USES_VAULT_POLICY MERGE. Cases: the marker already
# present returns success; a missing marker exhausts the budget and fails
# (an inert hold must never read as "held"); a bad budget fails instead of
# polling forever or returning success.
test_ifa_uses_vault_policy_wait_for_hold_marker_cases() (
	source "${det_lib}"
	source "${uses_vault_policy_cells_lib}"

	local rc work script
	work="$(mktemp -d -t ifa-uses-vault-policy-holdmarker.XXXXXX)"
	trap 'rm -rf "${work}"' EXIT
	script="${work}/fault-hold.json"
	sleep() { :; }

	# Marker already present: success without consuming budget.
	: > "${script}.restart-sentinel.statement-held"
	rc=0
	ifa_uses_vault_policy_wait_for_hold_marker holdcell "${script}" 5 || rc=$?
	[[ "${rc}" -eq 0 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_hold_marker failed while the hold marker exists"

	# Marker never appears: exhausts the budget and fails.
	rm -f "${script}.restart-sentinel.statement-held"
	rc=0
	ifa_uses_vault_policy_wait_for_hold_marker holdcell "${script}" 2 || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_hold_marker returned success with no hold marker; the kill would land on an unheld worker"

	# Bad budget: fails instead of polling.
	rc=0
	ifa_uses_vault_policy_wait_for_hold_marker holdcell "${script}" 0 || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_hold_marker accepted a zero budget"
)

# The seam's kill cell must exist in the sourced lib: the gate dispatches
# cell_kill_uses_vault_policy by name, and a rename without a dispatch
# update would die only inside the live shard.
test_ifa_uses_vault_policy_kill_cell_is_defined() (
	source "${repo_root}/scripts/lib/ifa_fault_injection_hold.sh"
	source "${uses_vault_policy_cells_lib}"
	declare -F cell_kill_uses_vault_policy >/dev/null \
		|| fail "cell_kill_uses_vault_policy is not defined in ${uses_vault_policy_cells_lib}"
	declare -F ifa_fault_write_hold_script >/dev/null \
		|| fail "ifa_fault_write_hold_script is not defined in scripts/lib/ifa_fault_injection_hold.sh"
	declare -F ifa_fault_assert_hold_marker >/dev/null \
		|| fail "ifa_fault_assert_hold_marker is not defined in scripts/lib/ifa_fault_injection_hold.sh"
)
