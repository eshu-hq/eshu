#!/usr/bin/env bash
# Hermetic cases for scripts/ci/resolve-image-scan-ref.sh (#7699, #8059).
# A fixture fake `curl` (scripts/fixtures/resolve-image-scan-ref-stub-curl.sh)
# plays a GHCR registry that answers the Bearer challenge and serves
# manifests only to requests carrying the Bearer token. The challenge realm
# is fake-ghcr.invalid, never ghcr.io, which proves the script follows the
# realm instead of hardcoding the token URL. The cases assert the resolved
# ref, the skip decision, the exchange itself, secret hygiene, and the
# fail-loud path. The stub lives in a fixture file because its body exceeds
# the heredoc budget (#5074).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="${repo_root}/scripts/ci/resolve-image-scan-ref.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

if [ ! -x "${target}" ]; then
	echo "test-resolve-image-scan-ref: missing executable script at ${target}" >&2
	exit 1
fi

pass=0
fail=0
check() {
	local desc="$1" status="$2"
	if [ "${status}" -eq 0 ]; then
		printf 'PASS: %s\n' "${desc}"
		pass=$((pass + 1))
	else
		printf 'FAIL: %s\n' "${desc}"
		fail=$((fail + 1))
	fi
}

mkdir -p "${work}/bin"

# Fake GHCR registry. State: ${STUB_DIR}/tags/<tag> presence decides 200 vs
# 404 for Bearer manifest reads; anything without the minted token draws the
# 401 challenge. Every call is logged sanitized to curl.log.
cp "${repo_root}/scripts/fixtures/resolve-image-scan-ref-stub-curl.sh" "${work}/bin/curl"
chmod +x "${work}/bin/curl"

export PATH="${work}/bin:${PATH}"

# Distinctive secret: the resolver output must never contain this value. The
# minted Bearer value is the stub's constant fixture-bearer-token.
FAKE_GITHUB_TOKEN="fake-ghtoken-4Vz8Lp3qW2"
BEARER_VALUE="fixture-bearer-token"

SHA="0123456789abcdef0123456789abcdef01234567"

fresh_case() {
	STUB_DIR="$(mktemp -d "${work}/case.XXXXXX")"
	export STUB_DIR
	export STUB_REPO="eshu-hq/eshu"
	unset STUB_FORCE_CODE
	export STUB_TOKEN_MODE=token
	mkdir -p "${STUB_DIR}/tags"
	: >"${STUB_DIR}/curl.log"
	GH_OUT="${STUB_DIR}/output.txt"
	: >"${GH_OUT}"
}

run_resolver() {
	# run_resolver <branch> <sha>: runs the script, captures rc + output.
	set +e
	RESOLVE_OUTPUT="$(
		TRIGGER_HEAD_BRANCH="$1" TRIGGER_HEAD_SHA="$2" \
			REPOSITORY="eshu-hq/eshu" GITHUB_TOKEN="${FAKE_GITHUB_TOKEN}" \
			GITHUB_OUTPUT="${GH_OUT}" bash "${target}" 2>&1
	)"
	RESOLVE_RC=$?
	set -e
}

out_has() { grep -q -F -e "$1" "${GH_OUT}"; }
req_has() { grep -q -F -e "$1" "${STUB_DIR}/curl.log"; }
output_leaks() { printf '%s' "${RESOLVE_OUTPUT}" | grep -q -F -e "$1" || return 1; }
output_file_leaks() { grep -q -F -e "$1" "${GH_OUT}" || return 1; }

# 1. A tag release resolves to the version ref and scans.
fresh_case
printf 'x' >"${STUB_DIR}/tags/1.2.3"
run_resolver "v1.2.3" "${SHA}"
check "tag release exits 0 (rc=${RESOLVE_RC})" "$([ "${RESOLVE_RC}" == "0" ] && echo 0 || echo 1)"
check "tag release resolves the version ref" "$(out_has 'ref=ghcr.io/eshu-hq/eshu:1.2.3' && echo 0 || echo 1)"
check "tag release does not skip" "$(out_has 'skip=false' && echo 0 || echo 1)"

# 2. A main push resolves to the full-sha ref and scans.
fresh_case
printf 'x' >"${STUB_DIR}/tags/sha-${SHA}"
run_resolver "main" "${SHA}"
check "main push exits 0 (rc=${RESOLVE_RC})" "$([ "${RESOLVE_RC}" == "0" ] && echo 0 || echo 1)"
check "main push resolves sha-<full sha>" "$(out_has "ref=ghcr.io/eshu-hq/eshu:sha-${SHA}" && echo 0 || echo 1)"
check "main push does not skip" "$(out_has 'skip=false' && echo 0 || echo 1)"

# 3. A chart-only run (no per-commit tag) skips green on a 404 with a
# valid token.
fresh_case
run_resolver "main" "${SHA}"
check "chart-only exits 0 (rc=${RESOLVE_RC})" "$([ "${RESOLVE_RC}" == "0" ] && echo 0 || echo 1)"
check "chart-only skips" "$(out_has 'skip=true' && echo 0 || echo 1)"
check "chart-only still records the ref" "$(out_has "ref=ghcr.io/eshu-hq/eshu:sha-${SHA}" && echo 0 || echo 1)"
check "chart-only says why" "$(printf '%s' "${RESOLVE_OUTPUT}" | grep -q 'pushed no image' && echo 0 || echo 1)"
check "chart-only reached the manifest with a token (404, not 401)" "$(req_has "manifest tag=sha-${SHA} bearer=yes -> 404" && echo 0 || echo 1)"

# 4. A dispatch on another branch resolves to the sha ref, not the branch.
fresh_case
printf 'x' >"${STUB_DIR}/tags/sha-${SHA}"
run_resolver "feature-thing" "${SHA}"
check "dispatch resolves the sha ref" "$(out_has "ref=ghcr.io/eshu-hq/eshu:sha-${SHA}" && echo 0 || echo 1)"
check "dispatch does not resolve the branch" "$(out_has 'eshu:feature-thing' && echo 1 || echo 0)"

# 5. No branch and no sha skips without touching the registry.
fresh_case
run_resolver "" ""
check "empty trigger exits 0 (rc=${RESOLVE_RC})" "$([ "${RESOLVE_RC}" == "0" ] && echo 0 || echo 1)"
check "empty trigger skips" "$(out_has 'skip=true' && echo 0 || echo 1)"
check "empty trigger makes no registry call" "$([ ! -s "${STUB_DIR}/curl.log" ] && echo 0 || echo 1)"

# 6. A non-404 registry failure fails loudly instead of skipping.
fresh_case
printf 'x' >"${STUB_DIR}/tags/sha-${SHA}"
export STUB_FORCE_CODE=500
run_resolver "main" "${SHA}"
check "registry 500 exits non-zero (rc=${RESOLVE_RC})" "$([ "${RESOLVE_RC}" != "0" ] && echo 0 || echo 1)"

# 7. The Bearer exchange: the first manifest hit challenges (401), the
# script mints a token from the challenge realm, and only the Bearer
# request is served. Fails on the pre-#8059 script, which sends basic
# auth and exits 1 on the 401.
fresh_case
printf 'x' >"${STUB_DIR}/tags/sha-${SHA}"
run_resolver "main" "${SHA}"
check "exchange flow exits 0 (rc=${RESOLVE_RC})" "$([ "${RESOLVE_RC}" == "0" ] && echo 0 || echo 1)"
check "first manifest hit was challenged (no bearer -> 401)" "$(req_has "manifest tag=sha-${SHA} bearer=no -> 401" && echo 0 || echo 1)"
check "script minted a token from the realm" "$(req_has 'token basic=yes mode=token' && echo 0 || echo 1)"
check "manifest served only to the bearer request" "$(req_has "manifest tag=sha-${SHA} bearer=yes -> 200" && echo 0 || echo 1)"

# 8. A denied token exchange fails loudly instead of skipping.
fresh_case
printf 'x' >"${STUB_DIR}/tags/sha-${SHA}"
export STUB_TOKEN_MODE=denied
run_resolver "main" "${SHA}"
check "denied exchange exits non-zero (rc=${RESOLVE_RC})" "$([ "${RESOLVE_RC}" != "0" ] && echo 0 || echo 1)"

# 9. Neither the token nor the Bearer value ever reaches the step log.
fresh_case
printf 'x' >"${STUB_DIR}/tags/sha-${SHA}"
run_resolver "main" "${SHA}"
check "resolver output hides the github token" "$(output_leaks "${FAKE_GITHUB_TOKEN}" && echo 1 || echo 0)"
check "resolver output hides the bearer token" "$(output_leaks "${BEARER_VALUE}" && echo 1 || echo 0)"
check "output file hides the github token" "$(output_file_leaks "${FAKE_GITHUB_TOKEN}" && echo 1 || echo 0)"
check "output file hides the bearer token" "$(output_file_leaks "${BEARER_VALUE}" && echo 1 || echo 0)"

printf 'test-resolve-image-scan-ref: %d passed, %d failed\n' "${pass}" "${fail}"
[ "${fail}" == "0" ]
