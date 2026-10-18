#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Fake `gh` for scripts/dev/test-pre-enqueue-check.sh. Serves the JSON
# fixtures in $FIXTURES for the read calls scripts/dev/pre-enqueue-check.sh
# makes, and refuses anything else so a new, unfixtured (or writing) call
# fails the self-test instead of passing silently. Every call's argv is
# appended to $FIXTURES/calls.log.
set -euo pipefail
printf '%s\n' "$*" >>"${FIXTURES}/calls.log"
serve() {
	cat "${FIXTURES}/$1" 2>/dev/null || {
		echo "fake gh: no fixture $1 for: $*" >&2
		exit 1
	}
}
case "$1 ${2:-}" in
"pr view")
	if [[ "$3" == "${FAKE_PR}" ]]; then serve pr.json; else serve "files-$3.json"; fi
	;;
"pr checks")
	# gh 2.46.0 (this host) has no --json on `gh pr checks`; the script must
	# read check rows another way (#8381).
	for arg in "$@"; do
		if [[ "${arg}" == --json* ]]; then
			echo "unknown flag: --json" >&2
			exit 1
		fi
	done
	echo "fake gh: pr checks is not fixtured; read check-runs through gh api" >&2
	exit 2
	;;
"api graphql")
	query=""
	for arg in "$@"; do [[ "${arg}" == query=* ]] && query="${arg}"; done
	case "${query}" in
	*mergeQueue*) serve queue.json ;;
	*reviewThreads*) serve threads.json ;;
	*) echo "fake gh: unsupported graphql: ${query}" >&2; exit 2 ;;
	esac
	;;
"api "*)
	# Real gh applies --jq to every page of a --paginate read, so the fixture is
	# a stream of page objects and the filter runs over each one.
	path="" jqf="" prev=""
	for arg in "$@"; do
		[[ "${prev}" == --jq ]] && jqf="${arg}"
		[[ "${arg}" == repos/* ]] && path="${arg}"
		prev="${arg}"
	done
	case "${path}" in
	repos/*/commits/*/status*) serve status.json ;;
	repos/*/commits/*/check-runs*)
		if [[ -f "${FIXTURES}/check-runs.rc" ]]; then
			echo "fake gh: simulated check-runs failure" >&2
			exit "$(cat "${FIXTURES}/check-runs.rc")"
		fi
		if [[ -n "${jqf}" ]]; then serve check-runs.json | jq -c "${jqf}"; else serve check-runs.json; fi
		;;
	*)
		echo "fake gh: unsupported api: $*" >&2
		exit 2
		;;
	esac
	;;
*)
	echo "fake gh: unsupported: $*" >&2
	exit 2
	;;
esac
