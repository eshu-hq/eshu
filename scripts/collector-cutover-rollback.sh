#!/usr/bin/env bash
# Roll back a collector producer-authority cutover (#8895, epic #6707).
#
# The ecosystem plan defines rollback as a producer-authority transition, not a
# deployment reversal. This script strings the EXISTING primitives together in
# the one safe order and refuses to move on when a precondition does not hold:
#
#   1. stop new claims for the external producer
#   2. wait for in-flight leases to drain
#   3. revoke the external producer's grant
#   4. restore the last supported (in-tree) producer
#   5. replay from the last committed generation
#   6. verify exact fact, projection, graph, and read truth
#
# It adds no new core command. See docs/public/operate/collector-cutover-rollback.md
# for the runbook, the evidence to record, and the read-truth queries.
#
# Subcommands:
#   plan    print the six steps with the exact commands; runs nothing
#   run     execute the steps (requires --yes)
#   verify  check authority and compare truth to a baseline (read-only)
#
# Dependencies are injected through the environment so tests can stub them:
#   ESHU_BIN (eshu)  KUBECTL_BIN (kubectl)  PSQL_BIN (psql)  HELM_BIN (helm)
# psql reads its connection from the standard libpq environment (PGHOST,
# PGUSER, PGDATABASE, PGPASSFILE, ...). No secret is ever passed on a command
# line or printed.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

ESHU_BIN="${ESHU_BIN:-eshu}"
KUBECTL_BIN="${KUBECTL_BIN:-kubectl}"
PSQL_BIN="${PSQL_BIN:-psql}"
HELM_BIN="${HELM_BIN:-helm}"

# Exit codes. Distinct codes let a wrapper tell a refusal from a failure.
readonly EX_USAGE=2
readonly EX_STEP_FAILED=3
readonly EX_REFUSED=4
readonly EX_TIMEOUT=5
readonly EX_TRUTH_MISMATCH=6
readonly EX_DUAL_AUTHORITY=7

usage() {
	cat <<'USAGE'
Usage: collector-cutover-rollback.sh <plan|run|verify> [options]

Identity (plan and run require all of these; verify needs the first three):
  --collector-kind KIND         workflow collector kind, for example jira
  --external-instance ID[,ID]   collector instance(s) the external producer holds
  --in-tree-instance ID[,ID]    collector instance(s) of the in-tree producer
  --component-id ID             component package ID of the external producer
  --component-version VERSION   component version whose grants are revoked
  --component-home DIR          component registry home the workloads use
  --producer-id ID              grant producer ID (default: the component ID)
  --grant-kind FACT_KIND        granted core fact kind; repeat for each kind
  --grant-scope SCOPE           grant scope, a manifest collector kind
                                (default: the collector kind)

Workloads:
  --namespace NS                Kubernetes namespace of the Eshu release
  --worker-deployment NAME      component-extension worker Deployment
  --coordinator-deployment NAME workflow-coordinator Deployment
  --in-tree-deployment NAME     in-tree collector Deployment to restore
  --in-tree-replicas N          replicas to restore (default 1)
  --worker-selector LABELS      worker pod selector
                                (default app.kubernetes.io/component=component-extension-collector)

Restore (step 4) uses exactly one of:
  --helm-release R --helm-chart C --restore-values FILE
                                helm upgrade --reuse-values with the rehearsed
                                rollback values overlay
  --restore-applied-by-gitops   the same values change was already applied
                                through GitOps AFTER step 3 completed

Run control:
  --yes                         required for run
  --baseline FILE               pre-cutover truth snapshot (required for run)
  --capture FILE                verify only: write a truth snapshot and exit
  --keys-only                   compare kind, count, and stable keys but not payload
  --from-step N                 start at step N (earlier predicates re-checked)
  --through-step N              stop after step N
  --since TIMESTAMP             replay window start; required with --from-step 5 or 6
  --drain-timeout SECONDS       step 2 limit (default 600; env ESHU_ROLLBACK_DRAIN_TIMEOUT)
  --step-timeout SECONDS        step 1 confirmation limit (default 180)
  --replay-timeout SECONDS      step 5 limit (default 1800)
  --poll-seconds SECONDS        poll interval (default 5)
  -h, --help                    show this help

Exit codes: 0 ok, 2 usage, 3 step failed, 4 refused (precondition not met),
5 timeout, 6 truth mismatch, 7 dual authority detected.
USAGE
}

die() {
	local code="$1"
	shift
	printf 'ERROR: %s\n' "$*" >&2
	exit "$code"
}

# --- option state ----------------------------------------------------------

subcommand=""
collector_kind=""
external_instances=""
in_tree_instances=""
component_id=""
component_version=""
component_home=""
producer_id=""
grant_scope=""
grant_kinds=()
namespace=""
worker_deployment=""
coordinator_deployment=""
in_tree_deployment=""
in_tree_replicas="1"
worker_selector="app.kubernetes.io/component=component-extension-collector"
helm_release=""
helm_chart=""
restore_values=""
restore_by_gitops="false"
assume_yes="false"
baseline_file=""
capture_file=""
keys_only="false"
from_step="1"
through_step="6"
since=""
drain_timeout="${ESHU_ROLLBACK_DRAIN_TIMEOUT:-600}"
step_timeout="${ESHU_ROLLBACK_STEP_TIMEOUT:-180}"
replay_timeout="${ESHU_ROLLBACK_REPLAY_TIMEOUT:-1800}"
poll_seconds="${ESHU_ROLLBACK_POLL_SECONDS:-5}"

# need_value fails with a usage error when an option has no value.
need_value() {
	[[ $# -ge 2 && -n "$2" ]] || die "$EX_USAGE" "option $1 needs a value"
}

parse_args() {
	[[ $# -ge 1 ]] || {
		usage >&2
		exit "$EX_USAGE"
	}
	case "$1" in
	-h | --help)
		usage
		exit 0
		;;
	plan | run | verify) subcommand="$1" ;;
	*) die "$EX_USAGE" "unknown subcommand: $1 (want plan, run, or verify)" ;;
	esac
	shift
	while [[ $# -gt 0 ]]; do
		case "$1" in
		-h | --help)
			usage
			exit 0
			;;
		--yes)
			assume_yes="true"
			shift
			continue
			;;
		--keys-only)
			keys_only="true"
			shift
			continue
			;;
		--restore-applied-by-gitops)
			restore_by_gitops="true"
			shift
			continue
			;;
		esac
		need_value "$@"
		case "$1" in
		--collector-kind) collector_kind="$2" ;;
		--external-instance) external_instances="$2" ;;
		--in-tree-instance) in_tree_instances="$2" ;;
		--component-id) component_id="$2" ;;
		--component-version) component_version="$2" ;;
		--component-home) component_home="$2" ;;
		--producer-id) producer_id="$2" ;;
		--grant-kind) grant_kinds+=("$2") ;;
		--grant-scope) grant_scope="$2" ;;
		--namespace) namespace="$2" ;;
		--worker-deployment) worker_deployment="$2" ;;
		--coordinator-deployment) coordinator_deployment="$2" ;;
		--in-tree-deployment) in_tree_deployment="$2" ;;
		--in-tree-replicas) in_tree_replicas="$2" ;;
		--worker-selector) worker_selector="$2" ;;
		--helm-release) helm_release="$2" ;;
		--helm-chart) helm_chart="$2" ;;
		--restore-values) restore_values="$2" ;;
		--baseline) baseline_file="$2" ;;
		--capture) capture_file="$2" ;;
		--from-step) from_step="$2" ;;
		--through-step) through_step="$2" ;;
		--since) since="$2" ;;
		--drain-timeout) drain_timeout="$2" ;;
		--step-timeout) step_timeout="$2" ;;
		--replay-timeout) replay_timeout="$2" ;;
		--poll-seconds) poll_seconds="$2" ;;
		*) die "$EX_USAGE" "unknown option: $1" ;;
		esac
		shift 2
	done
}

# --- validation -------------------------------------------------------------

# check_token rejects values that could change a command or a query. Every
# identifier is also passed to psql as a quoted variable, never interpolated.
check_token() {
	local name="$1" value="$2"
	[[ "$value" =~ ^[A-Za-z0-9][A-Za-z0-9._:@+-]*$ ]] ||
		die "$EX_USAGE" "$name has an unsafe or empty value: '$value'"
}

check_list() {
	local name="$1" value="$2" part
	[[ -n "$value" ]] || die "$EX_USAGE" "$name is required"
	local IFS=,
	for part in $value; do
		check_token "$name" "$part"
	done
}

check_number() {
	local name="$1" value="$2"
	[[ "$value" =~ ^[0-9]+([.][0-9]+)?$ ]] || die "$EX_USAGE" "$name must be a number: '$value'"
}

validate() {
	check_token "--collector-kind" "$collector_kind"
	check_list "--external-instance" "$external_instances"
	check_list "--in-tree-instance" "$in_tree_instances"
	check_number "--poll-seconds" "$poll_seconds"
	check_number "--drain-timeout" "$drain_timeout"
	check_number "--step-timeout" "$step_timeout"
	check_number "--replay-timeout" "$replay_timeout"
	[[ "$from_step" =~ ^[1-6]$ ]] || die "$EX_USAGE" "--from-step must be 1 to 6"
	[[ "$through_step" =~ ^[1-6]$ ]] || die "$EX_USAGE" "--through-step must be 1 to 6"
	((from_step <= through_step)) || die "$EX_USAGE" "--from-step must not exceed --through-step"
	if [[ "$subcommand" == "verify" ]]; then
		return 0
	fi
	check_token "--component-id" "$component_id"
	check_token "--component-version" "$component_version"
	[[ -n "$component_home" ]] || die "$EX_USAGE" "--component-home is required"
	[[ ${#grant_kinds[@]} -gt 0 ]] || die "$EX_USAGE" "at least one --grant-kind is required"
	local kind
	for kind in "${grant_kinds[@]}"; do
		check_token "--grant-kind" "$kind"
	done
	[[ -n "$producer_id" ]] || producer_id="$component_id"
	check_token "--producer-id" "$producer_id"
	[[ -n "$grant_scope" ]] || grant_scope="$collector_kind"
	check_token "--grant-scope" "$grant_scope"
	check_token "--namespace" "$namespace"
	check_token "--worker-deployment" "$worker_deployment"
	check_token "--coordinator-deployment" "$coordinator_deployment"
	check_token "--in-tree-deployment" "$in_tree_deployment"
	[[ "$in_tree_replicas" =~ ^[1-9][0-9]*$ ]] || die "$EX_USAGE" "--in-tree-replicas must be a positive integer"
	[[ "$worker_selector" =~ ^[A-Za-z0-9._/=,!-]+$ ]] || die "$EX_USAGE" "--worker-selector is not a label selector"
	if [[ "$restore_by_gitops" == "true" ]]; then
		[[ -z "$helm_release$helm_chart$restore_values" ]] ||
			die "$EX_USAGE" "--restore-applied-by-gitops excludes the helm restore options"
	else
		check_token "--helm-release" "$helm_release"
		[[ -n "$helm_chart" ]] || die "$EX_USAGE" "--helm-chart is required for the helm restore"
		[[ -n "$restore_values" ]] || die "$EX_USAGE" "--restore-values is required for the helm restore"
	fi
}

# --- command builders --------------------------------------------------------
# One argv builder per command. `plan` prints these and `run` executes them, so
# the printed plan cannot drift from what runs. Each sets the global CMD array.

CMD=()

build_disable() { # instance
	CMD=("$ESHU_BIN" component disable "$component_id" --instance "$1" --component-home "$component_home")
}
build_scale_worker_zero() {
	CMD=("$KUBECTL_BIN" -n "$namespace" scale "deployment/${worker_deployment}" --replicas=0)
}
build_restart_coordinator() {
	CMD=("$KUBECTL_BIN" -n "$namespace" rollout restart "deployment/${coordinator_deployment}")
}
build_coordinator_status() {
	CMD=("$KUBECTL_BIN" -n "$namespace" rollout status "deployment/${coordinator_deployment}" "--timeout=${step_timeout}s")
}
build_worker_pods() {
	CMD=("$KUBECTL_BIN" -n "$namespace" get pods -l "$worker_selector" -o name)
}
build_revoke() { # fact kind
	CMD=("$ESHU_BIN" component revoke-grant "$producer_id" --version "$component_version"
		--kind "$1" --scope "$grant_scope" --component-home "$component_home")
}
build_grants() {
	CMD=("$ESHU_BIN" component grants --producer "$producer_id" --component-home "$component_home")
}
build_helm_restore() {
	CMD=("$HELM_BIN" upgrade "$helm_release" "$helm_chart" --namespace "$namespace" --reuse-values -f "$restore_values")
}
build_scale_in_tree() {
	CMD=("$KUBECTL_BIN" -n "$namespace" scale "deployment/${in_tree_deployment}" "--replicas=${in_tree_replicas}")
}
build_in_tree_status() {
	CMD=("$KUBECTL_BIN" -n "$namespace" rollout status "deployment/${in_tree_deployment}" "--timeout=${step_timeout}s")
}

# show_cmd prints CMD in copy-pasteable form.
show_cmd() {
	local out="" word
	for word in "${CMD[@]}"; do
		out+="$(printf '%q ' "$word")"
	done
	printf '  $ %s\n' "${out% }"
}

# --- plan ---------------------------------------------------------------------

plan_psql_line() { # description
	printf '  $ %s -X -A -t -F "|" -v ON_ERROR_STOP=1 -v kind=%s ... -f -   # %s (read-only SQL on stdin)\n' \
		"$PSQL_BIN" "$collector_kind" "$1"
}

print_plan() {
	local instance kind
	printf 'Rollback plan for collector kind %s\n' "$collector_kind"
	printf 'External instance(s): %s   In-tree instance(s): %s\n' "$external_instances" "$in_tree_instances"
	printf 'Nothing below has been executed. `run --yes` performs it in this order.\n\n'

	printf 'STEP 1 stop new claims for the external producer\n'
	local -a externals
	IFS=, read -r -a externals <<<"$external_instances"
	for instance in "${externals[@]}"; do
		build_disable "$instance"
		show_cmd
	done
	build_scale_worker_zero
	show_cmd
	build_restart_coordinator
	show_cmd
	build_coordinator_status
	show_cmd
	build_worker_pods
	show_cmd
	printf '  Gate: no worker pod remains and the coordinator rollout is complete.\n'
	printf '  Record: disable result per instance, worker pod count 0, coordinator rollout line.\n\n'

	printf 'STEP 2 wait for in-flight leases to drain (refuses to start before step 1 is confirmed)\n'
	plan_psql_line "claimed work items and active claims for the external instance(s)"
	printf '  Gate: 0 claimed work items and 0 active claims. The coordinator reaper expires stale\n'
	printf '  leases on its own interval; this step only waits. It never rewrites claim rows.\n'
	printf '  Record: final counts and the seconds waited.\n\n'

	printf 'STEP 3 revoke the external producer grant (refuses to start before step 2 drained)\n'
	for kind in "${grant_kinds[@]}"; do
		build_revoke "$kind"
		show_cmd
	done
	build_grants
	show_cmd
	printf '  Gate: every grant for %s@%s shows revoked=true.\n' "$producer_id" "$component_version"
	printf '  Record: the grants listing after revocation.\n\n'

	printf 'STEP 4 restore the in-tree producer (refuses unless steps 1 to 3 still hold)\n'
	plan_psql_line "re-check the drain gate immediately before restoring"
	if [[ "$restore_by_gitops" == "true" ]]; then
		printf '  (restore values were applied through GitOps after step 3; the script only scales and waits)\n'
	else
		build_helm_restore
		show_cmd
	fi
	build_scale_in_tree
	show_cmd
	build_in_tree_status
	show_cmd
	build_restart_coordinator
	show_cmd
	build_coordinator_status
	show_cmd
	printf '  Record: restore start time (database clock), helm revision or GitOps commit, rollout lines.\n\n'

	printf 'STEP 5 replay from the last committed generation\n'
	plan_psql_line "work items for the in-tree instance(s) updated since the restore start"
	printf '  The restart in step 4 triggers the first planning pass; later passes follow the\n'
	printf '  coordinator reconcile interval. Gate: no pending, claimed, or retryable item remains,\n'
	printf '  at least one completed, none failed terminal.\n'
	printf '  Record: pending/claimed/completed/retryable/terminal counts.\n\n'

	printf 'STEP 6 verify exact truth against the pre-cutover baseline\n'
	plan_psql_line "dual-authority check: external claims, in-tree claims, overlapping claim intervals"
	plan_psql_line "per fact kind: count, stable-key digest, payload digest over active generations"
	printf '  Gate: no dual authority, and every fact kind equals the baseline file.\n'
	printf '  Then compare the API and MCP read-truth queries in the runbook.\n'
	printf '  Record: the verify output and the read-truth comparison.\n'
}

main() {
	parse_args "$@"
	validate
	case "$subcommand" in
	plan) print_plan ;;
	run | verify)
		# shellcheck source=lib/collector-cutover-rollback-steps.sh
		source "${script_dir}/lib/collector-cutover-rollback-steps.sh"
		"cmd_${subcommand}"
		;;
	esac
}

main "$@"
