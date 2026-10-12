#!/usr/bin/env bash
# Fake `gh` for scripts/test-resolve-required-gates-pr.sh. It answers from
# ESHU_WORLD_JSON, every PR the base repository knows about, the way GitHub
# does: commits/<sha>/pulls lists only PRs whose head is in the base
# repository (#7950), and the open pull request list is paged by per_page.
set -euo pipefail
[[ "${1:-}" == api ]] || exit 79
shift
paginate=false
slurp=false
while [[ "${1:-}" == --* ]]; do
	case "$1" in
	--paginate) paginate=true ;;
	--slurp) slurp=true ;;
	*) exit 79 ;;
	esac
	shift
done
endpoint="${1:-}"
case "${endpoint}" in
"repos/${GITHUB_REPOSITORY}/commits/"*/pulls)
	sha="${endpoint#"repos/${GITHUB_REPOSITORY}/commits/"}"
	sha="${sha%/pulls}"
	jq -c --arg repo "${GITHUB_REPOSITORY}" --arg sha "${sha}" \
		'[.[] | select(.head.repo.full_name == $repo and .head.sha == $sha)]' "${ESHU_WORLD_JSON}"
	;;
"repos/${GITHUB_REPOSITORY}/pulls?state=open&per_page="*)
	per_page="${endpoint##*per_page=}"
	pages="$(jq -c --argjson n "${per_page}" \
		'[.[] | select(.state == "open")] | [range(0; length; $n) as $i | .[$i:$i + $n]] | if length == 0 then [[]] else . end' \
		"${ESHU_WORLD_JSON}")"
	if [[ "${paginate}" == true && "${slurp}" == true ]]; then
		printf '%s\n' "${pages}"
	elif [[ "${paginate}" == true ]]; then
		jq -c 'add' <<<"${pages}"
	else
		jq -c '.[0]' <<<"${pages}"
	fi
	;;
*) exit 79 ;;
esac
