#!/usr/bin/env bash
#
# prepr-stamp-verify.sh — pre-push gate. Refuse to push a commit that
# `make pre-pr` has not validated on this exact SHA.
#
# `make pre-pr` writes a per-SHA stamp under the shared git common dir on
# success; this checks the commit being pushed has one. A rebase or amend after
# pre-pr changes the SHA, so the stamp goes stale and the push is blocked until
# `make pre-pr` is re-run — which is exactly the "pre-pr immediately before push,
# any diff change invalidates it" discipline, enforced instead of trusted. This
# is what makes a green local gate a hard requirement at push time rather than
# relying on CI to first catch a failure.
#
# Escape hatch (rare, explicit): ESHU_ALLOW_UNSTAMPED_PUSH=1 bypasses the check.
# Use it only when you accept that CI becomes the first gate for that push.
set -uo pipefail

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
common_dir="$(git -C "${repo_root}" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "${repo_root}/.git")"
stamp_dir="${common_dir}/eshu-prepr-stamp"

if [[ "${ESHU_ALLOW_UNSTAMPED_PUSH:-0}" == "1" ]]; then
	printf 'prepr-stamp-verify: ESHU_ALLOW_UNSTAMPED_PUSH=1 — skipping the stamp check; CI is the first gate for this push.\n' >&2
	exit 0
fi

# The commit being pushed: pre-commit sets PRE_COMMIT_TO_REF for the pre-push
# stage; fall back to HEAD for a raw or non-pre-commit invocation.
ref="${PRE_COMMIT_TO_REF:-HEAD}"
sha="$(git -C "${repo_root}" rev-parse --verify "${ref}^{commit}" 2>/dev/null || true)"
[[ -n "${sha}" ]] || sha="$(git -C "${repo_root}" rev-parse --verify HEAD 2>/dev/null || true)"
# Nothing resolvable (e.g. a branch delete) — let git proceed.
[[ -n "${sha}" ]] || exit 0

if [[ ! -f "${stamp_dir}/${sha}" ]]; then
	printf 'prepr-stamp-verify: %s is not stamped by make pre-pr — push blocked.\n' "${sha:0:12}" >&2
	printf '  Run:  make pre-pr   then push.\n' >&2
	printf '  A rebase or amend after pre-pr invalidates the stamp — re-run it.\n' >&2
	printf '  Bypass (CI becomes the first gate):  ESHU_ALLOW_UNSTAMPED_PUSH=1 git push ...\n' >&2
	exit 1
fi

deferred="$(sed -n 's/^deferred=//p' "${stamp_dir}/${sha}" 2>/dev/null)"
if [[ -n "${deferred}" ]]; then
	printf 'prepr-stamp-verify: %s stamped; live gates deferred to CI: %s\n' "${sha:0:12}" "${deferred}" >&2
fi
exit 0
