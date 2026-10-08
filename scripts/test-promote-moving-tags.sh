#!/usr/bin/env bash
# Hermetic cases for scripts/ci/promote-moving-tags.sh (#7699). No GHCR, no
# network, no real git history: a fake `curl` serves a CRS-sized registry from
# $STUB_DIR/tags + $STUB_DIR/revs, a fake `docker` applies imagetools creates
# to that state (and can lose one race on demand), and a fake `git` answers
# merge-base --is-ancestor from a recorded parent list. The cases assert the
# promote/skip/fail DECISIONS and the exact tag writes for every ancestry
# shape, plus the retry loop, the sha sanity check, and the tag/dispatch arms.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="${repo_root}/scripts/ci/promote-moving-tags.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

command -v jq >/dev/null || {
	echo "test-promote-moving-tags: jq is required" >&2
	exit 1
}
if [ ! -x "${target}" ]; then
	echo "test-promote-moving-tags: missing executable script at ${target}" >&2
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

# Fake GHCR. State: ${STUB_DIR}/tags/<tag> holds a digest, and
# ${STUB_DIR}/revs/<sanitized digest> holds that image's revision label (a
# missing revs file means the image carries no label). Serves index, manifest,
# and blob documents synthesized from that state, and HEAD headers carrying
# the digest. Every call is logged to curl.log.
cat >"${work}/bin/curl" <<'STUB'
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
STUB

# Fake docker: applies `buildx imagetools create` to the tags state and logs
# every call. With RACE_LOSE_ONCE=1 the first create writes RACE_DIGEST (a
# concurrent winner's) instead of the requested digest.
cat >"${work}/bin/docker" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
echo "docker $*" >>"${STUB_DIR}/docker.log"
digest=""
tags=()
prev=""
for a in "$@"; do
	if [[ "${prev}" == "-t" ]]; then tags+=("${a}"); fi
	[[ "${a}" == *@sha256:* ]] && digest="${a##*@}"
	prev="${a}"
done
if [[ "${RACE_LOSE_ONCE:-0}" == "1" && ! -f "${STUB_DIR}/raced" ]]; then
	touch "${STUB_DIR}/raced"
	digest="${RACE_DIGEST}"
fi
for t in "${tags[@]}"; do
	printf '%s' "${digest}" >"${STUB_DIR}/tags/${t##*:}"
done
STUB

# Fake git: merge-base --is-ancestor over ${STUB_DIR}/ancestors ("child parent"
# per line, transitive). Anything else is an error.
cat >"${work}/bin/git" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "merge-base" && "${2:-}" == "--is-ancestor" ]]; then
	a="$3"
	b="$4"
	[[ "${a}" == "${b}" ]] && exit 0
	[ -f "${STUB_DIR}/ancestors" ] || exit 1
	seen=" ${b} "
	queue=("${b}")
	while [[ "${#queue[@]}" -gt 0 ]]; do
		n="${queue[0]}"
		queue=("${queue[@]:1}")
		while read -r child parent _; do
			[[ "${child}" == "${n}" ]] || continue
			[[ "${parent}" == "${a}" ]] && exit 0
			if [[ "${seen}" != *" ${parent} "* ]]; then
				seen+=" ${parent} "
				queue+=("${parent}")
			fi
		done <"${STUB_DIR}/ancestors"
	done
	exit 1
fi
echo "git stub: unsupported: $*" >&2
exit 2
STUB
chmod +x "${work}/bin/curl" "${work}/bin/docker" "${work}/bin/git"

export PATH="${work}/bin:${PATH}"

D_MINE="sha256:1111"
D_NEWER="sha256:2222"
D_OLD="sha256:3333"
D_X="sha256:4444"
D_OTHER="sha256:5555"
D_NOLABEL="sha256:6666"
MY_SHA="0123456789abcdef0123456789abcdef01234567"
SHA_NEWER="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
SHA_OLD="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
SHA_X="cccccccccccccccccccccccccccccccccccccccc"

san() { printf '%s' "$1" | tr -c 'A-Za-z0-9' '_'; }

# fresh_case <name>: new state dir with the sha tag always present.
fresh_case() {
	STUB_DIR="$(mktemp -d "${work}/case.XXXXXX")"
	export STUB_DIR
	export STUB_REPO="eshu-hq/eshu"
	export RACE_LOSE_ONCE=0
	mkdir -p "${STUB_DIR}/tags" "${STUB_DIR}/revs"
	: >"${STUB_DIR}/curl.log"
	: >"${STUB_DIR}/docker.log"
	: >"${STUB_DIR}/ancestors"
	printf '%s' "${D_MINE}" >"${STUB_DIR}/tags/sha-${MY_SHA}"
	printf '%s' "${MY_SHA}" >"${STUB_DIR}/revs/$(san "${D_MINE}")"
}

run_promote() {
	# run_promote <ref> <attempts>: runs the script, captures rc + output.
	local ref="$1" attempts="$2"
	set +e
	PROMOTE_OUTPUT="$(
		IMAGE="ghcr.io/eshu-hq/eshu" REPOSITORY="eshu-hq/eshu" \
			DIGEST="${D_MINE}" MY_SHA="${MY_SHA}" GITHUB_REF="${ref}" \
			GITHUB_TOKEN="fake" PROMOTE_ATTEMPTS="${attempts}" \
			PROMOTE_INTERVAL_S=0 bash "${target}" 2>&1
	)"
	PROMOTE_RC=$?
	set -e
}

creates() { grep -c '^docker ' "${STUB_DIR}/docker.log" || true; }
curl_calls() { grep -c '^curl ' "${STUB_DIR}/curl.log" || true; }
tag_is() { [ -f "${STUB_DIR}/tags/$1" ] && [ "$(cat "${STUB_DIR}/tags/$1")" == "$2" ]; }
single_dual_tag_create() {
	# The one create call must move BOTH tags in one command.
	[ "$(creates)" == "1" ] &&
		grep -q -- '-t ghcr.io/eshu-hq/eshu:main -t ghcr.io/eshu-hq/eshu:latest' "${STUB_DIR}/docker.log"
}

# 1. Absent :main promotes.
fresh_case absent
run_promote "refs/heads/main" 3
check "absent main exits 0 (rc=${PROMOTE_RC})" "$([ "${PROMOTE_RC}" == "0" ] && echo 0 || echo 1)"
check "absent main moves both tags in one create" "$(single_dual_tag_create && echo 0 || echo 1)"
check "absent main leaves tags on the digest" "$(tag_is main "${D_MINE}" && tag_is latest "${D_MINE}" && echo 0 || echo 1)"

# 2. Already current is a no-op.
fresh_case current
printf '%s' "${D_MINE}" >"${STUB_DIR}/tags/main"
printf '%s' "${D_MINE}" >"${STUB_DIR}/tags/latest"
run_promote "refs/heads/main" 3
check "current main exits 0 (rc=${PROMOTE_RC})" "$([ "${PROMOTE_RC}" == "0" ] && echo 0 || echo 1)"
check "current main pushes nothing" "$([ "$(creates)" == "0" ] && echo 0 || echo 1)"

# 3. A newer commit already won: skip green without pushing.
fresh_case descendant
printf '%s' "${D_NEWER}" >"${STUB_DIR}/tags/main"
printf '%s' "${D_NEWER}" >"${STUB_DIR}/tags/latest"
printf '%s' "${SHA_NEWER}" >"${STUB_DIR}/revs/$(san "${D_NEWER}")"
printf '%s %s\n' "${SHA_NEWER}" "${MY_SHA}" >"${STUB_DIR}/ancestors"
run_promote "refs/heads/main" 3
check "descendant main exits 0 (rc=${PROMOTE_RC})" "$([ "${PROMOTE_RC}" == "0" ] && echo 0 || echo 1)"
check "descendant main pushes nothing" "$([ "$(creates)" == "0" ] && echo 0 || echo 1)"
check "descendant main still points at the newer digest" "$(tag_is main "${D_NEWER}" && echo 0 || echo 1)"

# 4. An ancestor loses: promote.
fresh_case ancestor
printf '%s' "${D_OLD}" >"${STUB_DIR}/tags/main"
printf '%s' "${D_OLD}" >"${STUB_DIR}/tags/latest"
printf '%s' "${SHA_OLD}" >"${STUB_DIR}/revs/$(san "${D_OLD}")"
printf '%s %s\n' "${MY_SHA}" "${SHA_OLD}" >"${STUB_DIR}/ancestors"
run_promote "refs/heads/main" 3
check "ancestor main exits 0 (rc=${PROMOTE_RC})" "$([ "${PROMOTE_RC}" == "0" ] && echo 0 || echo 1)"
check "ancestor main moves both tags in one create" "$(single_dual_tag_create && echo 0 || echo 1)"
check "ancestor main leaves tags on the digest" "$(tag_is main "${D_MINE}" && tag_is latest "${D_MINE}" && echo 0 || echo 1)"

# 5. Unrelated history fails loudly without pushing.
fresh_case unrelated
printf '%s' "${D_X}" >"${STUB_DIR}/tags/main"
printf '%s' "${SHA_X}" >"${STUB_DIR}/revs/$(san "${D_X}")"
run_promote "refs/heads/main" 3
check "unrelated main exits non-zero (rc=${PROMOTE_RC})" "$([ "${PROMOTE_RC}" != "0" ] && echo 0 || echo 1)"
check "unrelated main pushes nothing" "$([ "$(creates)" == "0" ] && echo 0 || echo 1)"
check "unrelated main says why" "$(printf '%s' "${PROMOTE_OUTPUT}" | grep -q 'unrelated' && echo 0 || echo 1)"

# 6. A lost race re-evaluates: the first push does not stick, the loop sees
# the concurrent winner's newer commit and skips green.
fresh_case race
export RACE_LOSE_ONCE=1 RACE_DIGEST="${D_NEWER}"
printf '%s' "${SHA_NEWER}" >"${STUB_DIR}/revs/$(san "${D_NEWER}")"
printf '%s %s\n' "${SHA_NEWER}" "${MY_SHA}" >"${STUB_DIR}/ancestors"
run_promote "refs/heads/main" 3
check "lost race exits 0 (rc=${PROMOTE_RC})" "$([ "${PROMOTE_RC}" == "0" ] && echo 0 || echo 1)"
check "lost race attempted one create" "$([ "$(creates)" == "1" ] && echo 0 || echo 1)"
check "lost race leaves the winner's tags alone" "$(tag_is main "${D_NEWER}" && tag_is latest "${D_NEWER}" && echo 0 || echo 1)"
check "lost race reports the newer commit" "$(printf '%s' "${PROMOTE_OUTPUT}" | grep -q 'newer commit' && echo 0 || echo 1)"

# 7. The sha sanity check fails closed: a sha tag pointing anywhere else
# never promotes, and the attempts exhaust loudly.
fresh_case mismatch
printf '%s' "${D_OTHER}" >"${STUB_DIR}/tags/sha-${MY_SHA}"
run_promote "refs/heads/main" 2
check "sha mismatch exits non-zero (rc=${PROMOTE_RC})" "$([ "${PROMOTE_RC}" != "0" ] && echo 0 || echo 1)"
check "sha mismatch pushes nothing" "$([ "$(creates)" == "0" ] && echo 0 || echo 1)"
check "sha mismatch reports non-convergence" "$(printf '%s' "${PROMOTE_OUTPUT}" | grep -q 'did not converge' && echo 0 || echo 1)"

# 8. Tag pushes promote nothing and touch neither GHCR nor docker.
fresh_case tagref
run_promote "refs/tags/v9.9.9" 3
check "tag ref exits 0 (rc=${PROMOTE_RC})" "$([ "${PROMOTE_RC}" == "0" ] && echo 0 || echo 1)"
check "tag ref makes no GHCR or docker calls" "$([ "$(curl_calls)" == "0" ] && [ "$(creates)" == "0" ] && echo 0 || echo 1)"

# 9. Dispatch on another branch pushes that branch's tag unguarded: one
# docker call, zero GHCR reads.
fresh_case dispatch
run_promote "refs/heads/feature/x" 3
check "dispatch branch exits 0 (rc=${PROMOTE_RC})" "$([ "${PROMOTE_RC}" == "0" ] && echo 0 || echo 1)"
check "dispatch branch pushes the sanitized tag once" "$([ "$(creates)" == "1" ] && grep -q -- '-t ghcr.io/eshu-hq/eshu:feature-x ' "${STUB_DIR}/docker.log" && echo 0 || echo 1)"
check "dispatch branch reads nothing" "$([ "$(curl_calls)" == "0" ] && echo 0 || echo 1)"

# 10. An image without a revision label cannot be ancestry-checked: fail
# closed without pushing.
fresh_case nolabel
printf '%s' "${D_NOLABEL}" >"${STUB_DIR}/tags/main"
run_promote "refs/heads/main" 1
check "missing label exits non-zero (rc=${PROMOTE_RC})" "$([ "${PROMOTE_RC}" != "0" ] && echo 0 || echo 1)"
check "missing label pushes nothing" "$([ "$(creates)" == "0" ] && echo 0 || echo 1)"

printf 'test-promote-moving-tags: %d passed, %d failed\n' "${pass}" "${fail}"
[ "${fail}" == "0" ]
