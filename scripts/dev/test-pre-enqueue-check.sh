#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Hermetic self-test for scripts/dev/pre-enqueue-check.sh (#7332). No GitHub,
# no network: a fake `gh` on PATH (scripts/lib/test-pre-enqueue-check-fake-gh.sh)
# serves per-case JSON fixtures, and a local bare repository stands in for
# origin so the merge-tree arms run real `git merge-tree` over real conflicts,
# fetching the head and queue-tip commits by SHA the way they are fetched from
# GitHub.
#
# One all-green case must exit 0; each seeded RED must exit 1 with exactly one
# FAIL line, naming the arm it seeds.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
target="${repo_root}/scripts/dev/pre-enqueue-check.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

for tool in jq rg git; do
	command -v "${tool}" >/dev/null || {
		echo "test-pre-enqueue-check: ${tool} is required" >&2
		exit 1
	}
done
[[ -x "${target}" ]] || {
	echo "test-pre-enqueue-check: missing executable ${target}" >&2
	exit 1
}

# Isolate git from the caller: hooks export GIT_DIR into worktrees, and user
# config (signing, hooksPath) must not leak into the fixture repositories.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.invalid
export GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.invalid

pass=0
fail=0
check() {
	if [[ "$2" -eq 0 ]]; then
		printf 'PASS: %s\n' "$1"
		pass=$((pass + 1))
	else
		printf 'FAIL: %s\n' "$1"
		fail=$((fail + 1))
	fi
}

# --- git fixture -------------------------------------------------------------
g() { git -C "$1" "${@:2}" >/dev/null 2>&1; }
seed="${work}/seed"
git init -q -b main "${seed}"
printf 'a1\n' >"${seed}/a.txt"
printf 'b1\n' >"${seed}/b.txt"
g "${seed}" add -A && g "${seed}" commit -qm base
# commit_on <branch> <file> <content>: one commit on a branch cut from base.
commit_on() {
	g "${seed}" checkout -q -B "$1" main
	printf '%s\n' "$3" >"${seed}/$2"
	g "${seed}" commit -qam "$1"
	git -C "${seed}" rev-parse HEAD
}
HEAD_SHA="$(commit_on feature a.txt head)"
TIP_OK="$(commit_on q-ok b.txt tip)"
TIP_BAD="$(commit_on q-bad a.txt tip)"
g "${seed}" checkout -q main
new_origin() {
	git clone -q --bare "${seed}" "$1"
	git -C "$1" config uploadpack.allowAnySHA1InWant true
}
new_origin "${work}/origin.git"
# origin-conflict.git: main moved on and now conflicts with the PR head.
new_origin "${work}/origin-conflict.git"
g "${seed}" checkout -q -B main-moved main
printf 'main2\n' >"${seed}/a.txt"
g "${seed}" commit -qam moved
g "${seed}" push -q "${work}/origin-conflict.git" main-moved:main

# --- fake gh and case fixtures ----------------------------------------------
export FAKE_PR=100 PRE_ENQUEUE_REPO=eshu-hq/eshu
mkdir -p "${work}/bin"
cp "${repo_root}/scripts/lib/test-pre-enqueue-check-fake-gh.sh" "${work}/bin/gh"
chmod +x "${work}/bin/gh"
export PATH="${work}/bin:${PATH}"

# new_case <name> [origin]: green fixtures plus a fresh single-branch clone,
# so the head and queue tip are absent locally and must be fetched by SHA.
new_case() {
	FIXTURES="${work}/case-$1"
	CLONE="${FIXTURES}/clone"
	export FIXTURES
	mkdir -p "${FIXTURES}"
	git clone -q --single-branch --branch main "${2:-${work}/origin.git}" "${CLONE}"
	jq -n --arg h "${HEAD_SHA}" '{state:"OPEN",isDraft:false,headRefOid:$h,
		mergeStateStatus:"CLEAN",body:"Tightens the gate.\n\nCloses #7332\n",
		files:[{path:"a.txt"},{path:"docs/x.md"}]}' >"${FIXTURES}/pr.json"
	jq -n --arg t "${TIP_OK}" '{data:{repository:{mergeQueue:{entries:{
		pageInfo:{hasNextPage:false},
		nodes:[{position:1,headCommit:{oid:"0000000000000000000000000000000000000001"},pullRequest:{number:201}},
		       {position:2,headCommit:{oid:$t},pullRequest:{number:202}}]}}}}}' >"${FIXTURES}/queue.json"
	jq -n '{files:[{path:"docs/x.md"}]}' >"${FIXTURES}/files-201.json"
	jq -n '{files:[{path:"b.txt"}]}' >"${FIXTURES}/files-202.json"
	# A passing check NAMED like a failure proves counting uses the state
	# bucket, not the line text.
	jq -n '[{name:"lint",state:"SUCCESS",bucket:"pass"},
		{name:"fail-closed-guard pending-rows",state:"SUCCESS",bucket:"pass"},
		{name:"docs",state:"SKIPPED",bucket:"skipping"}]' >"${FIXTURES}/checks.json"
	jq -n '{state:"success",statuses:[{context:"required-gates-complete",state:"success"},
		{context:"go-core-complete",state:"success"}]}' >"${FIXTURES}/status.json"
	jq -n '{data:{repository:{pullRequest:{reviewThreads:{totalCount:2,
		pageInfo:{hasNextPage:false},nodes:[{isResolved:true},{isResolved:true}]}}}}}' >"${FIXTURES}/threads.json"
}
# edit <fixture> <jq-filter> [jq args...]
edit() {
	local f="${FIXTURES}/$1" filter="$2"
	shift 2
	jq "$@" "${filter}" "${f}" >"${f}.tmp" && mv "${f}.tmp" "${f}"
}
run() {
	set +e
	OUT="$(cd "${CLONE}" && "${target}" "${FAKE_PR}" "${1:-${HEAD_SHA}}" 2>&1)"
	RC=$?
	set -e
	printf '%s\n' "${OUT}" >"${FIXTURES}/out.txt"
}
fail_lines() { rg -c '^FAIL ' <<<"${OUT}" || echo 0; }
# expect_red <desc> <arm>: exit 1, exactly one FAIL line, and it names <arm>.
expect_red() {
	run "${3:-}"
	local ok=0
	[[ "${RC}" -eq 1 ]] || ok=1
	[[ "$(fail_lines)" == 1 ]] || ok=1
	rg -q "^FAIL $2 " <<<"${OUT}" || ok=1
	rg -q "^pre-enqueue-check: FAIL .*\(failed: $2\)$" <<<"${OUT}" || ok=1
	check "RED ${1} -> FAIL ${2}" "${ok}"
	printf '  %s\n' "$(rg "^FAIL " <<<"${OUT}" || echo "(no FAIL line; rc=${RC})")"
	[[ "${ok}" -eq 0 ]] || printf '%s\n' "${OUT}" | sed 's/^/    | /'
}

# --- GREEN -------------------------------------------------------------------
new_case green
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
[[ "$(fail_lines)" == 0 ]] || ok=1
[[ "$(rg -c '^PASS ' <<<"${OUT}")" == 6 ]] || ok=1
rg -q "clean against queue tip ${TIP_OK} \(#202\)" <<<"${OUT}" || ok=1
rg -q 'queued #201 \(position 1\) file overlap: docs/x.md$' <<<"${OUT}" || ok=1
rg -q 'queued #202 \(position 2\) file overlap: none$' <<<"${OUT}" || ok=1
rg -q 'closing keywords: Closes #7332' <<<"${OUT}" || ok=1
check "GREEN all six arms pass, exit 0" "${ok}"
printf '%s\n' "${OUT}" | sed 's/^/    | /'
ok=0
if rg -v '^(pr view|pr checks|api graphql|api repos/eshu-hq/eshu/commits/[0-9a-f]{40}/status)' \
	"${FIXTURES}/calls.log" >/dev/null; then ok=1; fi
check "GREEN issues only read calls to gh" "${ok}"

new_case empty-queue
edit queue.json '.data.repository.mergeQueue.entries.nodes = []'
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
rg -q '^PASS merge-queue +queue empty$' <<<"${OUT}" || ok=1
check "GREEN empty queue passes with 'queue empty'" "${ok}"

new_case self-queued
edit queue.json '.data.repository.mergeQueue.entries.nodes = [{position:1,headCommit:{oid:"x"},pullRequest:{number:100}}]'
run
ok=0
[[ "${RC}" -eq 0 ]] || ok=1
rg -q 'PR #100 is already queued' <<<"${OUT}" || ok=1
rg -q '^PASS merge-queue +queue empty$' <<<"${OUT}" || ok=1
check "GREEN a queue holding only this PR counts as empty" "${ok}"

# --- seeded REDs, one per arm ------------------------------------------------
new_case head-mismatch
expect_red "head moved past the reviewed SHA" pr-state "${TIP_OK}"
new_case draft
edit pr.json '.isDraft = true'
expect_red "draft PR" pr-state
new_case closed
edit pr.json '.state = "MERGED"'
expect_red "PR not open" pr-state
new_case main-conflict "${work}/origin-conflict.git"
expect_red "merge-tree conflict vs origin/main" merge-main
new_case queue-conflict
edit queue.json '.data.repository.mergeQueue.entries.nodes[1].headCommit.oid = $t' --arg t "${TIP_BAD}"
expect_red "merge-tree conflict vs queue tip" merge-queue
new_case queue-tip-missing
edit queue.json '.data.repository.mergeQueue.entries.nodes[1].headCommit.oid = "1111111111111111111111111111111111111111"'
expect_red "queue tip not fetchable" merge-queue
new_case pending
edit checks.json '. + [{name:"go-race",state:"IN_PROGRESS",bucket:"pending"}]'
printf '8\n' >"${FIXTURES}/checks.rc"
expect_red "pending check" checks
new_case failing
edit checks.json '. + [{name:"go-core",state:"FAILURE",bucket:"fail"}]'
printf '1\n' >"${FIXTURES}/checks.rc"
expect_red "failing check" checks
new_case cancelled
edit checks.json '. + [{name:"e2e",state:"CANCELLED",bucket:"cancel"}]'
expect_red "cancelled check" checks
new_case no-checks
edit checks.json '[]'
expect_red "no check rows" checks
new_case rgc-pending
edit status.json '.statuses[0].state = "pending"'
expect_red "required-gates-complete pending" checks
new_case rgc-absent
edit status.json '.statuses |= map(select(.context != "required-gates-complete"))'
expect_red "required-gates-complete absent" checks
new_case not-clean
edit pr.json '.mergeStateStatus = "BLOCKED"'
expect_red "mergeStateStatus BLOCKED" checks
new_case unresolved
edit threads.json '.data.repository.pullRequest.reviewThreads.nodes[1].isResolved = false'
expect_red "unresolved review thread" threads
new_case threads-truncated
edit threads.json '.data.repository.pullRequest.reviewThreads.pageInfo.hasNextPage = true'
expect_red "more threads than one page" threads
new_case no-closing
edit pr.json '.body = "Tightens the gate. Refs #7332\n"'
expect_red "missing closing keyword" body
new_case attribution
edit pr.json '.body += "\nGenerated with a coding assistant\n"'
expect_red "AI attribution in body" body
new_case coauthor
edit pr.json '.body += "\nCo-Authored-By: someone <x@example.invalid>\n"'
expect_red "Co-Authored-By trailer in body" body

# --- usage -------------------------------------------------------------------
set +e
"${target}" 100 abc123 >/dev/null 2>&1
rc=$?
set -e
check "short SHA is a usage error (exit 2)" "$([[ "${rc}" -eq 2 ]] && echo 0 || echo 1)"

printf '\ntest-pre-enqueue-check: %d passed, %d failed\n' "${pass}" "${fail}"
[[ "${fail}" -eq 0 ]]
