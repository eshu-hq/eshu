#!/usr/bin/env python3
"""Builds the additionalContext note goal-refresh.sh injects on every prompt.

Invoked by goal-refresh.sh with the goal text (minus the SESSION header) on
stdin, CONSENT_ENV holding any launcher-side grant, and NUDGE_SKILLS holding
the space-separated ids of named-but-unloaded project skills. Split out of
goal-refresh.sh, which the repo's 500-line file cap could otherwise not fit
while still growing the goal-contract behavior.
"""
import json
import os
import sys

lines = sys.stdin.read().splitlines()

# CONSENT: <acts> is a permission the owner already gave. It is restated
# separately from the objective, because an act the owner has consented to is
# no longer something to stop and ask about -- and the ask-only-for-consent
# sentence below reads as an invitation to do exactly that.
# Leading metadata only, for the same reason the shell view in goal-refresh.sh
# stops at the first ordinary line: a body line discussing the consent format
# is objective text, not a grant, and reading it as one both truncates the
# goal and tells the agent the owner permitted something they never mentioned.
meta = []
seen_header = False
for line in lines:
    stripped_line = line.lstrip()
    low = stripped_line.lower()
    if low.startswith("consent:"):
        meta.append(stripped_line)
        continue
    if stripped_line.startswith("SESSION:") and not seen_header:
        seen_header = True
        continue
    break
acts = [m.split(":", 1)[1].strip() for m in meta if m.split(":", 1)[1].strip()]
env_acts = os.environ.get("CONSENT_ENV", "").strip()
if env_acts:
    acts.append(env_acts)

kept_lines, in_meta, saw_header = [], True, False
for line in lines:
    if in_meta:
        bare = line.lstrip()
        if bare.lower().startswith("consent:"):
            continue
        if bare.startswith("SESSION:") and not saw_header:
            saw_header = True
        else:
            in_meta = False
    kept_lines.append(line)
goal = "\n".join(kept_lines).strip()
if not goal:
    raise SystemExit(0)

granted = ", ".join(acts)
# Blanket is a whole token, the same test the Stop hook applies -- a substring
# match read "install deps" as blanket consent there, and the two halves have
# to agree about what a grant means or the same file says two things.
blanket = any(
    tok.strip().lower() in ("all", "*") for act in acts for tok in act.split(",")
)
if granted and blanket:
    consent_note = (
        "\n\nOWNER CONSENT ALREADY GRANTED, blanket, for: " + granted
        + ". Carry out the irreversible acts this goal needs; do not stop to "
          "ask for them. Ask only when complete evidence would still leave a "
          "product-taste call."
    )
elif granted:
    consent_note = (
        "\n\nOWNER CONSENT ALREADY GRANTED for: " + granted
        + ". Carry those out yourself when the work reaches them; do not stop "
          "to ask again for anything on that list. Ask only for an "
          "irreversible act NOT on it, or when complete evidence would still "
          "leave a product-taste call."
    )
else:
    consent_note = (
        " Ask only for consent on an irreversible act (push, merge, deploy, "
        "delete, data mutation), or when complete evidence would still leave a "
        "product-taste call."
    )

# A goal that names a project skill this session has not loaded gets a nudge,
# computed by lib/skill-nudge-lib.sh before this script is invoked.
nudge_ids = os.environ.get("NUDGE_SKILLS", "").split()
nudge_note = ""
if nudge_ids:
    nudge_note = "\n\n" + " ".join("load `%s` now" % i for i in nudge_ids)

note = (
    "ACTIVE GOAL (restated each turn so it cannot go stale):\n"
    + goal
    + "\n\nKeep working it. Take the next concrete action yourself. Do not ask "
      "the owner something the code, a local doc, a loaded skill, or a "
      "Deep-tier model dispatch can settle -- their rules already cover it."
    + consent_note
    + nudge_note
)
print(json.dumps({"hookSpecificOutput": {
    "hookEventName": "UserPromptSubmit",
    "additionalContext": note,
}}))
