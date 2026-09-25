#!/usr/bin/env bash
# Sourced by scripts/test-goal-refresh-hook.sh -- not run on its own.
#
# codex#4018964359 against sustained-drives.md:32: the documented Claude
# rendering was two commands, `/goal <text>` then `/goal consent push,
# pr-open`. goal-continue.sh's Stop hook keeps the agent working once the
# first `/goal` lands, so an unattended drive has no reliable chance to submit
# the second command before the first irreversible act -- the grant arrives
# too late for the run it was meant to cover.
#
# CLAUDE_GOAL_CONSENT already closes this, and this proves it end to end
# rather than trusting the claim: it is an environment variable, read
# directly by both hooks (goal-continue.sh:262-264, and goal-refresh.sh via
# CONSENT_ENV into lib/goal-refresh-note.py:37-39) independently of anything
# written to the goal file. Set once by the launcher BEFORE the session's
# first prompt, it is already in effect for the very first Stop -- there is
# no second command to race against, because there is no second command at
# all. That is the atomic form docs/internal/agent-hooks.md and
# sustained-drives.md now document as the Claude rendering.

acw="${work}/atomic-consent"
mkdir -p "${acw}/.claude"
asid="atomicconsent-$$"
agoal_file="${acw}/.claude/active-goal.${asid}"

stop_with_env() { # session_id cwd prompt_id consent_env
	SID="$1" SID_CWD="$2" python3 -c '
import json, os, sys
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": sys.argv[1],
                  "stop_hook_active": False, "cwd": os.environ["SID_CWD"],
                  "hook_event_name": "Stop"}))
' "$3" | CLAUDE_GOAL_CONSENT="$4" bash "${CONTINUE}" 2>/dev/null
}

# ── ONE submission, launcher-side consent already in effect ────────────────
rm -f "${agoal_file}"
CLAUDE_GOAL_CONSENT='push, pr-open' \
	submit "${asid}" '/goal ship the atomic-consent drive' "${acw}" >/dev/null
if [[ -f "${agoal_file}" ]] && rg -q 'ship the atomic-consent drive' "${agoal_file}"; then
	ok "ATOMIC: the single /goal submission records the goal body"
else
	no "ATOMIC: the single /goal submission records the goal body"
fi
if rg -qi '^consent:' "${agoal_file}" 2>/dev/null; then
	no "ATOMIC: no CONSENT line is needed in the file -- the grant is env-only"
else
	ok "ATOMIC: no CONSENT line is needed in the file -- the grant is env-only"
fi

astop="$(stop_with_env "${asid}" "${acw}" e2e-atomic 'push, pr-open')"
if printf '%s' "${astop}" | rg -q '"decision"[[:space:]]*:[[:space:]]*"block"'; then
	ok "ATOMIC: the goal is still open, so the Stop hook still asks for progress"
else
	no "ATOMIC: the goal is still open, so the Stop hook still asks for progress"
fi
if printf '%s' "${astop}" | rg -q 'OWNER CONSENT ALREADY GRANTED for: push, pr-open'; then
	ok "ATOMIC: the Stop hook honours the launcher-side grant on the FIRST stop"
else
	no "ATOMIC: the Stop hook honours the launcher-side grant on the FIRST stop (got: ${astop})"
fi
if printf '%s' "${astop}" | rg -qF 'you need consent for an irreversible act (push, merge, deploy, delete, data mutation, anything outward-facing)'; then
	no "ATOMIC: the Stop hook must not ask about push -- it was already granted"
else
	ok "ATOMIC: the Stop hook does not ask about push"
fi

# ── control: mentioning consent in the goal BODY is not a grant ────────────
#
# The pitfall this whole feature guards against: CONSENT: is metadata only in
# a LEADING block. A goal whose objective text happens to discuss consent must
# not be silently read as one, in the body OR by name-dropping the word alone.
bcw="${work}/atomic-consent-body-mention"
mkdir -p "${bcw}/.claude"
bsid="atomicconsent-body-$$"
bgoal_file="${bcw}/.claude/active-goal.${bsid}"
rm -f "${bgoal_file}"
submit "${bsid}" '/goal ship it. Note: the owner already gave consent for push in chat.' "${bcw}" >/dev/null
bstop="$(stop_with_env "${bsid}" "${bcw}" e2e-body-mention '')"
# The Stop hook no longer offers a consent stop reason at all, so the grant
# banner below is the only signal a mention was misread as a grant. This case
# pins that the goal itself is still open.
if printf '%s' "${bstop}" | rg -q '"decision": *"block"'; then
	ok "CONTROL: a body line merely mentioning consent leaves the goal open"
else
	no "CONTROL: a body line merely mentioning consent leaves the goal open (got: ${bstop})"
fi
if printf '%s' "${bstop}" | rg -q 'OWNER CONSENT ALREADY GRANTED'; then
	no "CONTROL: mentioning consent in prose must not grant anything"
else
	ok "CONTROL: mentioning consent in prose grants nothing"
fi

# LAST line on purpose -- see the sibling companions.
goal_refresh_atomic_consent_cases_loaded=1
