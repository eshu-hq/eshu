#!/usr/bin/env bash
# Fake `psql` for test-collector-cutover-rollback.sh. It reads the SQL from
# stdin, finds the "-- eshu-rollback:<tag>" first line, and answers from files
# in $FAKE_STATE:
#   claims              kind|instance|item_status|claim_status (the fake claim table)
#   drain_polls_left    after this many drain polls the claims file is emptied
#   dual                ext_claimed|tree_claimed|overlaps (default 0|0|0)
#   replay              pending|claimed|completed|retryable|terminal
#   truth               the current per-kind truth rows
set -euo pipefail

state="${FAKE_STATE:?FAKE_STATE is required}"
kind="" instances=""
while [[ $# -gt 0 ]]; do
	case "$1" in
	-v)
		case "$2" in
		kind=*) kind="${2#kind=}" ;;
		instances=*) instances="${2#instances=}" ;;
		esac
		shift 2
		;;
	*) shift ;;
	esac
done

query="$(cat)"
tag="$(printf '%s\n' "${query}" | awk '/^-- eshu-rollback:/ { sub(/^-- eshu-rollback:/, ""); print; exit }')"
printf 'psql %s kind=%s\n' "${tag}" "${kind}" >>"${state}/calls.log"

case "${tag}" in
drain)
	if [[ -f "${state}/drain_polls_left" ]]; then
		left="$(cat "${state}/drain_polls_left")"
		if ((left <= 0)); then
			: >"${state}/claims"
		else
			printf '%s\n' "$((left - 1))" >"${state}/drain_polls_left"
		fi
	fi
	awk -F'|' -v kind="${kind}" -v instances="${instances}" '
		BEGIN { n = split(instances, list, ","); for (i = 1; i <= n; i++) want[list[i]] = 1 }
		$1 == kind && ($2 in want) {
			if ($3 == "claimed") claimed++
			if ($4 == "active") active++
		}
		END { printf "%d|%d\n", claimed, active }' "${state}/claims"
	;;
dual)
	cat "${state}/dual" 2>/dev/null || printf '0|0|0\n'
	;;
now)
	printf '2026-10-23 12:00:00+00\n'
	;;
replay)
	cat "${state}/replay"
	;;
truth)
	cat "${state}/truth"
	;;
*)
	printf 'fake psql: unknown query tag "%s"\n' "${tag}" >&2
	exit 64
	;;
esac
