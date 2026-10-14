#!/usr/bin/env bash
# Resolves the container image ref the post-publish Trivy scan covers (#7699).
# Branch pushes and manual dispatches resolve to the immutable per-commit
# sha-<full sha> tag: under parallel publishing the moving :main tag is racy
# (scans would duplicate and miss), while the per-commit tag exists if and
# only if the triggering run pushed an image. Tag releases keep their version
# ref. When the ref is absent from GHCR — a chart-only run pushed no image —
# the scan skips green instead of failing on a ref that will never exist.
#
# GHCR does not take basic auth on the manifest endpoint: it answers 401 with
# a Bearer challenge, so the script mints a pull-scoped token from the
# challenge realm and retries the manifest read with it (#8059), the same
# exchange docker/login-action performs.
#
# Required env: TRIGGER_HEAD_BRANCH, TRIGGER_HEAD_SHA (either may be empty),
# REPOSITORY, GITHUB_TOKEN (packages read), GITHUB_OUTPUT. Writes ref=<ref>
# (when resolvable) and skip=<true|false> to GITHUB_OUTPUT. The token and
# the Bearer token never appear on stdout or stderr.
set -euo pipefail

branch="${TRIGGER_HEAD_BRANCH:-}"
sha="${TRIGGER_HEAD_SHA:-}"
repo="${REPOSITORY:?REPOSITORY is required}"
token="${GITHUB_TOKEN:?GITHUB_TOKEN is required}"
out="${GITHUB_OUTPUT:?GITHUB_OUTPUT is required}"

ref=""
if [[ "${branch}" =~ ^v[0-9] ]]; then
	ref="ghcr.io/${repo}:${branch#v}"
elif [[ -n "${sha}" ]]; then
	ref="ghcr.io/${repo}:sha-${sha}"
else
	{
		echo "skip=true"
	} >>"${out}"
	echo "image-scan: triggering run has no branch or sha; nothing to scan"
	exit 0
fi

tag="${ref##*:}"
api="https://ghcr.io/v2/${repo}"
manifest_accept='application/vnd.oci.image.index.v1+json'

# manifest_probe: prints the manifest response headers for a basic-auth
# request, with the HTTP code on the last line. Fails when curl itself fails.
manifest_probe() {
	curl --silent --show-error --connect-timeout 10 --max-time 30 \
		-D - -o /dev/null -w $'\n%{http_code}' \
		-u "x-access-token:${token}" \
		-H "Accept: ${manifest_accept}" \
		"${api}/manifests/${tag}"
}

# manifest_code <bearer>: prints the manifest HTTP code for a request
# carrying the Bearer token. Fails when curl itself fails.
manifest_code() {
	local bearer="$1"
	curl --silent --show-error --connect-timeout 10 --max-time 30 \
		-o /dev/null -w '%{http_code}' \
		-H "Authorization: Bearer ${bearer}" \
		-H "Accept: ${manifest_accept}" \
		"${api}/manifests/${tag}"
}

# emit_for_code <code>: records the scan decision for a manifest HTTP code.
emit_for_code() {
	local manifest_status="$1"
	case "${manifest_status}" in
	200)
		{
			echo "ref=${ref}"
			echo "skip=false"
		} >>"${out}"
		echo "image-scan: scanning ${ref}"
		;;
	404)
		{
			echo "ref=${ref}"
			echo "skip=true"
		} >>"${out}"
		echo "image-scan: ${ref} is not in GHCR; the triggering run pushed no image, skipping"
		;;
	*)
		echo "image-scan: GHCR lookup for ${ref} failed with HTTP ${manifest_status}" >&2
		exit 1
		;;
	esac
}

# challenge_value <key>: prints key="..." from the Bearer challenge.
challenge_value() {
	printf '%s' "${challenge}" | sed -n "s/.*${1}=\"\([^\"]*\)\".*/\1/p"
}

probe="$(manifest_probe)" || {
	echo "image-scan: GHCR lookup for ${ref} failed (no response)" >&2
	exit 1
}
code="${probe##*$'\n'}"
headers="${probe%$'\n'*}"

if [[ "${code}" == "401" ]]; then
	# Basic auth drew the registry challenge: mint a pull-scoped token from
	# its realm and retry the manifest read with the Bearer token.
	challenge="$(printf '%s' "${headers}" | grep -i '^www-authenticate:' | head -n 1 || true)"
	realm="$(challenge_value realm)"
	service="$(challenge_value service)"
	scope="$(challenge_value scope)"
	if [[ "${challenge}" != *"Bearer"* || -z "${realm}" || -z "${service}" || -z "${scope}" ]]; then
		echo "image-scan: GHCR lookup for ${ref} failed with HTTP 401 (no Bearer challenge)" >&2
		exit 1
	fi
	token_response="$(curl --fail --silent --show-error --connect-timeout 10 --max-time 30 \
		--get -u "x-access-token:${token}" \
		--data-urlencode "service=${service}" \
		--data-urlencode "scope=${scope}" "${realm}")" || {
		echo 'image-scan: GHCR token exchange failed' >&2
		exit 1
	}
	bearer="$(jq -er '(.token // .access_token) | select(type == "string" and length > 0)' <<<"${token_response}")" || {
		echo 'image-scan: GHCR token response has no token' >&2
		exit 1
	}
	code="$(manifest_code "${bearer}")" || {
		echo "image-scan: GHCR lookup for ${ref} failed (no response)" >&2
		exit 1
	}
fi

emit_for_code "${code}"
