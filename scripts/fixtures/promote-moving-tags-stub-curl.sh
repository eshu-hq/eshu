#!/usr/bin/env bash
set -euo pipefail
url=""
head_mode=0
for a in "$@"; do
	case "${a}" in
	https://*) url="${a}" ;;
	-D) head_mode=1 ;;
	esac
done
echo "curl ${url} head=${head_mode}" >>"${STUB_DIR}/curl.log"
path="${url#https://ghcr.io/v2/${STUB_REPO}/}"
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
