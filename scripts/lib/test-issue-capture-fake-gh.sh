#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Fake `gh` for scripts/test-issue-capture-hook.sh. Serves the two read calls
# the issue-ledger hooks make (`gh issue view N -R owner/repo --json
# createdAt|state`) from files in $FIXTURES and refuses anything else, so a new
# unfixtured or writing call fails the suite instead of passing silently. Every
# call's argv is appended to $FIXTURES/calls.log.
#
# Knobs, all files under $FIXTURES:
#   mode            "fail" exits 1 (gh unreachable), "hang" blocks past any
#                   sane timeout, anything else (or absent) serves normally.
#   created-<N>     ISO timestamp for createdAt; default is "now".
#   state-<N>       OPEN or CLOSED for state; default OPEN.
set -uo pipefail
printf '%s\n' "$*" >>"${FIXTURES}/calls.log"
mode="$(cat "${FIXTURES}/mode" 2>/dev/null || printf ok)"
case "${mode}" in
fail)
	echo "fake gh: simulated failure" >&2
	exit 1
	;;
hang)
	# exec, so the sleep IS this process: a killed fake leaves no grandchild
	# holding the caller's pipe open past its timeout.
	exec sleep 30
	;;
esac
if [[ "${1:-} ${2:-}" != "issue view" ]]; then
	echo "fake gh: unsupported: $*" >&2
	exit 2
fi
n="${3:-}"
field=""
prev=""
for arg in "$@"; do
	[[ "${prev}" == --json ]] && field="${arg}"
	prev="${arg}"
done
case "${field}" in
createdAt)
	created="$(cat "${FIXTURES}/created-${n}" 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ)"
	printf '{"createdAt":"%s"}\n' "${created}"
	;;
state)
	state="$(cat "${FIXTURES}/state-${n}" 2>/dev/null || printf OPEN)"
	printf '{"state":"%s"}\n' "${state}"
	;;
*)
	echo "fake gh: unsupported --json field: ${field}" >&2
	exit 2
	;;
esac
