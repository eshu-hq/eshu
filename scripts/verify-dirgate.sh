#!/usr/bin/env bash
# Directory-size and naming gate (issue #6054) -- the bash mirror of the
# tools/golangci-lint-dirgate Go plugin. See scripts/lib/dirgate-core.sh
# for the shared implementation this script drives; scripts/test-verify-
# dirgate.sh is its test mirror and BITES proof; specs/ci-gates.v1.yaml's
# go-dir-gate entry wires --all in as the local/CI command.
#
# Usage:
#   scripts/verify-dirgate.sh --all               whole repo tree
#   scripts/verify-dirgate.sh --files <f> [f...]  changed files (pre-commit)
#   scripts/verify-dirgate.sh --digest <dir>      print count+digest for one
#                                                  go/-relative directory
# DIRGATE_REPO_ROOT and DIRGATE_GO_DIR let scripts/test-verify-dirgate.sh
# point this script at an isolated scratch tree instead of the real repo,
# so the BITES proof exercises this exact CLI entry point (not a
# reimplementation of it) without seeding real violations into go/.
set -euo pipefail

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# The repo root is the parent of this script's own directory, computed
# directly. Asking git for it -- `git -C "${script_root}" rev-parse
# --show-toplevel` -- looked more robust and was not: git exports GIT_DIR to
# every hook it runs, and with GIT_DIR set rev-parse stops discovering the
# work tree and just reports the directory git ran in, i.e. `<root>/scripts`.
# go_dir then became `<root>/scripts/go`, which does not exist, so
# dirgate_evaluate_dir treated every directory as "nothing to check" and the
# gate passed. A relative GIT_DIR (a normal clone) made that call FAIL, and
# the old `||` fallback -- this same parent-of-script_root path -- recovered;
# an ABSOLUTE GIT_DIR (a linked worktree, which CLAUDE.md mandates all work
# happen in) made it SUCCEED and lie, so `git commit` reported the gate green
# on a tree `pre-commit run` failed. Pinned by
# test_hook_env_git_dir_does_not_blind_the_gate in
# scripts/lib/test-verify-dirgate-misc-cases.sh.
repo_root="${DIRGATE_REPO_ROOT:-$(cd "${script_root}/.." && pwd)}"
go_dir="${DIRGATE_GO_DIR:-${repo_root}/go}"

# shellcheck source=lib/dirgate-core.sh
source "${script_root}/lib/dirgate-core.sh"

usage() {
	printf 'usage: verify-dirgate.sh --all|--files <files...>|--digest <go/-relative-dir>\n' >&2
}

mode="${1:-}"
[[ $# -gt 0 ]] && shift

case "${mode}" in
	--all)
		exit_status=0
		while IFS= read -r dirkey; do
			[[ -n "${dirkey}" ]] || continue
			dirgate_skip_dir "${dirkey}" && continue
			if ! dirgate_evaluate_dir "${dirkey}" "${go_dir}/${dirkey}"; then
				exit_status=1
			fi
		# `go/*.go` IS recursive here. A git pathspec is not a shell glob: `*`
		# crosses `/`, so this lists every tracked .go file under go/ at any
		# depth. Measured on this tree: 12335 matches, 0 of them at depth 1,
		# reaching go/internal/collector/awscloud/acm_types.go. Three reviewers
		# have read this as a depth-1 match and filed it as a bug, so: it is
		# not one, and `go/**/*.go` would be equivalent, not a fix.
		# The awk below then reduces each file path to its directory.
		done < <(git -C "${repo_root}" ls-files 'go/*.go' \
			| sed -E 's#^go/##' \
			| awk -F/ 'NF>1 { d=$1; for (i=2;i<NF;i++) d=d"/"$i; print d }' \
			| LC_ALL=C sort -u)
		dirgate_report_removable_grandfathers "${go_dir}"
		if ! dirgate_verify_naming_exempt_ledger "${go_dir}"; then
			exit_status=1
		fi
		exit "${exit_status}"
		;;
	--files)
		exit_status=0
		# Count what was actually evaluated. Zero is legitimate -- a commit
		# touching only .go paths outside go/ (tools/, or the generated
		# grandfather.go itself) maps to no package directory -- but it is
		# indistinguishable from "checked everything, all clean" unless the
		# count is reported. A run that evaluated nothing has been cited as
		# proof a row was correct; printing the number is what makes that
		# citation self-refuting.
		evaluated=0
		while IFS= read -r dirkey; do
			[[ -n "${dirkey}" ]] || continue
			dirgate_skip_dir "${dirkey}" && continue
			evaluated=$((evaluated + 1))
			if ! dirgate_evaluate_dir "${dirkey}" "${go_dir}/${dirkey}"; then
				exit_status=1
			fi
		done < <(dirgate_changed_dirs "$@")
		printf 'dirgate: evaluated %d director%s from %d path(s)\n' \
			"${evaluated}" "$([[ ${evaluated} -eq 1 ]] && printf 'y' || printf 'ies')" "$#"
		exit "${exit_status}"
		;;
	--digest)
		dir="${1:-}"
		if [[ -z "${dir}" ]]; then
			usage
			exit 2
		fi
		dirgate_print_digest "${go_dir}/${dir}"
		;;
	*)
		usage
		exit 2
		;;
esac
