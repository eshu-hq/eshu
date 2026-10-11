#!/usr/bin/env bash
set -euo pipefail

fail() {
	printf '::error::%s\n' "$1" >&2
	exit 1
}

[[ "${GITHUB_REPOSITORY:-}" == 'eshu-hq/eshu' ]] || fail 'release image guard only permits eshu-hq/eshu'
github_token="${GITHUB_TOKEN:-}"
[[ -n "${github_token}" ]] || fail 'release image guard requires the workflow token'
expected_tag="${EXPECTED_TAG:-}"
[[ "${expected_tag}" =~ ^sha-[0-9a-f]{40}$ ]] || fail 'release image tag must contain a full commit SHA'

repo='eshu-hq/eshu'
if ! token_response="$(curl --fail --silent --show-error --connect-timeout 10 --max-time 30 \
	--get -u "x-access-token:${github_token}" \
	--data-urlencode 'service=ghcr.io' \
	--data-urlencode "scope=repository:${repo}:pull" \
	https://ghcr.io/token 2>/dev/null)"; then
	fail 'could not authenticate a read-only GHCR manifest check'
fi
if ! registry_token="$(jq -er '(.token // .access_token) | select(type == "string" and length > 0)' \
	<<< "${token_response}" 2>/dev/null)"; then
	fail 'GHCR returned an invalid read-only token response'
fi
unset token_response github_token

manifest_accept='application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json'
if ! headers="$(curl --silent --show-error --connect-timeout 10 --max-time 30 \
	-D - -o /dev/null \
	-H "Authorization: Bearer ${registry_token}" \
	-H "Accept: ${manifest_accept}" \
	"https://ghcr.io/v2/${repo}/manifests/${expected_tag}" 2>/dev/null)"; then
	fail 'could not complete the read-only GHCR manifest check'
fi
unset registry_token

status_code=''
digest_header=''
while IFS= read -r line; do
	line="${line%$'\r'}"
	if [[ "${line}" == HTTP/* ]]; then
		read -r _ status_code _ <<< "${line}"
		digest_header=''
	elif [[ "${line,,}" == 'docker-content-digest:'* ]]; then
		digest_header="${line#*: }"
	fi
done <<< "${headers}"

case "${status_code}" in
	404)
		[[ -z "${digest_header}" ]] || fail 'GHCR returned an unexpected digest for a missing image tag'
		printf '%s\n' 'release image tag is absent'
		;;
	200)
		[[ "${digest_header}" =~ ^sha256:[0-9a-f]{64}$ ]] || fail 'GHCR returned a malformed existing image digest'
		fail 'release image tag already exists; refusing overwrite or automatic rerun'
		;;
	401|403)
		fail 'GHCR denied the read-only manifest check'
		;;
	*)
		fail "GHCR returned unexpected manifest status ${status_code:-missing}"
		;;
esac
