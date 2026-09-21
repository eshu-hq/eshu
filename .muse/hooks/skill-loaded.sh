#!/usr/bin/env bash
# .muse/hooks/skill-loaded.sh -- Muse Code PostToolUse hook (read_skill).
#
# Envelope contract, recorded live from `muse exec` (see
# docs/internal/agent-hooks-muse.md): Muse invokes skills through the
# `read_skill` tool with the id at `tool_input.name`, where Claude uses the
# `Skill` tool with `tool_input.skill`. The translation below fills `skill`
# from `name` when it is absent (the sibling also keeps a `name` fallback),
# then delegates to .claude/hooks/skill-loaded.sh so the marker convention
# (`/tmp/claude-skill-loaded-<sid12>-<id>`) stays shared across harnesses.
set -uo pipefail

# shellcheck source=.muse/hooks/lib/muse-root.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib/muse-root.sh"

CLAUDE_HOOK="$(muse_repo_root)/.claude/hooks/skill-loaded.sh"
payload="$(cat)"

translated="$(printf '%s' "${payload}" | python3 -c '
import json, sys
raw = sys.stdin.read()
try:
    d = json.loads(raw)
except Exception:
    print(raw, end="")
    raise SystemExit(0)
ti = d.get("tool_input")
if isinstance(ti, dict) and not ti.get("skill") and ti.get("name"):
    ti["skill"] = ti["name"]
print(json.dumps(d))
' 2>/dev/null)" || translated="${payload}"

printf '%s' "${translated}" | exec "${CLAUDE_HOOK}"
