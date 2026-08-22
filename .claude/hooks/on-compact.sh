#!/bin/bash
# on-compact.sh — SessionStart hook (matcher: compact|resume). Fires right after
# context compaction or session resume, the exact moment long-goal sessions lose
# their rules and their loaded skills.
#
# This text names the skill rather than restating its content. The canon rule is
# that rules live once; a hook that re-lists obligations becomes a second
# rulebook that drifts from the first.
set -u
cat >/dev/null
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
