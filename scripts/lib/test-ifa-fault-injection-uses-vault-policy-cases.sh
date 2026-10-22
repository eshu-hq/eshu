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
# is armed. The single-domain cases below pin the queue-lifecycle mapping
# for one endpoint domain (kubernetes_workload_materialization): succeeded
# counts as done; pending/claimed/running/retrying count as open
# (retrying is transient -- the reclaim path re-drives stale rows through
# it); superseded is ignored (benign-terminal: the drain contract defines
# residual as NOT IN (succeeded, superseded)); failed/dead_letter -- plus
# any unknown future status -- fail fast. Zero committal rows is never a
# pass. The multi-domain cases beside them pin the two-domain shape the
# kill cell needs: the cloud set is NOT empty (the trust chain derives
# iam_role_cloud_resource_uid values the aws_resource_materialization
# domain commits), so every listed domain needs ok>=1 with none open --
# CI shard 4/4 on #8737 died on exactly the single-domain vacuous pass.
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
	endpoint_poll_outputs=("kubernetes_workload_materialization|succeeded|3")
	printf '%s' 0 >"${poll_index_file}"
		rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains readycell 5 kubernetes_workload_materialization || rc=$?
	[[ "${rc}" -eq 0 && "$(cat "${poll_index_file}")" -eq 1 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains failed while the endpoint domain drained with succeeded rows (rc=${rc} calls=$(cat "${poll_index_file}"))"

	# A retrying row is waited on, not failed: first poll open, second
	# poll drained. This is the P1: retrying is transient, and failing
	# fast on it would false-red the cell the first time a workload
	# row retries.
	endpoint_poll_outputs=("kubernetes_workload_materialization|retrying|1" "kubernetes_workload_materialization|succeeded|2")
	printf '%s' 0 >"${poll_index_file}"
		rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains retrycell 5 kubernetes_workload_materialization || rc=$?
	[[ "${rc}" -eq 0 && "$(cat "${poll_index_file}")" -eq 2 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains failed fast on a retrying endpoint row instead of waiting for it (rc=${rc} calls=$(cat "${poll_index_file}"))"

	# Superseded beside succeeded is ignored, not blocking and not bad.
	endpoint_poll_outputs=("kubernetes_workload_materialization|superseded|1
kubernetes_workload_materialization|succeeded|1")
	printf '%s' 0 >"${poll_index_file}"
	rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains mixedcell 5 kubernetes_workload_materialization || rc=$?
	[[ "${rc}" -eq 0 && "$(cat "${poll_index_file}")" -eq 1 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains blocked or failed on a superseded row beside succeeded rows (rc=${rc})"

	# A superseded row is ignored: it neither blocks nor passes alone.
	endpoint_poll_outputs=("kubernetes_workload_materialization|superseded|1")
	printf '%s' 0 >"${poll_index_file}"
		rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains supersededcell 2 kubernetes_workload_materialization || rc=$?
	[[ "${rc}" -ne 0 && "$(cat "${poll_index_file}")" -eq 2 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains passed on superseded rows alone, or failed without exhausting the budget (rc=${rc} calls=$(cat "${poll_index_file}"))"

	# A dead-letter row fails fast: one poll, no budget burn.
	endpoint_poll_outputs=("kubernetes_workload_materialization|dead_letter|1")
	printf '%s' 0 >"${poll_index_file}"
		rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains deadcell 5 kubernetes_workload_materialization || rc=$?
	[[ "${rc}" -ne 0 && "$(cat "${poll_index_file}")" -eq 1 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains did not fail fast on a dead-letter endpoint row (rc=${rc} calls=$(cat "${poll_index_file}"))"

	# An unknown future status fails fast in the safe direction.
	endpoint_poll_outputs=("kubernetes_workload_materialization|bogus|1")
	printf '%s' 0 >"${poll_index_file}"
		rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains unknowncell 5 kubernetes_workload_materialization || rc=$?
	[[ "${rc}" -ne 0 && "$(cat "${poll_index_file}")" -eq 1 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains did not fail fast on an unknown endpoint row status (rc=${rc} calls=$(cat "${poll_index_file}"))"

	# Open rows that never drain: exhausts the budget and fails instead
	# of arming a hold the projection can never reach.
	endpoint_poll_outputs=("kubernetes_workload_materialization|pending|2")
	printf '%s' 0 >"${poll_index_file}"
		rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains unready 2 kubernetes_workload_materialization || rc=$?
	[[ "${rc}" -ne 0 && "$(cat "${poll_index_file}")" -eq 2 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains returned success with endpoint rows still open (rc=${rc} calls=$(cat "${poll_index_file}"))"

	# A non-numeric count fails instead of classifying garbage.
	endpoint_poll_outputs=("kubernetes_workload_materialization|succeeded|lots")
	printf '%s' 0 >"${poll_index_file}"
		rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains nonnumeric 2 kubernetes_workload_materialization || rc=$?
	[[ "${rc}" -ne 0 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains returned success on a non-numeric endpoint count"

	# A bad budget fails before any poll.
	endpoint_poll_outputs=("kubernetes_workload_materialization|succeeded|3")
	printf '%s' 0 >"${poll_index_file}"
		rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains badbudget 0 kubernetes_workload_materialization || rc=$?
	[[ "${rc}" -ne 0 && "$(cat "${poll_index_file}")" -eq 0 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains polled on a bad budget (rc=${rc} calls=$(cat "${poll_index_file}"))"
)

# ifa_uses_vault_policy_wait_for_endpoint_domains with TWO endpoint domains
# (CI fault-injection shard 4/4 on #8737: the single-domain wait passed on
# the kubernetes workload endpoints while the gate still deferred on 2
# uncommitted cloud_resource_uid endpoints, and the marker wait died at
# budget with the hold never engaged). The trust chain enriches envelopes
# with iam_role_cloud_resource_uid values derived from aws_iam_principal
# facts, and the aws_resource_materialization domain commits those nodes
# from the same cassette's aws_resource facts -- so the kill cell must
# drain BOTH domains before arming the hold. Rows carry domain|status|count
# and every listed domain needs ok>=1 with none open; a domain with zero
# rows never passes (otherwise the wait would go vacuous on the domain the
# cell forgot to drain -- exactly the 4/4 shape).
test_ifa_uses_vault_policy_wait_for_endpoint_domains_multi_cases() (
	source "${det_lib}"
	source "${uses_vault_policy_cells_lib}"

	local rc
	psql() { :; }
	sleep() { :; }
	ifa_det_pg() {
		local idx
		idx="$(cat "${poll_index_file}")"
		printf '%s' "$((idx + 1))" >"${poll_index_file}"
		printf '%s\n' "${endpoint_poll_outputs[${idx}]}"
	}

	use_compose=0
	ESHU_POSTGRES_DSN="postgresql://unused"
	compose_file="docker-compose.yaml"
	FAULT_COMPOSE_PROJECT="test"
	log_dir="$(mktemp -d -t ifa-uses-vault-policy-endpoints-multi.XXXXXX)"
	poll_index_file="$(mktemp -t ifa-uses-vault-policy-pollidx-multi.XXXXXX)"
	trap 'rm -rf "${log_dir}" "${poll_index_file}"' EXIT

	# Both endpoint domains drained: one poll, success, no waiting.
	endpoint_poll_outputs=("kubernetes_workload_materialization|succeeded|1
aws_resource_materialization|succeeded|2")
	printf '%s' 0 >"${poll_index_file}"
	rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains readycell 5 kubernetes_workload_materialization aws_resource_materialization || rc=$?
	[[ "${rc}" -eq 0 && "$(cat "${poll_index_file}")" -eq 1 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains failed while both endpoint domains drained (rc=${rc} calls=$(cat "${poll_index_file}"))"

	# One domain open: waited on, not failed -- first poll open, second
	# poll drained. This is the 4/4 shape: the workload domain drained
	# while the cloud domain was still open.
	endpoint_poll_outputs=("kubernetes_workload_materialization|succeeded|1
aws_resource_materialization|pending|2" "kubernetes_workload_materialization|succeeded|1
aws_resource_materialization|succeeded|2")
	printf '%s' 0 >"${poll_index_file}"
	rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains retrycell 5 kubernetes_workload_materialization aws_resource_materialization || rc=$?
	[[ "${rc}" -eq 0 && "$(cat "${poll_index_file}")" -eq 2 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains failed fast on an open cloud domain instead of waiting for it (rc=${rc} calls=$(cat "${poll_index_file}"))"

	# A domain with zero rows never passes: the wait must exhaust the
	# budget and fail instead of arming a hold on a half-committed graph.
	# (A single-domain poll of only the drained domain would pass here --
	# the vacuous pass that killed shard 4/4.)
	endpoint_poll_outputs=("kubernetes_workload_materialization|succeeded|1")
	printf '%s' 0 >"${poll_index_file}"
	rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains missingdomain 2 kubernetes_workload_materialization aws_resource_materialization || rc=$?
	[[ "${rc}" -ne 0 && "$(cat "${poll_index_file}")" -eq 2 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains passed with the cloud domain drained of zero rows (rc=${rc} calls=$(cat "${poll_index_file}"))"

	# A dead-letter row in EITHER domain fails fast: one poll, no wait.
	endpoint_poll_outputs=("kubernetes_workload_materialization|succeeded|1
aws_resource_materialization|dead_letter|1")
	printf '%s' 0 >"${poll_index_file}"
	rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains deadcell 5 kubernetes_workload_materialization aws_resource_materialization || rc=$?
	[[ "${rc}" -ne 0 && "$(cat "${poll_index_file}")" -eq 1 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains did not fail fast on a dead-letter cloud row (rc=${rc} calls=$(cat "${poll_index_file}"))"

	# A row for a domain outside the waited set fails fast: the IN filter
	# makes this unreachable in production, but a harness bug that leaks
	# another domain's rows must never classify silently.
	endpoint_poll_outputs=("some_other_domain|succeeded|1")
	printf '%s' 0 >"${poll_index_file}"
	rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains extradata 5 kubernetes_workload_materialization aws_resource_materialization || rc=$?
	[[ "${rc}" -ne 0 && "$(cat "${poll_index_file}")" -eq 1 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains classified a row for an unlisted domain (rc=${rc} calls=$(cat "${poll_index_file}"))"

	# Zero listed domains fails before any poll: an unspecified graph can
	# never be ready.
	endpoint_poll_outputs=("kubernetes_workload_materialization|succeeded|1")
	printf '%s' 0 >"${poll_index_file}"
	rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains nodomains 5 || rc=$?
	[[ "${rc}" -ne 0 && "$(cat "${poll_index_file}")" -eq 0 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains polled with no endpoint domains listed (rc=${rc} calls=$(cat "${poll_index_file}"))"

	# An invalid domain name fails before any poll: it never reaches SQL.
	endpoint_poll_outputs=("kubernetes_workload_materialization|succeeded|1")
	printf '%s' 0 >"${poll_index_file}"
	rc=0
	ifa_uses_vault_policy_wait_for_endpoint_domains baddomain 5 "kubernetes_workload_materialization; DROP TABLE fact_work_items;--" || rc=$?
	[[ "${rc}" -ne 0 && "$(cat "${poll_index_file}")" -eq 0 ]] \
		|| fail "ifa_uses_vault_policy_wait_for_endpoint_domains polled with an invalid endpoint domain (rc=${rc} calls=$(cat "${poll_index_file}"))"
)

run_ifa_fault_injection_uses_vault_policy_cases() {
	test_ifa_uses_vault_policy_wait_for_read_models_cases
	test_ifa_uses_vault_policy_wait_for_endpoint_domains_cases
	test_ifa_uses_vault_policy_wait_for_endpoint_domains_multi_cases
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
