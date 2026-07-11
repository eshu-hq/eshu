#!/usr/bin/env bash
#
# verify-agent-canon.sh — fail if shared agent guidance drifts or conflicts.
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

skills_root="$repo_root/.agents/skills"
if [ -d "$skills_root" ]; then
  for skill_file in "$skills_root"/*/SKILL.md; do
    [ -f "$skill_file" ] || continue
    skill_name="$(basename "$(dirname "$skill_file")")"
    for harness in .claude .codex; do
      link="$repo_root/$harness/skills/$skill_name"
      if [ ! -L "$link" ]; then
        printf 'verify-agent-canon: %s cannot discover shared skill %s; missing symlink %s\n' \
          "$harness" "$skill_name" "$link" >&2
        exit 1
      fi
      if [ ! -f "$link/SKILL.md" ] || ! cmp -s "$skill_file" "$link/SKILL.md"; then
        printf 'verify-agent-canon: %s skill link %s does not resolve to %s\n' \
          "$harness" "$link" "$skill_file" >&2
        exit 1
      fi
    done
  done
  printf 'verify-agent-canon: shared skill discovery links are complete.\n'
fi

opencode_agents="$repo_root/.opencode/agent"
if [ -d "$opencode_agents" ]; then
  conflict_pattern='Push over HTTPS|Always .*--no-verify|https://github[.]com/eshu-hq/eshu[.]git'
  if rg -n "$conflict_pattern" "$opencode_agents" >&2; then
    printf 'verify-agent-canon: OpenCode role shim contradicts root Git policy.\n' >&2
    exit 1
  fi
  printf 'verify-agent-canon: OpenCode role shims do not override root Git policy.\n'
fi
