#!/usr/bin/env bash
# .claude/hooks/eshu-doc-staleness.sh
#
# Claude Code PostToolUse hook (matcher: Edit|MultiEdit|Write).
# Drains the stdin payload (we don't need it) and delegates to
# scripts/check-docs-stale.sh --all so Claude Code and Codex converge on the
# same .eshu-doc-state/stale.jsonl snapshot. The script is fast (stat-based)
# and --all rebuilds the snapshot from scratch each run, so duplicate appends
# do not accumulate.

set -u
set -o pipefail

# Drain stdin so Claude Code doesn't block on a closed pipe.
cat >/dev/null

repo_root="${CLAUDE_PROJECT_DIR:-}"
[ -z "$repo_root" ] && repo_root="$(git rev-parse --show-toplevel 2>/dev/null \
  || (cd "$(dirname "$0")/../.." && pwd))"

# --all rebuilds the full drift snapshot. Same call shape as the Codex hook so
# both tools converge on the same .eshu-doc-state/stale.jsonl content.
ESHU_DOC_KEEPER_TOOL=claude-code \
  "$repo_root/scripts/check-docs-stale.sh" --all

exit 0
