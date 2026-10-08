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
cp "${repo_root}/scripts/fixtures/promote-moving-tags-stub-curl.sh" "${work}/bin/curl"

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

# Fake git (fixtures/promote-moving-tags-stub-git.sh): merge-base
# --is-ancestor over ${STUB_DIR}/ancestors ("child parent" per line,
# transitive), exiting 128 for unknown objects like real git; fetch appends
# ${STUB_DIR}/fetch-add when a case provides it; cat-file -e reports
# existence. Anything else is an error. The stub lives in a fixture file
# because its body exceeds the heredoc budget (#5074).
cp "${repo_root}/scripts/fixtures/promote-moving-tags-stub-git.sh" "${work}/bin/git"
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

# 5. Unrelated history fails loudly without pushing. Both commits are known
# to the clone (siblings under SHA_OLD) so this case proves the truly
# unrelated branch, not the unknown-object path.
fresh_case unrelated
printf '%s' "${D_X}" >"${STUB_DIR}/tags/main"
printf '%s' "${SHA_X}" >"${STUB_DIR}/revs/$(san "${D_X}")"
printf '%s %s\n%s %s\n' "${MY_SHA}" "${SHA_OLD}" "${SHA_X}" "${SHA_OLD}" >"${STUB_DIR}/ancestors"
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

# 11. Out-of-order completion: main already carries a commit this shallow
# clone has never seen. The refresh fetch resolves it, the next ancestry
# check sees the descendant, and the run skips green instead of refusing.
fresh_case staleclone
printf '%s' "${D_NEWER}" >"${STUB_DIR}/tags/main"
printf '%s' "${D_NEWER}" >"${STUB_DIR}/tags/latest"
printf '%s' "${SHA_NEWER}" >"${STUB_DIR}/revs/$(san "${D_NEWER}")"
printf '%s %s\n' "${MY_SHA}" "${SHA_OLD}" >"${STUB_DIR}/ancestors"
printf '%s %s\n' "${SHA_NEWER}" "${MY_SHA}" >"${STUB_DIR}/fetch-add"
run_promote "refs/heads/main" 3
check "stale clone exits 0 (rc=${PROMOTE_RC})" "$([ "${PROMOTE_RC}" == "0" ] && echo 0 || echo 1)"
check "stale clone pushes nothing" "$([ "$(creates)" == "0" ] && echo 0 || echo 1)"
check "stale clone refreshed before deciding" "$(grep -q '^git fetch' "${STUB_DIR}/git.log" && echo 0 || echo 1)"
check "stale clone reports the newer commit" "$(printf '%s' "${PROMOTE_OUTPUT}" | grep -q 'newer commit' && echo 0 || echo 1)"

# 12. A commit that never resolves is a transient fetch problem, not proof
# of unrelated history: the attempts exhaust as non-convergence.
fresh_case ghost
printf '%s' "${D_X}" >"${STUB_DIR}/tags/main"
printf '%s' "${SHA_X}" >"${STUB_DIR}/revs/$(san "${D_X}")"
printf '%s %s\n' "${MY_SHA}" "${SHA_OLD}" >"${STUB_DIR}/ancestors"
run_promote "refs/heads/main" 2
check "ghost main exits non-zero (rc=${PROMOTE_RC})" "$([ "${PROMOTE_RC}" != "0" ] && echo 0 || echo 1)"
check "ghost main pushes nothing" "$([ "$(creates)" == "0" ] && echo 0 || echo 1)"
check "ghost main reports non-convergence" "$(printf '%s' "${PROMOTE_OUTPUT}" | grep -q 'did not converge' && echo 0 || echo 1)"
check "ghost main never claims unrelated" "$(printf '%s' "${PROMOTE_OUTPUT}" | grep -q 'unrelated' && echo 1 || echo 0)"

printf 'test-promote-moving-tags: %d passed, %d failed\n' "${pass}" "${fail}"
[ "${fail}" == "0" ]
