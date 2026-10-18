#!/usr/bin/env bash
# check-agent-env.sh — verify a fresh clone can run Eshu agents (Muse first).
#
# Brand-new-machine contract: cloning eshu-hq/eshu and running `muse` in the
# clone must load the same project rules, skills, and agents as every other
# harness. Everything required is committed (AGENTS.md, .agents/, .muse/,
# .claude/, .codex/, .cursor/, .opencode/); this script proves the clone is
# complete and names any host tool that is missing. It changes nothing.
#
# Required host tools: git, bash, python3. Required for PR/issue work: gh.
# Recommended: rg, pre-commit, go, make. Workspace trust is per-machine Muse
# state and cannot be committed: an untrusted Muse session silently skips
# project hooks (see docs/internal/agent-hooks-muse.md), so the final line
# tells the operator the one manual step that remains.
set -uo pipefail

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$repo_root" || exit 1
fail=0
warn=0

ok() { printf 'OK   %s\n' "$1"; }
bad() { printf 'FAIL %s\n' "$1"; fail=1; }
note() { printf 'WARN %s\n' "$1"; warn=1; }

for tool in git bash python3; do
  if command -v "$tool" >/dev/null 2>&1; then ok "required tool: $tool"; else bad "required tool missing: $tool"; fi
done
for tool in gh rg pre-commit go make muse; do
  if command -v "$tool" >/dev/null 2>&1; then ok "tool: $tool"; else note "tool missing (needed for PR/issue or gate work): $tool"; fi
done

for path in AGENTS.md .agents/roles.json .agents/skills .agents/agents \
  .muse/hooks.json .muse/hooks .claude/settings.json .claude/hooks \
  .claude/skills .codex/hooks.json .cursor/hooks.json .opencode/opencode.jsonc \
  scripts/agent-roles.py scripts/goal-role-router.py scripts/test-muse-hooks.sh; do
  if [[ -e "$path" ]]; then ok "committed path: $path"; else bad "committed path missing (incomplete clone?): $path"; fi
done

skill_count="$(ls .agents/skills/ 2>/dev/null | wc -l | tr -d ' ')"
agent_count="$(ls .agents/agents/*.md 2>/dev/null | wc -l | tr -d ' ')"
[[ "$skill_count" -ge 1 ]] && ok "project skills discoverable: $skill_count" || bad "no project skills under .agents/skills/"
[[ "$agent_count" -ge 1 ]] && ok "Muse role shims discoverable: $agent_count" || bad "no Muse role shims under .agents/agents/"

if command -v python3 >/dev/null 2>&1; then
  if python3 scripts/agent-roles.py check >/dev/null 2>&1; then
    ok "agent role bindings match .agents/roles.json"
  else
    bad "agent role bindings drifted: run python3 scripts/agent-roles.py generate"
  fi
  if python3 -c 'import json; json.load(open(".muse/hooks.json"))' >/dev/null 2>&1; then
    ok ".muse/hooks.json is valid JSON"
  else
    bad ".muse/hooks.json is not valid JSON (Muse loads no project hooks)"
  fi
fi

printf '\ncheck-agent-env: manual per-machine steps that cannot be committed:\n'
printf '  1. Trust this workspace in Muse (or use `muse exec --trust-workspace`),\n'
printf '     then start a NEW session so project hooks/settings reload.\n'
printf '  2. Authenticate gh for this repo path (~/personal-repos/* uses linuxdynasty).\n'
printf '  3. Run scripts/dev/bootstrap-hooks.sh once per clone for git hooks.\n'

if [[ "$fail" -ne 0 ]]; then
  printf 'check-agent-env: FAIL — fix the FAIL lines above; Muse parity is not expected yet.\n' >&2
  exit 1
fi
[[ "$warn" -ne 0 ]] && printf 'check-agent-env: PASS with warnings (tools above are needed only for the named work).\n' || printf 'check-agent-env: PASS\n'
exit 0
