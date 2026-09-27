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
	serve checks.json
	# gh pr checks exits 8 while checks pend; the script must ignore it.
	exit "$(cat "${FIXTURES}/checks.rc" 2>/dev/null || echo 0)"
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
"api repos/"*)
	[[ "$2" == */commits/*/status* ]] || { echo "fake gh: unsupported api: $2" >&2; exit 2; }
	serve status.json
	;;
*)
	echo "fake gh: unsupported: $*" >&2
	exit 2
	;;
esac
