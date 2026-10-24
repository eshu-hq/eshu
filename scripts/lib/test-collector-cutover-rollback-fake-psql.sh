#!/usr/bin/env bash
# Fake `psql` for test-collector-cutover-rollback.sh. It reads the SQL file
# named by -f, finds the "-- eshu-rollback:<tag>" first line, and answers from
# files in $FAKE_STATE:
#   claims             kind|instance|item_status|claim_status (the fake claim table)
#   drain_polls_left   after this many drain polls the claims file is emptied
#   overlaps           overlapping claim pairs for the dual query (default 0)
#   replay             pending|claimed|completed|retryable|terminal
#   truth              the current per-kind truth rows
#   keys               the current per-fact key lines
# The drain and dual answers are computed from the claims file, from the SQL's
# own contract: any instance of the kind outside the in_tree list is external.
#
# The fake also enforces the interface the real SQL has: every :'name' the SQL
# references must be passed with -v, and every -v passed must be referenced. Real
# psql leaves an undefined variable as literal text and fails with a syntax error,
# so a rename on one side only is caught here. Marker files inject faults:
#   psql_fail_<tag>      psql exits 2 with a connection error
#   psql_garbage_<tag>   psql prints text that is not the documented row shape
set -euo pipefail

state="${FAKE_STATE:?FAKE_STATE is required}"
names=()
values=()
sql_path="-"
while [[ $# -gt 0 ]]; do
	case "$1" in
	-f)
		sql_path="$2"
		shift 2
		;;
	-v)
		# ON_ERROR_STOP configures psql itself; every other -v feeds the SQL.
		if [[ "${2%%=*}" != "ON_ERROR_STOP" ]]; then
			names+=("${2%%=*}")
			values+=("${2#*=}")
		fi
		shift 2
		;;
	*) shift ;;
	esac
done

var() { # name
	local i
	for i in "${!names[@]}"; do
		if [[ "${names[$i]}" == "$1" ]]; then
			printf '%s' "${values[$i]}"
			return 0
		fi
	done
}

if [[ "${sql_path}" == "-" ]]; then query="$(cat)"; else query="$(cat "${sql_path}")"; fi
tag="$(printf '%s\n' "${query}" | awk '/^-- eshu-rollback:/ { sub(/^-- eshu-rollback:/, ""); print; exit }')"
kind="$(var kind)"
extra=""
[[ "${tag}" != "now" ]] || extra=" skew=$(var skew)"
printf 'psql %s kind=%s%s connect_timeout=%s\n' "${tag}" "${kind}" "${extra}" "${PGCONNECT_TIMEOUT:-unset}" >>"${state}/calls.log"

referenced="$(printf '%s\n' "${query}" | awk '
	/^[[:space:]]*--/ { next }
	{
		line = $0
		while (match(line, /:'"'"'[A-Za-z_][A-Za-z0-9_]*'"'"'/)) {
			print substr(line, RSTART + 2, RLENGTH - 3)
			line = substr(line, RSTART + RLENGTH)
		}
	}' | sort -u)"
passed="$(printf '%s\n' "${names[@]}" | sort -u)"
if [[ "${referenced}" != "${passed}" ]]; then
	printf 'fake psql: the SQL references variables [%s] but the caller passed [%s]\n' \
		"$(printf '%s' "${referenced}" | tr '\n' ' ')" "$(printf '%s' "${passed}" | tr '\n' ' ')" >&2
	exit 3
fi

if [[ -e "${state}/psql_fail_${tag}" ]]; then
	printf 'psql: error: connection to server failed\n' >&2
	exit 2
fi
if [[ -e "${state}/psql_garbage_${tag}" ]]; then
	printf 'not rows\n'
	exit 0
fi

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
	awk -F'|' -v kind="${kind}" -v in_tree="$(var in_tree)" '
		BEGIN { n = split(in_tree, list, ","); for (i = 1; i <= n; i++) tree[list[i]] = 1 }
		$1 == kind && !($2 in tree) {
			if ($3 == "claimed") claimed++
			if ($4 == "active") active++
		}
		END { printf "%d|%d\n", claimed, active }' "${state}/claims"
	;;
dual)
	awk -F'|' -v kind="${kind}" -v in_tree="$(var in_tree)" -v overlaps="$(cat "${state}/overlaps" 2>/dev/null || echo 0)" '
		BEGIN { n = split(in_tree, list, ","); for (i = 1; i <= n; i++) tree[list[i]] = 1 }
		$1 == kind && $3 == "claimed" { if ($2 in tree) in_claimed++; else ext_claimed++ }
		END { printf "%d|%d|%d\n", ext_claimed, in_claimed, overlaps }' "${state}/claims"
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
keys)
	cat "${state}/keys"
	;;
*)
	printf 'fake psql: unknown query tag "%s"\n' "${tag}" >&2
	exit 64
	;;
esac
