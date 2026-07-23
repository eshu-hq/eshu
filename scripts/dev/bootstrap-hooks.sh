#!/usr/bin/env bash
#
# bootstrap-hooks.sh — idempotently install this clone's pre-commit hooks.
#
# Worktrees share the clone's .git/hooks, so running this once from any worktree
# installs the hooks for every worktree of the clone. Safe to re-run.
#
# Honest limit: git cannot run hooks on a fresh clone without one explicit
# install step, and any hook is bypassable with --no-verify. The non-bypassable
# gate is CI (.github/workflows/verify-agent-hygiene.yml and friends). These
# local hooks are fast feedback so failures surface at commit time, not on the PR.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$repo_root"

if ! command -v pre-commit >/dev/null 2>&1; then
  printf 'bootstrap-hooks: pre-commit is not installed. Install it, then re-run:\n' >&2
  printf '  pip install pre-commit   # or: brew install pre-commit\n' >&2
  exit 1
fi

# pre-commit install is idempotent — re-running is a no-op if already current.
pre-commit install --install-hooks
pre-commit install --hook-type pre-push
pre-commit install --hook-type commit-msg

printf 'bootstrap-hooks: hooks installed for this clone (commit, pre-push, commit-msg).\n'
printf 'bootstrap-hooks: NEVER --no-verify a commit or a push. The pre-push gate is a fast\n'
printf 'bootstrap-hooks: pre-pr stamp check: run `make pre-pr` (it stamps the SHA on success),\n'
printf 'bootstrap-hooks: then push. Sanctioned bypass only: ESHU_ALLOW_UNSTAMPED_PUSH=1.\n'
