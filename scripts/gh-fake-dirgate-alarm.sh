#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Fake gh(1) for test-dirgate-main-alarm.sh: serves case fixtures by URL
# fragment and records mutating calls. Split out of the test so no heredoc
# exceeds the 512-byte budget.
set -u
dir="${DIRGATE_ALARM_CASE:?}"
if [ "${1:-}" != "api" ]; then
	echo "fake gh: only 'api' is implemented, got: $*" >&2
	exit 2
fi
shift
if [ "${1:-}" = "-X" ]; then
	shift 2
fi
path="$1"
shift
body=""
while [ $# -gt 0 ]; do
	case "$1" in
	-f|--field)
		body="${body}${2}
"
		shift 2
		;;
		*)
		shift
		;;
	esac
done
mutate() {
	local out="${dir}/payload-$1.txt"
	printf '%s' "${body}" >"${out}"
	printf 'POST %s || %s\n' "${path}" "${out}" >>"${dir}/calls.log"
	printf '{}\n'
}
case "${path}" in
	*/jobs\?*|*/jobs)
		cat "${dir}/jobs.json"
		;;
	*/jobs/*/logs)
		cat "${dir}/job.log"
		;;
	*/commits/*/pulls*)
		cat "${dir}/prs.json"
		;;
	*/issues/*/comments*)
		if printf '%s' "${body}" | rg -q '^body='; then
			mutate "comment"
		else
			cat "${dir}/comments.json"
		fi
		;;
	*/labels)
		name="$(printf '%s' "${body}" | rg '^name=' | head -1 | cut -d= -f2-)"
		printf '%s\n' "${name}" >>"${dir}/labels-known.txt"
		mutate "label"
		;;
	*/issues\?*|*/issues)
		if printf '%s' "${body}" | rg -q '^title='; then
			for want in $(printf '%s' "${body}" | rg '^labels\[\]=' | cut -d= -f2-); do
				if ! rg -q -x -e "${want}" "${dir}/labels-known.txt" 2>/dev/null; then
					echo "fake gh: 422 unknown label: ${want}" >&2
					exit 1
				fi
			done
			mutate "issue"
		else
			cat "${dir}/issues.json"
		fi
		;;
	*)
		echo "fake gh: unexpected path: ${path}" >&2
		exit 2
		;;
esac
