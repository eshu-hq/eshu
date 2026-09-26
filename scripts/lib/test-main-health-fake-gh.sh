#!/usr/bin/env bash
# Serves $FIXTURES/<name> for a read endpoint; records every call.
set -euo pipefail
jq -cn '$ARGS.positional' --args -- "$@" >>"${FIXTURES}/calls.log"
[[ "$1" == "api" ]] || { echo "fake gh: unsupported: $*" >&2; exit 2; }
shift
method=GET
path=""
allow_esc=0
while [[ $# -gt 0 ]]; do
	case "$1" in
	-X) method="$2"; shift 2 ;;
	-f | -F)
		[[ "$2" == body=* ]] && printf '%s' "${2#body=}" >"${FIXTURES}/last-body.txt"
		shift 2 ;;
	--jq | --input) shift 2 ;;
	--paginate) shift ;;
	--allow-escape-sequences) allow_esc=1; shift ;;
	*) [[ -z "${path}" ]] && path="$1"; shift ;;
	esac
done
serve() { cat "${FIXTURES}/$1" 2>/dev/null || { echo "fake gh: no fixture $1 for ${path}" >&2; exit 1; }; }
# runs_filtered <fixture> [workflow-file]: apply the API's query filters.
runs_filtered() {
	local query="${path#*\?}" q='{}' kv
	[[ "${path}" == *\?* ]] || query=""
	for kv in ${query//&/ }; do
		q="$(jq -c --arg k "${kv%%=*}" --arg v "${kv#*=}" '. + {($k): $v}' <<<"${q}")"
	done
	jq -c --argjson q "${q}" --arg wf "${2:-}" \
		--argjson total "$(cat "${FIXTURES}/total-count" 2>/dev/null || echo null)" '
		[.workflow_runs[]
		 | select(($wf == "") or (.path == (".github/workflows/" + $wf)))
		 | select(($q.head_sha // .head_sha) == .head_sha)
		 | select(($q.event // .event) == .event)
		 | select(($q.branch // .head_branch) == .head_branch)
		 | select(($q.status // .status) == .status)] as $m
		| {total_count: ($total // ($m | length)),
		   workflow_runs: (if $q.per_page then $m[0:($q.per_page | tonumber)] else $m end)}' "${FIXTURES}/$1"
}
if [[ "${method}" != "GET" || "${path}" == graphql ]]; then
	[[ -f "${FIXTURES}/write-fails" ]] && rg -qxF -- "${path}" "${FIXTURES}/write-fails" && exit 1
	echo '{"number":99,"node_id":"N99","html_url":"https://github.example/eshu-hq/eshu/issues/99"}'
	exit 0
fi
case "${path}" in
repos/*/commits/main) serve tip.json ;;
repos/*/commits/*/status) jq '{state: "pending", statuses: .}' "${FIXTURES}/statuses.json" ;;
repos/*/actions/workflows/required-gates.yml/runs*) runs_filtered ruleset-runs.json ;;
repos/*/actions/workflows/*/runs*) wf="${path#*/workflows/}"; runs_filtered runs.json "${wf%%/*}" ;;
repos/*/actions/runs/*/jobs*) id="${path#*/runs/}"; serve "jobs-${id%%/*}.json" ;;
repos/*/actions/runs\?*) runs_filtered runs.json ;;
repos/*/actions/jobs/*/logs)
	# gh >= 2.101 refuses to print a response carrying terminal escape
	# sequences unless --allow-escape-sequences is passed (real CI logs do).
	id="${path#*/jobs/}"
	if [[ "${allow_esc}" -eq 0 ]] && rg -q $'\x1b' "${FIXTURES}/log-${id%%/*}.txt" 2>/dev/null; then
		echo "the response contains terminal escape sequences; pass --allow-escape-sequences to output it anyway" >&2
		exit 1
	fi
	serve "log-${id%%/*}.txt" ;;
repos/*/issues\?*) serve issues.json ;;
*) echo "fake gh: unhandled ${path}" >&2; exit 1 ;;
esac
