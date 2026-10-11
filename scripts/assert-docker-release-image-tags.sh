#!/usr/bin/env bash
set -euo pipefail

fail() {
	printf '::error::%s\n' "$1" >&2
	exit 1
}

expected_sha="${EXPECTED_SHA:-}"
[[ "${expected_sha}" =~ ^[0-9a-f]{40}$ ]] || fail 'release image tag guard requires a full lowercase 40-character SHA'

expected_tag="ghcr.io/eshu-hq/eshu:sha-${expected_sha}"
mapfile -t image_tags <<< "${RELEASE_IMAGE_TAGS:-}"
[[ "${#image_tags[@]}" -eq 1 ]] || fail 'release image metadata must contain exactly one tag'
[[ "${image_tags[0]}" == "${expected_tag}" ]] || fail 'release image metadata must contain only ghcr.io/eshu-hq/eshu:sha-<full expected SHA>'

printf 'release image metadata is exactly %s\n' "${expected_tag}"
