#!/usr/bin/env bash
# Markdown 500-line file cap under go/ and docs/ (issues #6187/#6545) -- the Markdown sibling
# of scripts/verify-dirgate.sh. See scripts/lib/markdown-line-cap-core.sh for
# the shared implementation this script drives and for the ledger's exact
# pass/fail rule; scripts/test-verify-markdown-line-cap.sh is its test mirror
# and BITES proof; specs/ci-gates.v1.yaml's markdown-file-cap entry wires
# --all in as the local/CI command and --files in as the pre-commit hook.
#
# Usage:
#   scripts/verify-markdown-line-cap.sh --all               whole repo tree
#   scripts/verify-markdown-line-cap.sh --files <f> [f...]  changed files
#   scripts/verify-markdown-line-cap.sh --pin <path>        print a ledger row
#
# MARKDOWN_LINE_CAP_REQUIRE_BASE=1 turns the ledger-growth check's "could not
# resolve a baseline" report from a NOTE into a failure. CI sets it (see
# test.yml's "Verify Markdown file cap" step) because a checkout that cannot
# reach its baseline has no anti-self-exemption backstop at all, and exiting 0
# there reads exactly like a pass. Local runs leave it unset, so a clone with
# no origin remote still gets every other check.
#
# MARKDOWN_LINE_CAP_REPO_ROOT and MARKDOWN_LINE_CAP_TSV let
# scripts/test-verify-markdown-line-cap.sh point this script at an isolated
# scratch tree and ledger, so the BITES proof exercises this exact CLI entry
# point rather than a reimplementation of it, without seeding real
# violations into go/.
set -euo pipefail

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# The repo root is the parent of this script's own directory, computed
# directly -- NOT via `git rev-parse --show-toplevel`. git exports GIT_DIR to
# every hook it runs, and with an absolute GIT_DIR (which is what a linked
# worktree has, and CLAUDE.md mandates all work happen in a worktree)
# rev-parse stops discovering the work tree and reports the directory git ran
# in, i.e. `<root>/scripts`. That made the dirgate gate silently pass on
# every directory until it was found; see verify-dirgate.sh's identical
# comment and test_hook_env_git_dir_does_not_blind_the_gate. The same trap is
# live here, so the same computation is used, and
# test_hook_env_git_dir_does_not_blind_the_gate in this gate's own test
# mirror pins it.
repo_root="${MARKDOWN_LINE_CAP_REPO_ROOT:-$(cd "${script_root}/.." && pwd)}"

# shellcheck source=lib/markdown-line-cap-core.sh
source "${script_root}/lib/markdown-line-cap-core.sh"

usage() {
	printf 'usage: verify-markdown-line-cap.sh --all|--files <files...>|--pin <repo-relative-path>\n' >&2
}

mode="${1:-}"
[[ $# -gt 0 ]] && shift

case "${mode}" in
	--all)
		exit_status=0
		evaluated=0
		while IFS= read -r path; do
			[[ -n "${path}" ]] || continue
			mdcap_skip_path "${path}" && continue
			evaluated=$((evaluated + 1))
			if ! mdcap_evaluate_file "${repo_root}" "${path}"; then
				exit_status=1
			fi
		# `go/*.md` IS recursive here. A git pathspec is not a shell glob:
		# `*` crosses `/`, so these list tracked Markdown under go/ and docs/
		# at any depth. See verify-dirgate.sh's identical note; three
		# reviewers have read that pathspec as a depth-1 match and filed it as
		# a bug.
		done < <(git -C "${repo_root}" ls-files 'go/*.md' 'docs/*.md' | LC_ALL=C sort)
		# Report the count for the same reason verify-dirgate.sh --files does:
		# a run that evaluated nothing exits 0 and is indistinguishable from
		# "checked everything, all clean" unless the number is printed.
		printf '%s: evaluated %d Markdown file(s) under go/ and docs/\n' "${MARKDOWN_LINE_CAP_NAME}" "${evaluated}"
		if ! mdcap_verify_ledger "${repo_root}"; then
			exit_status=1
		fi
		# Growth is checked against the committed baseline, not the working
		# tree, because a change that adds a file AND its own pin satisfies
		# every working-tree check above.
		if ! mdcap_verify_ledger_growth "${repo_root}"; then
			exit_status=1
		fi
		exit "${exit_status}"
		;;
	--files)
		exit_status=0
		evaluated=0
		for path in "$@"; do
			mdcap_skip_path "${path}" && continue
			evaluated=$((evaluated + 1))
			if ! mdcap_evaluate_file "${repo_root}" "${path}"; then
				exit_status=1
			fi
		done
		printf '%s: evaluated %d Markdown file(s) from %d path(s)\n' \
			"${MARKDOWN_LINE_CAP_NAME}" "${evaluated}" "$#"
		# The ledger is verified on EVERY invocation, not only under --all.
		# The pre-commit hook is wired to fire on an edit to the ledger
		# itself, and such a commit passes zero go/**.md paths here; without
		# this call it would evaluate 0 files, exit 0, and read exactly like a
		# pass. It is also cheap -- one `awk` per pinned row, 20 rows.
		if ! mdcap_verify_ledger "${repo_root}"; then
			exit_status=1
		fi
		# Growth is checked against the committed baseline, not the working
		# tree, because a change that adds a file AND its own pin satisfies
		# every working-tree check above.
		if ! mdcap_verify_ledger_growth "${repo_root}"; then
			exit_status=1
		fi
		exit "${exit_status}"
		;;
	--pin)
		path="${1:-}"
		if [[ -z "${path}" ]]; then
			usage
			exit 2
		fi
		mdcap_print_pin "${repo_root}" "${path}"
		;;
	*)
		usage
		exit 2
		;;
esac
