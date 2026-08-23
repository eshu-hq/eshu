#!/bin/bash
# skill-loaded.sh — PostToolUse hook for the Skill tool. Records which skills
# were actually loaded in this session so skill-nudge.sh can require them
# rather than merely suggest them.
#
# Never blocks. Its only job is to write a marker.
#
# Payload shape: the Skill tool takes {skill, args}, so the name is expected at
# tool_input.skill. Several fallbacks are read anyway, and the first payload of
# each session is captured to /tmp/claude-skill-payload-<sid>.json so the shape
# can be confirmed against a real invocation instead of assumed. Delete that
# capture line once the shape is pinned.
set -u

INPUT=$(cat)
command -v python3 >/dev/null 2>&1 || exit 0

SID=$(printf '%s' "$INPUT" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("session_id","na")[:12])' 2>/dev/null)
[ -n "${SID:-}" ] || exit 0

# One capture per session, for shape verification.
CAPTURE="/tmp/claude-skill-payload-${SID}.json"
[ -f "$CAPTURE" ] || printf '%s' "$INPUT" >"$CAPTURE" 2>/dev/null

# Read the skill name from the most likely field, then fall back. Normalise by
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
