#!/usr/bin/env bash
set -euo pipefail

fail() {
	printf '::error::%s\n' "$1" >&2
	exit 1
}

write_lanes() {
	local image="$1"
	local chart="$2"
	local apkfloors="$3"
	: "${GITHUB_OUTPUT:?GITHUB_OUTPUT is required}"
	{
		printf 'image=%s\n' "${image}"
		printf 'chart=%s\n' "${chart}"
		printf 'apkfloors=%s\n' "${apkfloors}"
	} >> "${GITHUB_OUTPUT}"
}

if [[ "${GITHUB_EVENT_NAME:-}" == "workflow_dispatch" ]]; then
	expected_sha="${EXPECTED_SHA:-}"
	[[ "${expected_sha}" =~ ^[0-9a-f]{40}$ ]] || fail 'release image dispatch requires a full lowercase 40-character expected_sha'
	[[ "${GITHUB_REPOSITORY:-}" == 'eshu-hq/eshu' ]] || fail 'release image dispatch is restricted to eshu-hq/eshu'
	[[ "${GITHUB_REF:-}" == 'refs/heads/release/7934-qa-metrics-jira' ]] || fail 'release image dispatch is restricted to refs/heads/release/7934-qa-metrics-jira'
	[[ "${GITHUB_WORKFLOW_REF:-}" == 'eshu-hq/eshu/.github/workflows/docker-publish.yml@refs/heads/release/7934-qa-metrics-jira' ]] || fail 'release image dispatch must run this workflow from the release ref'
	[[ "${GITHUB_SHA:-}" == "${expected_sha}" ]] || fail 'release image dispatch expected_sha does not match GITHUB_SHA'
	checkout_sha="$(git rev-parse HEAD)" || fail 'could not resolve the checked out commit'
	[[ "${checkout_sha}" == "${expected_sha}" ]] || fail 'release image dispatch expected_sha does not match checkout HEAD'
	write_lanes true false false
	exit 0
fi

if [[ "${GITHUB_REF_TYPE:-}" == "tag" ]]; then
	write_lanes true true true
	exit 0
fi

merge_group_lane=false
if [[ "${GITHUB_EVENT_NAME:-}" == "pull_request" ]]; then
	base_sha="${PULL_REQUEST_BASE_SHA:?PULL_REQUEST_BASE_SHA is required}"
	head_sha="${PULL_REQUEST_HEAD_SHA:?PULL_REQUEST_HEAD_SHA is required}"
elif [[ "${GITHUB_EVENT_NAME:-}" == "merge_group" ]]; then
	merge_group_lane=true
	head_sha="${GITHUB_SHA:?GITHUB_SHA is required}"
	base_sha="$(git merge-base origin/main "${head_sha}")"
elif [[ "${GITHUB_EVENT_NAME:-}" == "push" ]]; then
	base_sha="${PUSH_BEFORE_SHA:?PUSH_BEFORE_SHA is required}"
	head_sha="${GITHUB_SHA:?GITHUB_SHA is required}"
	if [[ "${base_sha}" =~ ^0+$ ]]; then
		base_sha="$(git rev-parse "${head_sha}^")"
	fi
else
	fail 'unsupported Docker publication event'
fi

image=false
chart=false
apkfloors=false
while IFS= read -r path; do
	case "${path}" in
		Dockerfile)
			image=true
			apkfloors=true
			;;
		.dockerignore)
			image=true
			;;
		scripts/verify-apk-floors.sh|scripts/test-verify-apk-floors.sh|scripts/fixtures/apk-floors/*)
			apkfloors=true
			;;
		go/*)
			if [[ "${merge_group_lane}" == "false" ]]; then
				image=true
			fi
			;;
		deploy/helm/*)
			chart=true
			;;
		.github/workflows/docker-publish.yml|scripts/select-docker-publication-lanes.sh|scripts/test-docker-release-image-dispatch.sh|scripts/assert-docker-release-image-tags.sh|scripts/assert-ghcr-image-tag-absent.sh)
			image=true
			chart=true
			apkfloors=true
			;;
	esac
done < <(git diff --name-only "${base_sha}" "${head_sha}")

write_lanes "${image}" "${chart}" "${apkfloors}"
