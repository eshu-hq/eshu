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
repo_root="${DIRGATE_REPO_ROOT:-$(git -C "${script_root}" rev-parse --show-toplevel 2>/dev/null || printf '%s\n' "${script_root}/..")}"
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
		while IFS= read -r dirkey; do
			[[ -n "${dirkey}" ]] || continue
			dirgate_skip_dir "${dirkey}" && continue
			if ! dirgate_evaluate_dir "${dirkey}" "${go_dir}/${dirkey}"; then
				exit_status=1
			fi
		done < <(dirgate_changed_dirs "$@")
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
