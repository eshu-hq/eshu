#!/usr/bin/env bash
# pre-push-merge.sh — the merge-tree step of scripts/dev/pre-push.sh (#7111 F5).
#
# Sourced, never executed. Depends on ${repo_root} and ${base} (set by the
# caller) and on `git` >= 2.38 (`git merge-tree --write-tree`).
#
# Why: the rest of the floor tests HEAD. What lands is HEAD merged with the
# base, and the base moves. #7053 compiled on its own head and failed CI's
# merge ref because main had moved a package it imported; #7078 had the same
# failure class. This step computes the exact merge GitHub's merge ref and the
# merge queue will test, and runs `go vet ./...` on it. `go vet` type-checks
# every package and its tests, so it catches every compile error `go build`
# would (except link-only errors) plus test-file breakage, without paying for
# linking every cmd binary (`go build ./...` measured 63-138s warm vs 20-31s for
# `go vet ./...` on this repository).
#
# What it does NOT catch: behavior that changes only when both sides combine
# but still compiles (for example main swapping a type a PR type-asserts on).
# The merge queue's CI run remains the authority for those.
#
# Scope and cost:
#   - HEAD already contains the base (the usual state after a rebase): the
#     merge IS HEAD, which the changed-package build/vet and the whole-module
#     go-vet registry gate already cover, so the step says so and skips.
#   - The merged tree's Go inputs (go/ plus every local `replace` directory
#     named in go/go.mod) are byte-identical to the base's: nothing in the
#     merge that main's own CI has not already vetted, so the step skips.
#   - Otherwise it materializes the merged tree into a stable directory under
#     this worktree's own git dir (removed with `git worktree remove`, never in
#     the worktree's status) and updates it incrementally between runs. The
#     path is stable on purpose: Go's vet cache keys on the source directory,
#     so a fresh temp directory measured 19s where the same directory took 2s.
#
# The committed HEAD is what is merged, because a push sends commits. Any
# uncommitted edit is reported and is not part of the merged tree.
#
# Sets pre_push_merge_tree to the merge tree id and pre_push_merge_summary to
# "<tree> (HEAD <sha> + <base> <sha>)", both empty when no merge was computed.
# The summary is captured when the merge is computed, so a HEAD that moves
# later in the run (a concurrent commit or amend) cannot relabel what was
# vetted.
# shellcheck disable=SC2154  # repo_root and base are set by the sourcing caller.

# shellcheck disable=SC2034  # read by scripts/dev/pre-push.sh's summary.
pre_push_merge_tree=""
# shellcheck disable=SC2034  # read by scripts/dev/pre-push.sh's summary.
pre_push_merge_summary=""

# pre_push_merge_go_inputs prints the tree paths `go vet` reads: go/ and the
# local replace targets go/go.mod names (`replace x => ../sdk/...`), resolved
# to repository-relative paths. Derived from the committed go.mod of <tree>, so
# a new local replace needs no edit here.
pre_push_merge_go_inputs() {
	local tree="$1" line target
	# The target may be quoted ("../sdk" or `../sdk`), which go.mod allows.
	# shellcheck disable=SC2016  # the backtick is a literal go.mod quote character.
	local re='=>[[:space:]]*["`]?\.\./([^[:space:]"`]+)'
	printf 'go\n'
	git -C "${repo_root}" show "${tree}:go/go.mod" 2>/dev/null | while IFS= read -r line; do
		[[ "${line}" =~ ${re} ]] || continue
		target="${BASH_REMATCH[1]}"
		printf '%s\n' "${target%/}"
	done
}

# pre_push_merge_inputs_unchanged <merged-tree> <base-commit> returns 0 when
# every Go input path has the same object id in both trees.
pre_push_merge_inputs_unchanged() {
	local tree="$1" base_commit="$2" path a b
	while IFS= read -r path; do
		a="$(git -C "${repo_root}" rev-parse --verify --quiet "${tree}:${path}")" || a=""
		b="$(git -C "${repo_root}" rev-parse --verify --quiet "${base_commit}^{tree}:${path}")" || b=""
		[[ "${a}" == "${b}" ]] || return 1
	done < <(pre_push_merge_go_inputs "${tree}")
	return 0
}

# pre_push_merge_lock takes a per-worktree lock on the merged-tree directory so
# two pre-push runs in one worktree cannot rewrite it under each other. The lock
# is a symlink whose target is the holder's pid: `ln -s` creates it and records
# the holder in one atomic step, so a reader never sees a lock with no pid. A
# lock left by a dead process is taken over by renaming it aside (only one
# racer's rename of that link succeeds), then checking that what was renamed is
# the dead holder's link and not a fresh lock a faster racer just took; if it
# was fresh it is put back and this run fails closed. A live holder fails the
# step closed. Residual window: a third run arriving between that put-back's
# rename and its restore can take the lock; the lock only serializes two
# developer-invoked runs in one worktree, so this is accepted.
pre_push_merge_lock() {
	local lock="$1" holder claimed got
	# An earlier revision kept the lock as a directory holding a pid file. `ln`
	# would create the new link inside it and report success, so clear one whose
	# holder is gone (a live one is honoured below through the same path).
	if [[ -d "${lock}" && ! -L "${lock}" ]]; then
		holder="$(cat "${lock}/pid" 2>/dev/null || true)"
		if [[ -n "${holder}" ]] && kill -0 "${holder}" 2>/dev/null; then
			printf 'pre-push: another pre-push (pid %s) is using %s; rerun after it finishes.\n' "${holder}" "${lock%.lock}" >&2
			return 1
		fi
		rm -rf "${lock}"
	fi
	holder=""
	if ln -sn "$$" "${lock}" 2>/dev/null; then
		return 0
	fi
	holder="$(readlink "${lock}" 2>/dev/null || true)"
	if [[ -n "${holder}" ]] && kill -0 "${holder}" 2>/dev/null; then
		printf 'pre-push: another pre-push (pid %s) is using %s; rerun after it finishes.\n' "${holder}" "${lock%.lock}" >&2
		return 1
	fi
	claimed="${lock}.stale.$$"
	if mv "${lock}" "${claimed}" 2>/dev/null; then
		got="$(readlink "${claimed}" 2>/dev/null || true)"
		if [[ "${got}" != "${holder}" ]]; then
			# A racing run took the lock between our read and our rename: put
			# it back (ln -s cannot overwrite) and yield.
			[[ -n "${got}" ]] && ln -sn "${got}" "${lock}" 2>/dev/null
			rm -rf "${claimed}"
			printf 'pre-push: another pre-push (pid %s) took %s first; rerun after it finishes.\n' "${got}" "${lock%.lock}" >&2
			return 1
		fi
		rm -rf "${claimed}"
	fi
	if ! ln -sn "$$" "${lock}" 2>/dev/null; then
		printf 'pre-push: another pre-push (pid %s) took %s first; rerun after it finishes.\n' \
			"$(readlink "${lock}" 2>/dev/null || true)" "${lock%.lock}" >&2
		return 1
	fi
}

# step_merge_vet: fail closed unless HEAD merges cleanly with ${base} and the
# merged tree passes `go vet ./...`.
step_merge_vet() {
	local head base_commit out rc tree head_tree git_dir root work idx status=0
	head="$(git -C "${repo_root}" rev-parse --verify 'HEAD^{commit}')" || return 1
	base_commit="$(git -C "${repo_root}" rev-parse --verify "${base}^{commit}")" || return 1
	out="$(git -C "${repo_root}" merge-tree --write-tree --name-only --no-messages "${base_commit}" "${head}" 2>&1)"
	rc=$?
	case "${rc}" in
	0) tree="$(printf '%s\n' "${out}" | sed -n 1p)" ;;
	1)
		printf 'pre-push: HEAD %s does not merge cleanly with %s (%s). Rebase onto it and resolve these paths:\n' \
			"${head:0:12}" "${base}" "${base_commit:0:12}" >&2
		printf '%s\n' "${out}" | sed -n '2,$p' | sed '/^$/d; s/^/  /' >&2
		return 1
		;;
	*)
		printf 'pre-push: cannot compute the merge of HEAD with %s (git merge-tree exit %s): %s\n' "${base}" "${rc}" "${out}" >&2
		if [[ "$(git -C "${repo_root}" rev-parse --is-shallow-repository 2>/dev/null)" == "true" ]]; then
			printf 'pre-push: this clone is shallow, so the merge base may be missing; run "git fetch --deepen=200 origin main" (or "git fetch --unshallow") and rerun.\n' >&2
		fi
		return 1
		;;
	esac
	# shellcheck disable=SC2034  # read by scripts/dev/pre-push.sh's summary.
	pre_push_merge_tree="${tree}"
	# shellcheck disable=SC2034  # read by scripts/dev/pre-push.sh's summary.
	pre_push_merge_summary="${tree} (HEAD ${head:0:12} + ${base} ${base_commit:0:12})"
	head_tree="$(git -C "${repo_root}" rev-parse "${head}^{tree}")"
	if [[ "${tree}" == "${head_tree}" ]]; then
		printf 'HEAD already contains %s (%s): merge tree %s is HEAD'"'"'s own tree, covered by the build/vet above.\n' \
			"${base}" "${base_commit:0:12}" "${tree}"
		return 0
	fi
	if pre_push_merge_inputs_unchanged "${tree}" "${base_commit}"; then
		printf 'merge tree %s: Go inputs are identical to %s (%s); nothing new to vet.\n' \
			"${tree}" "${base}" "${base_commit:0:12}"
		return 0
	fi
	if [[ -n "$(git -C "${repo_root}" status --porcelain --untracked-files=no)" ]]; then
		printf 'note: uncommitted edits are not part of the merged tree; it merges the committed HEAD %s.\n' "${head:0:12}"
	fi
	git_dir="$(git -C "${repo_root}" rev-parse --absolute-git-dir)" || return 1
	root="${git_dir}/eshu-pre-push-merge"
	work="${root}/tree"
	idx="${root}/index"
	mkdir -p "${root}" || return 1
	pre_push_merge_lock "${root}.lock" || return 1
	# A run killed mid read-tree (SIGKILL, OOM) leaves the tree index's own git
	# lock behind, which fails every later read-tree. We hold the exclusive pid
	# lock, so a lock file here cannot belong to a live sibling.
	rm -f "${idx}.lock"
	if [[ ! -f "${idx}" ]]; then
		rm -rf "${work}"
	fi
	mkdir -p "${work}"
	if ! GIT_INDEX_FILE="${idx}" git -C "${repo_root}" --work-tree="${work}" read-tree --reset -u "${tree}"; then
		rm -rf "${work}" "${idx}" "${root}.lock"
		printf 'pre-push: could not materialize merge tree %s into %s\n' "${tree}" "${work}" >&2
		return 1
	fi
	printf 'vetting merge tree %s = HEAD %s + %s %s (in %s)\n' \
		"${tree}" "${head:0:12}" "${base}" "${base_commit:0:12}" "${work}"
	( cd "${work}/go" && go vet ./... ) || status=1
	rm -rf "${root}.lock"
	return "${status}"
}
