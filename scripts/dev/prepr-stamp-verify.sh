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

# Which commits are being pushed? Git feeds a pre-push hook one update record per
# ref on stdin: "<local_ref> <local_sha> <remote_ref> <remote_sha>". Validate the
# tip of EVERY non-delete ref, so an explicit refspec (git push origin feat:rel)
# or a multi-ref push (git push --all) can't slip an unstamped commit past an
# already-stamped HEAD. When stdin carries no records — pre-commit consumes it and
# exposes PRE_COMMIT_TO_REF instead, or the hook is invoked by hand — fall back to
# that ref, then HEAD.
zero='0000000000000000000000000000000000000000'
shas=()
if [[ ! -t 0 ]]; then
	while read -r _local_ref local_sha _remote_ref _remote_sha; do
		[[ -n "${local_sha:-}" ]] || continue
		[[ "${local_sha}" == "${zero}" ]] && continue   # branch deletion — nothing to stamp
		shas+=("${local_sha}")
	done
fi
if [[ ${#shas[@]} -eq 0 ]]; then
	ref="${PRE_COMMIT_TO_REF:-HEAD}"
	sha="$(git -C "${repo_root}" rev-parse --verify "${ref}^{commit}" 2>/dev/null || true)"
	[[ -n "${sha}" ]] || sha="$(git -C "${repo_root}" rev-parse --verify HEAD 2>/dev/null || true)"
	[[ -n "${sha}" ]] && shas+=("${sha}")
fi
# Nothing resolvable (e.g. only a branch delete) — let git proceed.
[[ ${#shas[@]} -gt 0 ]] || exit 0

# De-duplicate: a multi-ref push can list the same tip twice. (macOS ships bash
# 3.2, which has no `mapfile`, so read the sorted-unique list the portable way.)
uniq_shas=()
while IFS= read -r _s; do
	[[ -n "${_s}" ]] && uniq_shas+=("${_s}")
done < <(printf '%s\n' "${shas[@]}" | sort -u)
shas=("${uniq_shas[@]}")

blocked=0
for sha in "${shas[@]}"; do
	if [[ ! -f "${stamp_dir}/${sha}" ]]; then
		printf 'prepr-stamp-verify: %s is not stamped by make pre-pr — push blocked.\n' "${sha:0:12}" >&2
		blocked=1
		continue
	fi
	deferred="$(sed -n 's/^deferred=//p' "${stamp_dir}/${sha}" 2>/dev/null)"
	if [[ -n "${deferred}" ]]; then
		printf 'prepr-stamp-verify: %s stamped; live gates deferred to CI: %s\n' "${sha:0:12}" "${deferred}" >&2
	fi
done

if [[ "${blocked}" == "1" ]]; then
	printf '  Run:  make pre-pr   then push.\n' >&2
	printf '  A rebase or amend after pre-pr invalidates the stamp — re-run it.\n' >&2
	printf '  Bypass (CI becomes the first gate):  ESHU_ALLOW_UNSTAMPED_PUSH=1 git push ...\n' >&2
	exit 1
fi
exit 0
