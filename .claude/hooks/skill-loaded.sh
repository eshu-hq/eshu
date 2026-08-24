#!/bin/bash
# skill-loaded.sh — PostToolUse hook for the Skill tool. Records which skills
# were actually loaded in this session so skill-nudge.sh can require them
# rather than merely suggest them.
#
# Never blocks. Its only job is to write a marker.
#
# Payload shape, read off a real invocation rather than assumed:
#
#   {"hook_event_name":"PostToolUse","tool_name":"Skill",
#    "session_id":"...","tool_input":{"skill":"golang-engineering"}, ...}
#
# So the name is at tool_input.skill. The `skill_name`/`name` fallbacks below
# are kept as cheap insurance against a future rename; if one of them ever
# starts matching, the shape changed and this comment is stale.
set -u

INPUT=$(cat)
command -v python3 >/dev/null 2>&1 || exit 0

SID=$(printf '%s' "$INPUT" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("session_id","na")[:12])' 2>/dev/null)
[ -n "${SID:-}" ] || exit 0

# Read the skill name from the verified field, then fall back. Normalise by
# dropping any plugin prefix (`superpowers:brainstorming` -> `brainstorming`)
# so the marker matches the bare id the nudge table uses.
NAME=$(printf '%s' "$INPUT" | python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)
except Exception:
    print("")
    raise SystemExit(0)
ti = d.get("tool_input") or {}
for key in ("skill", "skill_name", "name"):
    v = ti.get(key)
    if isinstance(v, str) and v.strip():
        print(v.strip().split(":")[-1])
        raise SystemExit(0)
print("")
' 2>/dev/null)

[ -n "${NAME:-}" ] || exit 0

SAFE=$(printf '%s' "$NAME" | tr -cd 'a-zA-Z0-9-' | cut -c1-40)
[ -n "$SAFE" ] || exit 0
touch "/tmp/claude-skill-loaded-${SID}-${SAFE}" 2>/dev/null
exit 0
