#!/usr/bin/env bash
# Directory-size and naming gate (issue #6054): a thin wrapper over go/cmd/dirgate,
# which holds the one implementation of the rule. The cap is a ratchet derived
# from git each run, plus two write-once baselines. See go/cmd/dirgate/README.md.
#
# Usage:
#   scripts/verify-dirgate.sh --ci                     merge ref vs its first parent (HEAD^1)
#   scripts/verify-dirgate.sh --pre-push               origin/main merged with HEAD vs origin/main
#   scripts/verify-dirgate.sh --pre-commit <files...>  index vs merge base, changed directories only
#   scripts/verify-dirgate.sh --all                    --ci under GitHub Actions, else --pre-push
#
# ESHU_DIRGATE_BASE (or ESHU_PRE_PUSH_BASE) names a comparison ref other than
# origin/main for --pre-push and --pre-commit. GITHUB_EVENT_NAME picks the CI
# behaviour: every event enforces, pull_request needs the merge ref.
#
# The repo root is the parent of this script's own directory, computed directly.
# Asking git for it is not hook-safe: git exports GIT_DIR to every hook it runs,
# and with GIT_DIR set `git -C scripts rev-parse --show-toplevel` reports the
# directory git ran in, not the repository root.
set -euo pipefail

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="${DIRGATE_REPO_ROOT:-$(cd "${script_root}/.." && pwd)}"

usage() {
	printf 'usage: verify-dirgate.sh --ci|--pre-push|--pre-commit <files...>|--all\n' >&2
}

mode="${1:-}"
[[ $# -gt 0 ]] && shift

case "${mode}" in
	--all)
		if [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
			tool_mode="ci"
		else
			tool_mode="pre-push"
		fi
		;;
	--ci) tool_mode="ci" ;;
	--pre-push) tool_mode="pre-push" ;;
	--pre-commit) tool_mode="pre-commit" ;;
	*)
		usage
		exit 2
		;;
esac

export ESHU_DIRGATE_BASE="${ESHU_DIRGATE_BASE:-${ESHU_PRE_PUSH_BASE:-}}"

# `go run` from go/ builds the tool from the module's own sources; GOROOT is
# unset because a stale exported GOROOT breaks the toolchain.
cd "${repo_root}/go"
exec env -u GOROOT go run ./cmd/dirgate -repo "${repo_root}" "${tool_mode}" "$@"
