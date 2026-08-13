#!/usr/bin/env bash
#
# Verify concrete ESHU_* citations and long flags in fenced Eshu shell commands.
#
# Precision-first flag scope: bash/sh/shell/console fences only; logical lines
# beginning with `eshu` (optionally after `$` or `>`); concrete long flags only.
# Prose, inline code, non-shell fences, and logical lines containing an
# unquoted shell-list operator (`|`, `&`, or `;`) are deliberately skipped,
# as are short flags, shell-expanded flag names, command-local flags after a
# leading root flag, and wildcard ESHU_* family prefixes.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
docs_root="${ESHU_DOCS_CLI_ENV_DOCS_ROOT:-${repo_root}/docs/public}"
baseline="${ESHU_DOCS_CLI_ENV_BASELINE_PATH:-${repo_root}/scripts/docs-cli-env-refs-baseline.txt}"
gocache="${ESHU_DOCS_CLI_ENV_GOCACHE:-${repo_root}/.gocache-docs-cli-env-refs}"
eshu_binary="${ESHU_DOCS_CLI_ENV_ESHU_BINARY:-}"
checker_binary="${ESHU_DOCS_CLI_ENV_CHECKER_BINARY:-}"
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
  -eshu "${eshu_binary}"
)
if [[ "${update}" == true ]]; then
  args+=(-update)
fi
"${checker_binary}" "${args[@]}"
