#!/usr/bin/env bash
set -euo pipefail
url=""
head_mode=0
authorized=0
for a in "$@"; do
	case "${a}" in
	https://*) url="${a}" ;;
	"Authorization: Bearer fixture-registry-token") authorized=1 ;;
	-D) head_mode=1 ;;
	esac
done
echo "curl ${url} head=${head_mode}" >>"${STUB_DIR}/curl.log"
if [[ "${url}" == https://ghcr.io/token ]]; then
 case "${STUB_TOKEN_MODE:-token}" in
 denied) exit 22 ;;
 empty) printf '{"token":""}' ;;
 malformed) printf 'not-json' ;;
 access_token) printf '{"access_token":"fixture-registry-token"}' ;;
 *) printf '{"token":"fixture-registry-token"}' ;;
 esac
 exit 0
fi
if [[ "${STUB_REQUIRE_BEARER:-0}" == 1 && "${authorized}" != 1 || "${STUB_REGISTRY_DENIED:-0}" == 1 ]]; then
 if [[ "${head_mode}" == 1 ]]; then
  printf 'HTTP/2 401\r\ndocker-content-digest: sha256:1111\r\nWWW-Authenticate: Bearer realm="https://ghcr.io/token",service="ghcr.io"\r\n\r\n'
 else
  printf '{"errors":[{"code":"UNAUTHORIZED"}]}\n401'
 fi
 exit 0
fi
path="${url#https://ghcr.io/v2/${STUB_REPO}/}"
if [[ -n "${STUB_MISSING_PATH:-}" && "${path}" == "${STUB_MISSING_PATH}" ]]; then
 printf '{"errors":[{"code":"NOT_FOUND"}]}\n404'
 exit 0
fi
san() { printf '%s' "$1" | tr -c 'A-Za-z0-9' '_'; }
known_digest() {
	local want="$1" f
	for f in "${STUB_DIR}"/tags/*; do
		[ -f "${f}" ] || continue
		[ "$(cat "${f}")" == "${want}" ] && return 0
	done
	return 1
}
if [[ "${head_mode}" == "1" ]]; then
	tag="${path#manifests/}"
	if [ -f "${STUB_DIR}/tags/${tag}" ]; then
		printf 'HTTP/2 200\r\ndocker-content-digest: %s\r\n\r\n' "$(cat "${STUB_DIR}/tags/${tag}")"
	else
		printf 'HTTP/2 404\r\n\r\n'
	fi
	exit 0
fi
case "${path}" in
manifests/sha256:mf*)
	hex="${path#manifests/sha256:mf}"
	d="sha256:${hex}"
	if known_digest "${d}"; then
		printf '{"schemaVersion":2,"config":{"digest":"sha256:cf%s"}}' "${hex}"
		printf '\n200'
	else
		printf '{"errors":[{"code":"MANIFEST_UNKNOWN"}]}'
		printf '\n404'
	fi
	;;
manifests/sha256:*)
	printf '{"errors":[{"code":"MANIFEST_UNKNOWN"}]}'
	printf '\n404'
	;;
manifests/*)
	tag="${path#manifests/}"
	if [ -f "${STUB_DIR}/tags/${tag}" ]; then
		d="$(cat "${STUB_DIR}/tags/${tag}")"
		hex="${d#sha256:}"
		printf '{"schemaVersion":2,"manifests":[{"digest":"sha256:mf%s","platform":{"architecture":"amd64","os":"linux"}}]}' "${hex}"
		printf '\n200'
	else
		printf '{"errors":[{"code":"MANIFEST_UNKNOWN"}]}'
		printf '\n404'
	fi
	;;
blobs/sha256:cf*)
	hex="${path#blobs/sha256:cf}"
	d="sha256:${hex}"
	if known_digest "${d}"; then
		rev_file="${STUB_DIR}/revs/$(san "${d}")"
		if [ -f "${rev_file}" ]; then
			printf '{"config":{"Labels":{"org.opencontainers.image.revision":"%s"}}}' "$(cat "${rev_file}")"
		else
			printf '{"config":{"Labels":{}}}'
		fi
		printf '\n200'
	else
		printf '{"errors":[{"code":"BLOB_UNKNOWN"}]}'
		printf '\n404'
	fi
	;;
*)
	printf '{"errors":[{"code":"DENIED"}]}'
	printf '\n500'
	;;
esac
