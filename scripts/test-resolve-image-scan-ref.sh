#!/usr/bin/env bash
# Hermetic cases for scripts/ci/resolve-image-scan-ref.sh (#7699). A fake
# `curl` answers manifest lookups from $STUB_DIR/tags (presence only); the
# cases assert the resolved ref, the skip decision, and the fail-loud path.
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

# Fake curl: only HEAD-equivalent manifest lookups. Prints the -w http_code:
# 200 when tags/<tag> exists, 404 otherwise, or STUB_CURL_CODE when set.
cat >"${work}/bin/curl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
url=""
for a in "$@"; do
	case "${a}" in https://*) url="${a}" ;; esac
done
echo "curl ${url}" >>"${STUB_DIR}/curl.log"
tag="${url##*/manifests/}"
if [ -n "${STUB_CURL_CODE:-}" ]; then
	printf '%s' "${STUB_CURL_CODE}"
elif [ -f "${STUB_DIR}/tags/${tag}" ]; then
	printf '200'
else
	printf '404'
fi
STUB
chmod +x "${work}/bin/curl"

export PATH="${work}/bin:${PATH}"

SHA="0123456789abcdef0123456789abcdef01234567"

fresh_case() {
	STUB_DIR="$(mktemp -d "${work}/case.XXXXXX")"
	export STUB_DIR
	unset STUB_CURL_CODE
	mkdir -p "${STUB_DIR}/tags"
	: >"${STUB_DIR}/curl.log"
	GH_OUT="${STUB_DIR}/output.txt"
	export GH_OUT
	: >"${GH_OUT}"
}

run_resolver() {
	# run_resolver <branch> <sha>: runs the script, captures rc + output.
	set +e
	RESOLVE_OUTPUT="$(
		TRIGGER_HEAD_BRANCH="$1" TRIGGER_HEAD_SHA="$2" \
			REPOSITORY="eshu-hq/eshu" GITHUB_TOKEN="fake" \
			GITHUB_OUTPUT="${GH_OUT}" bash "${target}" 2>&1
	)"
	RESOLVE_RC=$?
	set -e
}

out_has() { grep -q -F -e "$1" "${GH_OUT}"; }
curl_calls() { grep -c '^curl ' "${STUB_DIR}/curl.log" || true; }

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

# 3. A chart-only run (no per-commit tag) skips green.
fresh_case
run_resolver "main" "${SHA}"
check "chart-only exits 0 (rc=${RESOLVE_RC})" "$([ "${RESOLVE_RC}" == "0" ] && echo 0 || echo 1)"
check "chart-only skips" "$(out_has 'skip=true' && echo 0 || echo 1)"
check "chart-only still records the ref" "$(out_has "ref=ghcr.io/eshu-hq/eshu:sha-${SHA}" && echo 0 || echo 1)"
check "chart-only says why" "$(printf '%s' "${RESOLVE_OUTPUT}" | grep -q 'pushed no image' && echo 0 || echo 1)"

# 4. A dispatch on another branch resolves to the sha ref, not the branch.
fresh_case
printf 'x' >"${STUB_DIR}/tags/sha-${SHA}"
run_resolver "feature-thing" "${SHA}"
check "dispatch resolves the sha ref" "$(out_has "ref=ghcr.io/eshu-hq/eshu:sha-${SHA}" && echo 0 || echo 1)"
check "dispatch does not resolve the branch" "$(out_has 'eshu:feature-thing' && echo 1 || echo 0)"

# 5. No branch and no sha skips without touching GHCR.
fresh_case
run_resolver "" ""
check "empty trigger exits 0 (rc=${RESOLVE_RC})" "$([ "${RESOLVE_RC}" == "0" ] && echo 0 || echo 1)"
check "empty trigger skips" "$(out_has 'skip=true' && echo 0 || echo 1)"
check "empty trigger makes no GHCR call" "$([ "$(curl_calls)" == "0" ] && echo 0 || echo 1)"

# 6. A non-404 GHCR failure fails loudly instead of skipping.
fresh_case
export STUB_CURL_CODE=500
run_resolver "main" "${SHA}"
check "GHCR 500 exits non-zero (rc=${RESOLVE_RC})" "$([ "${RESOLVE_RC}" != "0" ] && echo 0 || echo 1)"

printf 'test-resolve-image-scan-ref: %d passed, %d failed\n' "${pass}" "${fail}"
[ "${fail}" == "0" ]
