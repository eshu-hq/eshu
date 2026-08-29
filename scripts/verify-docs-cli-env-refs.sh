#!/usr/bin/env bash
#
# Verify concrete ESHU_* citations and long flags in fenced Eshu shell commands.
#
# Precision-first flag scope: bash/sh/shell/console fences only; command
# segments beginning with `eshu`, optionally after a console prompt (`$` or
# `>`), `NAME=value` environment assignments, or `sudo`; concrete long flags
# only. Prose, inline code, non-shell fences, short flags, shell-expanded flag
# names, command-local flags after a leading root flag, and wildcard ESHU_*
# family prefixes are deliberately skipped.
#
# Since #6230 the prefixes above are stripped to FIND the command, not to
# rename it: `sudo eshu docs verify --stale` is attributed to
# `eshu docs verify`, while `sudo docker compose logs eshu` is still a non-Eshu
# command and stays out of scope. Only a bare `sudo` is stripped, so
# `sudo -u builder eshu ...` keeps its option word and stays out of scope
# rather than being guessed at.
#
# Since #6108 a logical line may be a simple list: segments separated by an
# unquoted `|`, `&&`, or `;`, each checked against its own command so one
# command's flags are never attributed to another. Any other shell form on the
# line -- `||`, a background `&`, `|&`, `;;`, a subshell, or command
# substitution -- keeps the whole line outside the gate's scope, and so does an
# empty segment. A subshell or substitution excludes the line whether or not it
# carries a list operator. Operators inside quotes, after a backslash, or in a trailing
# `#` comment are not segment boundaries.
#
# Every run prints how many command segments it attributed and how many `eshu`
# command lines it skipped that way, zero included, and asserts both: the skip
# count is pinned exactly in each direction, the attributed count has a floor.
# A scanner that quietly stopped reading shell fences fails here instead of
# reporting a clean run over a shrunken population.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
docs_root="${ESHU_DOCS_CLI_ENV_DOCS_ROOT:-${repo_root}/docs/public}"
baseline="${ESHU_DOCS_CLI_ENV_BASELINE_PATH:-${repo_root}/scripts/docs-cli-env-refs-baseline.txt}"
ceiling="${ESHU_DOCS_CLI_ENV_BASELINE_CEILING_PATH:-${repo_root}/scripts/docs-cli-env-refs-ceiling.txt}"
gocache="${ESHU_DOCS_CLI_ENV_GOCACHE:-${repo_root}/.gocache-docs-cli-env-refs}"
eshu_binary="${ESHU_DOCS_CLI_ENV_ESHU_BINARY:-}"
checker_binary="${ESHU_DOCS_CLI_ENV_CHECKER_BINARY:-}"
# Empty means "use the checker's code-owned pin/floor". Only the companion
# suite sets these, so the real gate always runs against the pinned values.
pinned_skipped="${ESHU_DOCS_CLI_ENV_PINNED_SKIPPED_LINES:-}"
min_attributed="${ESHU_DOCS_CLI_ENV_MIN_ATTRIBUTED_SEGMENTS:-}"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

update=false
case "${1:-}" in
  "") ;;
  -update) update=true ;;
  -h | --help)
    printf 'usage: %s [-update]\n' "${0##*/}"
    exit 0
    ;;
  *) printf 'verify-docs-cli-env-refs: unknown argument: %s\n' "$1" >&2; exit 2 ;;
esac

if [[ -z "${eshu_binary}" ]]; then
  eshu_binary="${tmp_dir}/eshu"
  GOCACHE="${gocache}-eshu" go -C "${repo_root}/go" build -o "${eshu_binary}" ./cmd/eshu
fi

if [[ -z "${checker_binary}" ]]; then
  checker_binary="${tmp_dir}/docs-cli-env-refs"
  GOCACHE="${gocache}-checker" go -C "${repo_root}/go" build \
    -o "${checker_binary}" ./cmd/docs-cli-env-refs
fi

args=(
  -docs-root "${docs_root}"
  -baseline "${baseline}"
  -baseline-ceiling "${ceiling}"
  -eshu "${eshu_binary}"
)
if [[ -n "${pinned_skipped}" ]]; then
  args+=(-pinned-skipped-lines "${pinned_skipped}")
fi
if [[ -n "${min_attributed}" ]]; then
  args+=(-min-attributed-segments "${min_attributed}")
fi
if [[ "${update}" == true ]]; then
  args+=(-update)
fi
"${checker_binary}" "${args[@]}"
