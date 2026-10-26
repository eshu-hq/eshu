#!/usr/bin/env bash
# Adapt Codex Stop events to the shared active-goal continuation policy.
# Codex-specific failure text is passed through as bounded recovery context.
set -uo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || exit 0
core_hook="${repo_root}/.claude/hooks/goal-continue.sh"
[ -x "${core_hook}" ] || exit 0

payload="$(cat)"
[ -n "${payload}" ] || exit 0

# The current Stop event exposes the latest assistant message. Preserve only
# error-like messages and cap their size so a large answer cannot crowd out the
# goal text in the continuation prompt. The payload itself still goes to the
# shared hook unchanged; it owns session and goal-file resolution.
stop_context="$(printf '%s' "${payload}" | python3 -c '
import json, re, sys

try:
    payload = json.load(sys.stdin)
except Exception:
    raise SystemExit(0)
message = payload.get("last_assistant_message")
if not isinstance(message, str) or not re.search(
    r"capacity|security|blocked|denied|error|failed", message, re.IGNORECASE
):
    raise SystemExit(0)
message = " ".join(message.split())
print(message[:1200])
' 2>/dev/null)"

GOAL_CONTINUE_STOP_CONTEXT="${stop_context}" bash "${core_hook}" <<<"${payload}"
