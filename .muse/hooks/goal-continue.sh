#!/usr/bin/env bash
# .muse/hooks/goal-continue.sh -- Muse Code Stop hook (no matcher).
#
# Envelope contract, recorded live from `muse exec` (see
# docs/internal/agent-hooks-muse.md): the Muse Stop payload carries the same
# keys as Claude's except the budget key, which is `turn_id` instead of
# `prompt_id`. The translation below fills `prompt_id` from `turn_id` when it
# is absent, then delegates to .claude/hooks/goal-continue.sh so one logic
# copy serves both harnesses. `turn_id` is stable across the stops of one
# turn (proven: consecutive stops share it, `stop_hook_active` flips true),
# so per-turn budget semantics are preserved.
#
# Fail-open is inherited: without python3 the payload passes through raw and
# the sibling exits 0 on its own guard.
set -uo pipefail

# shellcheck source=.muse/hooks/lib/muse-root.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib/muse-root.sh"

CLAUDE_HOOK="$(muse_repo_root)/.claude/hooks/goal-continue.sh"
payload="$(cat)"

translated="$(printf '%s' "${payload}" | python3 -c '
import json, sys
raw = sys.stdin.read()
try:
    d = json.loads(raw)
except Exception:
    print(raw, end="")
    raise SystemExit(0)
if not d.get("prompt_id") and d.get("turn_id"):
    d["prompt_id"] = d["turn_id"]
print(json.dumps(d))
' 2>/dev/null)" || translated="${payload}"

printf '%s' "${translated}" | exec "${CLAUDE_HOOK}"
