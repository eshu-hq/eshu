#!/bin/bash
# on-compact.sh — SessionStart hook (matcher: compact|resume). Fires right after
# context compaction or session resume, the exact moment long-goal sessions lose
# their rules and their loaded skills.
#
# This text names the skill rather than restating its content. The canon rule is
# that rules live once; a hook that re-lists obligations becomes a second
# rulebook that drifts from the first.
set -u
INPUT=$(cat)

# Clear this session's loaded-skill markers FIRST, before any scope check.
#
# skill-nudge.sh treats a marker as "the skill is in this context window", and
# `skill-loaded.sh` writes it keyed on the session id. A resume keeps that id
# while discarding the loaded skill content, so without this the marker
# outlives the thing it stands for: the nudge would wave through an edit whose
# governing skill is no longer loaded, which is the exact failure the block
# exists to prevent, and the hook's own message ("having loaded it earlier does
# not count") would be a lie.
#
# Scoped to this session's own markers, so a concurrent session keeps its own.
# Ahead of the Eshu scope guard on purpose: a stale marker is wrong everywhere,
# and the compaction that invalidated it does not care which repo you are in.
CLR_SID=$(printf '%s' "$INPUT" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("session_id","")[:12])' 2>/dev/null)
if [ -n "${CLR_SID:-}" ]; then
  rm -f "/tmp/claude-skill-loaded-${CLR_SID}-"* 2>/dev/null
fi

# Only speak inside an Eshu checkout. At user level this fires on every
# compaction on the machine, and telling an unrelated project to load
# eshu-session-lifecycle is both wrong and unactionable there.
CWD=$(printf '%s' "$INPUT" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("cwd",""))' 2>/dev/null)
[ -n "${CWD:-}" ] || CWD="$PWD"
d="$CWD"
while [ -n "$d" ] && [ "$d" != "/" ] && [ "$d" != "." ]; do
  [ -e "$d/.agents/skills/eshu-code-review" ] && break
  d="$(dirname "$d")"
done
[ -e "$d/.agents/skills/eshu-code-review" ] || exit 0

python3 -c '
import json
ctx = ("CONTEXT WAS COMPACTED OR RESUMED. Loaded skills are GONE from this "
 "context even if the summary says they were used: the skill LISTING survives, "
 "the skill CONTENT does not. Before the next tool call, invoke "
 "eshu-session-lifecycle via the Skill tool and follow its session-pickup "
 "playbook. It covers the three things that go wrong here: re-invoking the "
 "project skills that govern the surface you are on, confirming pwd is your "
 "worktree rather than the main checkout, and separating what the summary says "
 "was verified from what you can still cite a run for. Do not trust a "
 "summarized claim that a gate already passed. Cite it or re-run it.")
print(json.dumps({"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":ctx}}))
'
exit 0
