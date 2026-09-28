#!/usr/bin/env python3
"""Require manifest-backed Eshu roles for Codex child spawns."""

import json
import sys
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[1]
CANONICAL_TOOL = "collaborationspawn_agent"


def deny(reason: str) -> dict[str, object]:
    """Return Codex's supported PreToolUse denial envelope."""
    return {"hookSpecificOutput": {
        "hookEventName": "PreToolUse",
        "permissionDecision": "deny",
        "permissionDecisionReason": reason,
    }}


def guard(payload: Any) -> dict[str, object] | None:
    """Check a spawn payload, leaving unrelated calls and workspaces alone."""
    if not isinstance(payload, dict):
        return deny("Eshu spawn guard received malformed hook input; retry with a named Eshu role.")
    if payload.get("tool_name") != CANONICAL_TOOL:
        return None

    cwd = payload.get("cwd")
    if not isinstance(cwd, str) or not cwd:
        return deny("Eshu spawn guard needs the session cwd to validate this spawn.")
    if not Path(cwd).resolve().is_relative_to(ROOT):
        return None

    try:
        manifest = json.loads((ROOT / ".agents/roles.json").read_text())
        roles = manifest["roles"]
        if not isinstance(roles, dict) or not roles:
            raise ValueError("empty role catalog")
    except (OSError, ValueError, TypeError, KeyError):
        return deny("Eshu role manifest is unavailable or invalid; restore .agents/roles.json before spawning.")

    tool_input = payload.get("tool_input")
    if not isinstance(tool_input, dict):
        return deny("Eshu spawn input is malformed; specify agent_type with a named Eshu role.")
    role = tool_input.get("agent_type")
    if not isinstance(role, str) or role not in roles:
        names = ", ".join(sorted(roles))
        return deny(f"Select a named Eshu agent_type for this spawn: {names}. Generic or unknown roles are not allowed.")
    return None


def main() -> None:
    """Read one hook event and emit a denial only when needed."""
    try:
        payload = json.load(sys.stdin)
    except (ValueError, TypeError):
        payload = None
    decision = guard(payload)
    if decision is not None:
        print(json.dumps(decision))


if __name__ == "__main__":
    main()
