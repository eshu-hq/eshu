#!/usr/bin/env bash
#
# verify-agent-canon.sh — fail if the root agent rule canon drifts.
#
# AGENTS.md and CLAUDE.md MUST stay byte-identical: AGENTS.md is read by Codex
# and opencode, CLAUDE.md by Claude Code, and the repo rule requires the two to
# be in lockstep so every harness sees the same rules. This parity was enforced
# only by a local pre-commit hook (bypassable with --no-verify); this script is
# the CI gate so a drifted commit cannot merge.
#
# Exit 0 when identical; non-zero with a unified diff on drift.
set -euo pipefail

repo_root="${ESHU_AGENT_CANON_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  # Derive the repo root from the script's own location (hook- and worktree-safe);
  # git hooks export GIT_DIR, which breaks `git rev-parse --show-toplevel` from a
  # subdirectory. The script always lives at <repo>/scripts/.
  repo_root="$(cd "$(dirname "$0")/.." && pwd)"
fi

agents="$repo_root/AGENTS.md"
claude="$repo_root/CLAUDE.md"

missing=0
for f in "$agents" "$claude"; do
  if [ ! -f "$f" ]; then
    printf 'verify-agent-canon: missing required file %s\n' "$f" >&2
    missing=1
  fi
done
[ "$missing" -eq 0 ] || exit 1

diff_out="$(diff -u "$agents" "$claude" 2>&1 || true)"
if [ -n "$diff_out" ]; then
  printf 'verify-agent-canon: AGENTS.md and CLAUDE.md have drifted.\n' >&2
  printf 'They MUST be byte-identical (the root agent canon is shared across harnesses).\n\n' >&2
  printf '%s\n\n' "$diff_out" >&2
  printf 'Fix: make both files identical, then re-run.\n' >&2
  exit 1
fi

printf 'verify-agent-canon: AGENTS.md and CLAUDE.md are byte-identical.\n'
