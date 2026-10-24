#!/usr/bin/env bash
# Fake `kubectl` for test-collector-cutover-rollback.sh. State lives in
# $FAKE_STATE: worker_pods (count), worker_replicas (count), intree_replicas
# (count). Marker files make a scenario fail:
#   worker_stuck           the worker never scales down (pods and replicas stay)
#   worker_absent          the worker Deployment does not exist (NotFound)
#   scale_fail_worker      scaling the worker fails with a Forbidden error
#   rollout_fail           every `rollout status` fails
#   intree_rollout_fail    `rollout status` fails for every Deployment but the coordinator
#   kubectl_fail_get       `get pods` fails with a server error
#   kubectl_fail_get_deploy  `get deployment/...` fails with a server error
#   kubectl_cmd_missing    `get deployment/...` fails as a missing binary would
set -euo pipefail

state="${FAKE_STATE:?FAKE_STATE is required}"
printf 'kubectl %s\n' "$*" >>"${state}/calls.log"

if [[ "${1:-}" == "-n" ]]; then
	shift 2
fi

not_found() {
	printf 'Error from server (NotFound): deployments.apps "%s" not found\n' "$1" >&2
	exit 1
}

case "${1:-}" in
scale)
	target="${2#deployment/}"
	replicas="${3#--replicas=}"
	case "${target}" in
	*worker*)
		[[ ! -e "${state}/worker_absent" ]] || not_found "${target}"
		if [[ -e "${state}/scale_fail_worker" ]]; then
			printf 'Error from server (Forbidden): deployments.apps "%s" is forbidden\n' "${target}" >&2
			exit 1
		fi
		if [[ ! -e "${state}/worker_stuck" ]]; then
			printf '%s\n' "${replicas}" >"${state}/worker_pods"
			printf '%s\n' "${replicas}" >"${state}/worker_replicas"
		fi
		;;
	*)
		printf '%s\n' "${replicas}" >"${state}/intree_replicas"
		;;
	esac
	printf 'deployment.apps/%s scaled\n' "${target}"
	;;
rollout)
	target="${3#deployment/}"
	if [[ "${2}" == "status" ]]; then
		if [[ -e "${state}/rollout_fail" ]]; then
			printf 'error: timed out waiting for the condition\n' >&2
			exit 1
		fi
		if [[ -e "${state}/intree_rollout_fail" && "${target}" != *coordinator* ]]; then
			printf 'error: timed out waiting for the condition\n' >&2
			exit 1
		fi
		printf 'deployment "%s" successfully rolled out\n' "${target}"
	else
		printf 'deployment.apps/%s restarted\n' "${target}"
	fi
	;;
get)
	case "${2:-}" in
	pods)
		if [[ -e "${state}/kubectl_fail_get" ]]; then
			printf 'Error from server (InternalError): etcdserver: request timed out\n' >&2
			exit 1
		fi
		count="$(cat "${state}/worker_pods")"
		# Real kubectl may print this notice on stderr when nothing matches.
		((count > 0)) || printf 'No resources found in default namespace.\n' >&2
		i=0
		while ((i < count)); do
			printf 'pod/worker-%d\n' "${i}"
			i=$((i + 1))
		done
		;;
	deployment/*)
		target="${2#deployment/}"
		[[ ! -e "${state}/worker_absent" ]] || not_found "${target}"
		if [[ -e "${state}/kubectl_cmd_missing" ]]; then
			printf 'kubectl: command not found\n' >&2
			exit 127
		fi
		if [[ -e "${state}/kubectl_fail_get_deploy" ]]; then
			printf 'Error from server (InternalError): etcdserver: request timed out\n' >&2
			exit 1
		fi
		cat "${state}/worker_replicas"
		;;
	*)
		printf 'fake kubectl: unsupported get %s\n' "$*" >&2
		exit 64
		;;
	esac
	;;
*)
	printf 'fake kubectl: unsupported command %s\n' "$*" >&2
	exit 64
	;;
esac
