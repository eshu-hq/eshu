#!/usr/bin/env bash
# Fake `curl` for scripts/test-resolve-image-scan-ref.sh (#8059): a GHCR
# registry that answers the Bearer challenge and serves manifests only to
# requests carrying the Bearer token. State: ${STUB_DIR}/tags/<tag> presence
# decides 200 vs 404, and ${STUB_REPO} scopes the v2 path. Knobs:
# STUB_TOKEN_MODE (token|access_token|empty|malformed|denied, default token)
# and STUB_FORCE_CODE (manifest code override for Bearer reads). The challenge
# realm is fake-ghcr.invalid, never ghcr.io, so the suite proves the script
# follows the realm instead of hardcoding the token URL. Every call is logged
# sanitized (credential presence only, never values) to ${STUB_DIR}/curl.log.
set -euo pipefail

realm="https://fake-ghcr.invalid/token"
minted="fixture-bearer-token"

url=""
head_mode=0
bearer=""
basic=0
prev=""
for a in "$@"; do
	case "${a}" in
	https://*) url="${a}" ;;
	"Authorization: Bearer "*) bearer="${a#Authorization: Bearer }" ;;
	-D) head_mode=1 ;;
	esac
	if [[ "${prev}" == "-u" ]]; then basic=1; fi
	prev="${a}"
done
present() { [ -n "$1" ] && printf 'yes' || printf 'no'; }

# Token endpoint: mints only at the advertised realm, only for basic auth.
if [[ "${url}" == "${realm}" ]]; then
	echo "token basic=$(present "${basic}") mode=${STUB_TOKEN_MODE:-token}" >>"${STUB_DIR}/curl.log"
	if [[ "${basic}" != "1" ]]; then exit 22; fi
	case "${STUB_TOKEN_MODE:-token}" in
	denied) exit 22 ;;
	empty) printf '{"token":""}' ;;
	malformed) printf 'not-json' ;;
	access_token) printf '{"access_token":"fixture-bearer-token"}' ;;
	*) printf '{"token":"fixture-bearer-token"}' ;;
	esac
	exit 0
fi

# Manifest endpoint: challenges without the minted Bearer token.
path="${url#https://ghcr.io/v2/${STUB_REPO}/}"
tag="${path#manifests/}"
if [[ "${path}" != manifests/* || "${tag}" == "" ]]; then
	echo "manifest unknown bearer=$(present "${bearer}") -> 500" >>"${STUB_DIR}/curl.log"
	[ "${head_mode}" == "1" ] && printf 'HTTP/1.1 500 Internal Error\r\n\r\n\n500' || printf '500'
	exit 0
fi
if [[ "${bearer}" != "${minted}" ]]; then
	echo "manifest tag=${tag} bearer=no -> 401" >>"${STUB_DIR}/curl.log"
	if [[ "${head_mode}" == "1" ]]; then
		printf 'HTTP/1.1 401 Unauthorized\r\nWWW-Authenticate: Bearer realm="%s",service="fake-ghcr",scope="repository:%s:pull"\r\n\r\n\n401' \
			"${realm}" "${STUB_REPO}"
	else
		printf '401'
	fi
	exit 0
fi
code=""
if [ -n "${STUB_FORCE_CODE:-}" ]; then
	code="${STUB_FORCE_CODE}"
elif [ -f "${STUB_DIR}/tags/${tag}" ]; then
	code="200"
else
	code="404"
fi
echo "manifest tag=${tag} bearer=yes -> ${code}" >>"${STUB_DIR}/curl.log"
if [[ "${head_mode}" == "1" ]]; then
	case "${code}" in
	200) printf 'HTTP/1.1 200 OK\r\nContent-Type: application/vnd.oci.image.index.v1+json\r\nDocker-Content-Digest: sha256:%064d\r\n\r\n\n200' 0 ;;
	404) printf 'HTTP/1.1 404 Not Found\r\n\r\n\n404' ;;
	*) printf 'HTTP/1.1 %s\r\n\r\n\n%s' "${code}" "${code}" ;;
	esac
else
	printf '%s' "${code}"
fi
