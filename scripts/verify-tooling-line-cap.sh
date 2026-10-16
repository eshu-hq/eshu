#!/usr/bin/env bash
# 500-line file cap for tracked files under scripts/ (issue #8221) -- the
# scripts/ sibling of scripts/verify-markdown-line-cap.sh. See
# scripts/lib/tooling-line-cap-core.sh for the shared implementation and the
# ledger's exact pass/fail rule; scripts/test-verify-tooling-line-cap.sh is its
# test mirror and seeded RED/GREEN proof; specs/ci-gates.d/gates-01.yaml's
# scripts-file-cap entry wires --all in as the local/CI command and --files in
# as the pre-commit hook.
#
# Usage:
#   scripts/verify-tooling-line-cap.sh --all               whole scripts/ tree
#   scripts/verify-tooling-line-cap.sh --files <f> [f...]  changed files
#   scripts/verify-tooling-line-cap.sh --pin <path>        print a ledger row
#
# TOOLING_LINE_CAP_REQUIRE_BASE=1 turns the ledger-growth check's "could not
# resolve a baseline" report from a NOTE into a failure. CI sets it.
#
# TOOLING_LINE_CAP_REPO_ROOT and TOOLING_LINE_CAP_TSV let
# scripts/test-verify-tooling-line-cap.sh point this script at an isolated
# scratch tree and ledger, so the seeded proof exercises this exact CLI entry
# point without planting violations in the real scripts/ tree.
set -euo pipefail

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# The repo root is the parent of this script's own directory, not
# `git rev-parse --show-toplevel`: git exports GIT_DIR to hooks, and with an
# absolute GIT_DIR (every linked worktree) rev-parse reports the directory git
# ran in. See verify-markdown-line-cap.sh's identical comment.
repo_root="${TOOLING_LINE_CAP_REPO_ROOT:-$(cd "${script_root}/.." && pwd)}"

# shellcheck source=lib/tooling-line-cap-core.sh
source "${script_root}/lib/tooling-line-cap-core.sh"

usage() {
	printf 'usage: verify-tooling-line-cap.sh --all|--files <files...>|--pin <repo-relative-path>\n' >&2
}

mode="${1:-}"
[[ $# -gt 0 ]] && shift

case "${mode}" in
	--all)
		exit_status=0
		evaluated=0
		while IFS= read -r path; do
			[[ -n "${path}" ]] || continue
			slcap_skip_path "${path}" && continue
			evaluated=$((evaluated + 1))
			if ! slcap_evaluate_file "${repo_root}" "${path}"; then
				exit_status=1
			fi
		# A git pathspec is not a shell glob: `*` crosses `/`, so this lists
		# every tracked file under scripts/ at any depth.
		done < <(git -C "${repo_root}" ls-files 'scripts/*' | LC_ALL=C sort)
		# Report the count: a run that evaluated nothing exits 0 and reads like
		# "checked everything, all clean" unless the number is printed.
		printf '%s: evaluated %d file(s) under scripts/\n' "${TOOLING_LINE_CAP_NAME}" "${evaluated}"
		if ! slcap_verify_ledger "${repo_root}"; then
			exit_status=1
		fi
		if ! slcap_verify_ledger_growth "${repo_root}"; then
			exit_status=1
		fi
		exit "${exit_status}"
		;;
	--files)
		exit_status=0
		evaluated=0
		for path in "$@"; do
			slcap_skip_path "${path}" && continue
			evaluated=$((evaluated + 1))
			if ! slcap_evaluate_file "${repo_root}" "${path}"; then
				exit_status=1
			fi
		done
		printf '%s: evaluated %d file(s) from %d path(s)\n' \
			"${TOOLING_LINE_CAP_NAME}" "${evaluated}" "$#"
		# The ledger is verified on EVERY invocation so a commit that edits only
		# the ledger or this gate is checked rather than exiting 0 having
		# evaluated nothing.
		if ! slcap_verify_ledger "${repo_root}"; then
			exit_status=1
		fi
		if ! slcap_verify_ledger_growth "${repo_root}"; then
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
		slcap_print_pin "${repo_root}" "${path}"
		;;
	*)
		usage
		exit 2
		;;
esac
