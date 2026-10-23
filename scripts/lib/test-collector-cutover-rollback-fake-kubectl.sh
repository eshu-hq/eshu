#!/usr/bin/env bash
# Fake `kubectl` for test-collector-cutover-rollback.sh. State lives in
# $FAKE_STATE: worker_pods (count), intree_replicas (count). Marker files make
# a scenario fail: worker_stuck (pods never terminate), rollout_fail.
set -euo pipefail

state="${FAKE_STATE:?FAKE_STATE is required}"
printf 'kubectl %s\n' "$*" >>"${state}/calls.log"

if [[ "${1:-}" == "-n" ]]; then
	shift 2
fi

case "${1:-}" in
scale)
	target="${2#deployment/}"
	replicas="${3#--replicas=}"
	case "${target}" in
	*worker*)
		if [[ ! -e "${state}/worker_stuck" ]]; then
			printf '%s\n' "${replicas}" >"${state}/worker_pods"
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
		printf 'deployment "%s" successfully rolled out\n' "${target}"
	else
		printf 'deployment.apps/%s restarted\n' "${target}"
	fi
	;;
get)
	count="$(cat "${state}/worker_pods")"
	i=0
	while ((i < count)); do
		printf 'pod/worker-%d\n' "${i}"
		i=$((i + 1))
	done
	;;
*)
	printf 'fake kubectl: unsupported command %s\n' "$*" >&2
	exit 64
	;;
esac
