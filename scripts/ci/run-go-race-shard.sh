#!/usr/bin/env bash
# Run Go tests with the race detector in either scoped changed-tests mode
# (single runner for go/**/*_test.go-only PRs) or full sharded mode.
set -euo pipefail

event_name="${GITHUB_EVENT_NAME:-}"
scope="${GO_RACE_SCOPE:-full}"
packages_str="${GO_RACE_PACKAGES:-}"

if [[ "${event_name}" == "pull_request" && "${scope}" == "changed-tests" ]]; then
	if [[ -z "${packages_str}" ]]; then
		printf 'go-race: scope=changed-tests requires non-empty GO_RACE_PACKAGES\n' >&2
		exit 1
	fi
	read -r -a pkgs < <(printf '%s\n' "${packages_str}")
	if [[ "${#pkgs[@]}" -eq 0 ]]; then
		printf 'go-race: scope=changed-tests parsed zero packages\n' >&2
		exit 1
	fi
	for pkg in "${pkgs[@]}"; do
		if [[ "${pkg}" != "." && ! "${pkg}" =~ ^\./[A-Za-z0-9._/-]+$ ]]; then
			printf 'go-race: refusing unsafe package selector %q\n' "${pkg}" >&2
			exit 1
		fi
		case "${pkg}/" in
		*/../*)
			printf 'go-race: refusing parent traversal selector %q\n' "${pkg}" >&2
			exit 1
			;;
		esac
	done
	printf 'go-race (changed-tests): %d package(s): %s\n' "${#pkgs[@]}" "${pkgs[*]}"
	go test -count=1 -race -timeout 900s -p 2 "${pkgs[@]}"
	exit 0
fi

shard_index="${SHARD_INDEX:?SHARD_INDEX is required in full mode}"
shard_total="${SHARD_TOTAL:?SHARD_TOTAL is required in full mode}"

shard_pkgs="$(go list ./... | awk -v n="${shard_total}" -v k="${shard_index}" 'NR % n == (k % n)')"
if [[ -z "${shard_pkgs}" ]]; then
	printf 'shard %s/%s: no packages selected — nothing to test\n' "${shard_index}" "${shard_total}"
	exit 0
fi
count="$(printf '%s\n' "${shard_pkgs}" | wc -l | tr -d ' ')"
printf 'shard %s/%s: %s package(s)\n' "${shard_index}" "${shard_total}" "${count}"
printf '%s\n' "${shard_pkgs}" | xargs go test -count=1 -race -timeout 900s -p 2
