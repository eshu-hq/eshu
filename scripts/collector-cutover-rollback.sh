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
#   ROLLBACK_ESHU_BIN (eshu)  ROLLBACK_KUBECTL_BIN (kubectl)  ROLLBACK_PSQL_BIN (psql)  ROLLBACK_HELM_BIN (helm)
# psql reads its connection from the standard libpq environment (PGHOST,
# PGUSER, PGDATABASE, PGPASSFILE, ...). No secret is ever passed on a command
# line or printed. Hung calls are bounded: PGCONNECT_TIMEOUT (default 10 seconds)
# for the database connection and ROLLBACK_KUBECTL_REQUEST_TIMEOUT (default 30s,
# 0 disables it) for each kubectl request. ROLLBACK_CLOCK_SKEW_SECONDS (default
# 30) widens the replay window to absorb a slow coordinator clock.

# The exit codes and option variables below are consumed by the sourced step
# library, which shellcheck cannot see when this file is linted alone.
# shellcheck disable=SC2034
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

ROLLBACK_ESHU_BIN="${ROLLBACK_ESHU_BIN:-eshu}"
ROLLBACK_KUBECTL_BIN="${ROLLBACK_KUBECTL_BIN:-kubectl}"
ROLLBACK_PSQL_BIN="${ROLLBACK_PSQL_BIN:-psql}"
ROLLBACK_HELM_BIN="${ROLLBACK_HELM_BIN:-helm}"
ROLLBACK_KUBECTL_REQUEST_TIMEOUT="${ROLLBACK_KUBECTL_REQUEST_TIMEOUT:-30s}"
ROLLBACK_CLOCK_SKEW_SECONDS="${ROLLBACK_CLOCK_SKEW_SECONDS:-30}"
# A libpq connect that hangs would outlive every wait_until deadline.
export PGCONNECT_TIMEOUT="${PGCONNECT_TIMEOUT:-10}"

# A baseline and its key sidecar hold stable fact keys (issue, incident, and
# document IDs), so every file this script writes stays private to the operator.
umask 077

# mk_tmp makes a temp file (and its .err/.view companions) that is removed on
# every exit path, including a refusal and a signal, so a killed run leaves no
# raw key list in TMPDIR.
TMP_FILES=()
mk_tmp() { # varname
	local file
	# An explicit template: BSD mktemp ignores TMPDIR without one.
	file="$(mktemp "${TMPDIR:-/tmp}/eshu-rollback.XXXXXX")"
	TMP_FILES+=("$file" "${file}.err" "${file}.view")
	printf -v "$1" '%s' "$file"
}
cleanup_tmp() {
	if ((${#TMP_FILES[@]} > 0)); then rm -f "${TMP_FILES[@]}"; fi
}
trap cleanup_tmp EXIT

# Exit codes. Distinct codes let a wrapper tell a refusal from a failure.
readonly EX_USAGE=2
readonly EX_STEP_FAILED=3
readonly EX_REFUSED=4
readonly EX_TIMEOUT=5
readonly EX_TRUTH_MISMATCH=6
readonly EX_DUAL_AUTHORITY=7

usage() {
	cat "${script_dir}/lib/collector-cutover-rollback-usage.txt"
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
capture_keys="false"
report_drift="false"
drift_keys="20"
from_step="1"
through_step="6"
since=""
drain_timeout="${ROLLBACK_DRAIN_TIMEOUT:-600}"
step_timeout="${ROLLBACK_STEP_TIMEOUT:-180}"
replay_timeout="${ROLLBACK_REPLAY_TIMEOUT:-1800}"
poll_seconds="${ROLLBACK_POLL_SECONDS:-5}"

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
		--capture-keys)
			capture_keys="true"
			shift
			continue
			;;
		--report-drift)
			report_drift="true"
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
		--drift-keys) drift_keys="$2" ;;
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

# check_list validates a comma-separated ID list. It splits with read, never an
# unquoted expansion: a value such as '*' must not glob against the working
# directory and turn into file names that pass the token check.
check_list() {
	local name="$1" value="$2" part
	local -a parts
	[[ -n "$value" ]] || die "$EX_USAGE" "$name is required"
	# read -r -a reads one line, so a newline would hide every later entry from the
	# token check while the SQL still splits the whole value on commas.
	[[ "$value" != *[[:cntrl:]]* ]] ||
		die "$EX_USAGE" "$name must not contain control characters or newlines"
	[[ "$value" != ,* && "$value" != *, && "$value" != *,,* ]] ||
		die "$EX_USAGE" "$name has an empty entry: '$value'"
	IFS=, read -r -a parts <<<"$value"
	for part in "${parts[@]}"; do
		check_token "$name" "$part"
	done
}

# check_disjoint rejects an ID named as both external and in-tree: the drain and
# authority checks treat everything outside the in-tree list as external.
check_disjoint() {
	local external in_tree
	local -a externals trees
	IFS=, read -r -a externals <<<"$external_instances"
	IFS=, read -r -a trees <<<"$in_tree_instances"
	for external in "${externals[@]}"; do
		for in_tree in "${trees[@]}"; do
			[[ "$external" != "$in_tree" ]] ||
				die "$EX_USAGE" "instance '${external}' is named as both external and in-tree"
		done
	done
}

check_number() {
	local name="$1" value="$2"
	[[ "$value" =~ ^[0-9]+([.][0-9]+)?$ ]] || die "$EX_USAGE" "$name must be a number: '$value'"
}

check_seconds() {
	local name="$1" value="$2"
	[[ "$value" =~ ^[0-9]+$ ]] || die "$EX_USAGE" "$name must be whole seconds: '$value'"
}

validate() {
	check_token "--collector-kind" "$collector_kind"
	check_list "--external-instance" "$external_instances"
	check_list "--in-tree-instance" "$in_tree_instances"
	check_disjoint
	check_number "--poll-seconds" "$poll_seconds"
	check_seconds "--drift-keys" "$drift_keys"
	check_seconds "ROLLBACK_CLOCK_SKEW_SECONDS" "$ROLLBACK_CLOCK_SKEW_SECONDS"
	[[ "$ROLLBACK_KUBECTL_REQUEST_TIMEOUT" =~ ^[0-9]+(ms|s|m|h)?$ ]] ||
		die "$EX_USAGE" "ROLLBACK_KUBECTL_REQUEST_TIMEOUT must look like 30s or 0: '$ROLLBACK_KUBECTL_REQUEST_TIMEOUT'"
	[[ "$capture_keys" == "false" || -n "$capture_file" ]] || die "$EX_USAGE" "--capture-keys needs --capture FILE"
	check_seconds "--drain-timeout" "$drain_timeout"
	check_seconds "--step-timeout" "$step_timeout"
	check_seconds "--replay-timeout" "$replay_timeout"
	[[ -z "$since" || "$since" =~ ^[0-9A-Za-z:+.\ -]+$ ]] || die "$EX_USAGE" "--since is not a timestamp: '$since'"
	[[ "$from_step" =~ ^[1-6]$ ]] || die "$EX_USAGE" "--from-step must be 1 to 6"
	[[ "$through_step" =~ ^[1-6]$ ]] || die "$EX_USAGE" "--through-step must be 1 to 6"
	((from_step <= through_step)) || die "$EX_USAGE" "--from-step must not exceed --through-step"
	if [[ "$subcommand" == "verify" ]]; then
		# verify's cluster and registry checks are optional; check what was given.
		local optional
		for optional in namespace worker_deployment component_id component_version; do
			[[ -z "${!optional}" ]] || check_token "--${optional//_/-}" "${!optional}"
		done
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
	CMD=("$ROLLBACK_ESHU_BIN" component disable "$component_id" --instance "$1" --component-home "$component_home")
}
build_scale_worker_zero() {
	CMD=("$ROLLBACK_KUBECTL_BIN" -n "$namespace" scale "deployment/${worker_deployment}" --replicas=0
		"--request-timeout=${ROLLBACK_KUBECTL_REQUEST_TIMEOUT}")
}
build_restart_coordinator() {
	CMD=("$ROLLBACK_KUBECTL_BIN" -n "$namespace" rollout restart "deployment/${coordinator_deployment}"
		"--request-timeout=${ROLLBACK_KUBECTL_REQUEST_TIMEOUT}")
}
build_coordinator_status() {
	CMD=("$ROLLBACK_KUBECTL_BIN" -n "$namespace" rollout status "deployment/${coordinator_deployment}" "--timeout=${step_timeout}s")
}
build_worker_pods() {
	CMD=("$ROLLBACK_KUBECTL_BIN" -n "$namespace" get pods -l "$worker_selector" -o name
		"--request-timeout=${ROLLBACK_KUBECTL_REQUEST_TIMEOUT}")
}
# build_worker_replicas reads the worker Deployment's desired replica count. A
# selector that matches no pod cannot prove the worker is stopped; this can.
build_worker_replicas() {
	CMD=("$ROLLBACK_KUBECTL_BIN" -n "$namespace" get "deployment/${worker_deployment}" -o 'jsonpath={.spec.replicas}'
		"--request-timeout=${ROLLBACK_KUBECTL_REQUEST_TIMEOUT}")
}
# build_component_list reads the registry the coordinator and worker start from.
build_component_list() {
	CMD=("$ROLLBACK_ESHU_BIN" component list --component-home "$component_home")
}
build_revoke() { # fact kind
	CMD=("$ROLLBACK_ESHU_BIN" component revoke-grant "$producer_id" --version "$component_version"
		--kind "$1" --scope "$grant_scope" --component-home "$component_home")
}
build_grants() {
	CMD=("$ROLLBACK_ESHU_BIN" component grants --producer "$producer_id" --component-home "$component_home")
}
build_helm_restore() {
	CMD=("$ROLLBACK_HELM_BIN" upgrade "$helm_release" "$helm_chart" --namespace "$namespace" --reuse-values -f "$restore_values")
}
build_scale_in_tree() {
	CMD=("$ROLLBACK_KUBECTL_BIN" -n "$namespace" scale "deployment/${in_tree_deployment}" "--replicas=${in_tree_replicas}"
		"--request-timeout=${ROLLBACK_KUBECTL_REQUEST_TIMEOUT}")
}
build_in_tree_status() {
	CMD=("$ROLLBACK_KUBECTL_BIN" -n "$namespace" rollout status "deployment/${in_tree_deployment}" "--timeout=${step_timeout}s")
}

# show_cmd prints CMD in copy-pasteable form.
show_cmd() {
	local out="" word
	for word in "${CMD[@]}"; do
		out+="$(printf '%q ' "$word")"
	done
	printf '  $ %s\n' "${out% }"
}

sql_file() { printf '%s/lib/collector-cutover-rollback-%s.sql' "$script_dir" "$1"; }

main() {
	parse_args "$@"
	validate
	case "$subcommand" in
	plan)
		# shellcheck source=scripts/lib/collector-cutover-rollback-plan.sh
		source "${script_dir}/lib/collector-cutover-rollback-plan.sh"
		print_plan
		;;
	run | verify)
		# shellcheck source=scripts/lib/collector-cutover-rollback-steps.sh
		source "${script_dir}/lib/collector-cutover-rollback-steps.sh"
		"cmd_${subcommand}"
		;;
	esac
}

main "$@"
