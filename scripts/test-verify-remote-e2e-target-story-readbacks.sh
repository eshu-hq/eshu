#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

urlencode() {
	jq -rn --arg value "$1" '$value|@uri'
}

# shellcheck source=scripts/lib/remote_e2e_target_story_readbacks.sh
source "${repo_root}/scripts/lib/remote_e2e_target_story_readbacks.sh"

assert_eq() {
	local label="$1"
	local got="$2"
	local want="$3"
	if [[ "${got}" != "${want}" ]]; then
		printf '%s = %s, want %s\n' "${label}" "${got}" "${want}" >&2
		exit 1
	fi
}

selector="$(target_story_service_selector "service:api" "")"
path="$(target_story_service_api_path "${selector}" "service:api" "" "repo://example/api")"
assert_eq \
	"service-only path" \
	"${path}" \
	"/services/api/story?repo=repo%3A%2F%2Fexample%2Fapi"

selector="$(target_story_service_selector "service:api" "workload:api")"
path="$(target_story_service_api_path "${selector}" "service:api" "workload:api" "repo://example/api")"
assert_eq \
	"workload-disambiguated path" \
	"${path}" \
	"/services/api/story?service_id=service%3Aapi&repo=repo%3A%2F%2Fexample%2Fapi"
