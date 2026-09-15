#!/usr/bin/env bash
# Sourced by scripts/test-goal-refresh-hook.sh -- not run on its own.
#
# The one-line form: `/goal consent <acts> -- <goal text>` sets a goal and its
# consent in a SINGLE chat turn. Before this, the owner could only grant
# consent by editing the CONSENT line of a goal that already existed
# (`/goal consent <acts>`, tested in the sibling cases file) -- there was no
# way to hand over both the objective and the grant together, so the first
# `/goal <text>` always started work with no consent recorded until a second
# command landed. sustained-drives.md's own atomic-consent section already
# covers the launcher-side answer to that gap, `CLAUDE_GOAL_CONSENT`; this is
# the answer for a plain chat turn with no launcher in the loop.
#
# Split on the FIRST ` -- ` only, so a goal that itself discusses consent, or
# contains a later ` -- `, stays objective text rather than being mistaken for
# a second delimiter.

# ── the one-line form itself ────────────────────────────────────────────────

olw="${work}/oneline-consent"
mkdir -p "${olw}/.claude"
olsid="onelineconsent-$$"
olgoal="${olw}/.claude/active-goal.${olsid}"

rm -f "${olgoal}"
submit "${olsid}" \
	'/goal consent push, pr-open -- Drive #6642 to closed per eshu-issue-driver Completion Evidence.' \
	"${olw}" >/dev/null

if [[ "$(head -1 "${olgoal}" 2>/dev/null)" == "SESSION: ${olsid}" ]]; then
	ok "ONELINE: the single submission writes a SESSION header"
else
	no "ONELINE: the single submission writes a SESSION header (got: $(head -1 "${olgoal}" 2>/dev/null))"
fi
if rg -q '^CONSENT: push, pr-open$' "${olgoal}" 2>/dev/null; then
	ok "ONELINE: the single submission writes the CONSENT line"
else
	no "ONELINE: the single submission writes the CONSENT line"
fi
if rg -q 'Drive #6642 to closed per eshu-issue-driver Completion Evidence\.' "${olgoal}" 2>/dev/null; then
	ok "ONELINE: the single submission writes the goal text"
else
	no "ONELINE: the single submission writes the goal text"
fi

olinj="$(injected "$(submit "${olsid}" 'carry on' "${olw}")")"
if printf '%s' "${olinj}" | rg -q 'OWNER CONSENT ALREADY GRANTED for: push, pr-open'; then
	ok "ONELINE: the refresher restates the grant on the very next turn"
else
	no "ONELINE: the refresher restates the grant on the very next turn (got: ${olinj})"
fi

olstop="$(SID="${olsid}" CWD="${olw}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "oneline-e2e",
                  "stop_hook_active": False, "cwd": os.environ["CWD"],
                  "hook_event_name": "Stop"}))
' | bash "${CONTINUE}" 2>/dev/null)"
if printf '%s' "${olstop}" | rg -q 'OWNER CONSENT ALREADY GRANTED for: push, pr-open'; then
	ok "ONELINE: goal-continue.sh honours the grant on the FIRST stop, no second command needed"
else
	no "ONELINE: goal-continue.sh honours the grant on the FIRST stop (got: ${olstop})"
fi
if printf '%s' "${olstop}" | rg -qF 'you need consent for an irreversible act (push, merge, deploy, delete, data mutation, anything outward-facing)'; then
	no "ONELINE: goal-continue.sh must not ask about push -- it was already granted"
else
	ok "ONELINE: goal-continue.sh does not ask about push"
fi

# submit() itself redirects the hook's stderr to /dev/null, so the rejection
# cases below build the payload directly instead of going through it.
submit_capture_stderr() { # session_id prompt cwd
	SID="$1" PROMPT="$2" CWD="$3" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "p1",
                  "cwd": os.environ["CWD"], "prompt": os.environ["PROMPT"],
                  "hook_event_name": "UserPromptSubmit"}))
' | bash "${REFRESH}" 2>&1 1>/dev/null
}

# ── empty acts: `/goal consent -- text` ─────────────────────────────────────

eaw="${work}/oneline-empty-acts"
mkdir -p "${eaw}/.claude"
easid="onelineemptyacts-$$"
eagoal="${eaw}/.claude/active-goal.${easid}"
rm -f "${eagoal}"
eaerr="$(submit_capture_stderr "${easid}" '/goal consent -- text' "${eaw}")"
if [[ -f "${eagoal}" ]]; then
	no "EMPTY ACTS: /goal consent -- text must write nothing"
else
	ok "EMPTY ACTS: /goal consent -- text writes nothing"
fi
if printf '%s' "${eaerr}" | rg -qi 'non-empty'; then
	ok "EMPTY ACTS: the rejection is reported on stderr"
else
	no "EMPTY ACTS: the rejection is reported on stderr (got: ${eaerr})"
fi

# ── empty text: `/goal consent push --` ─────────────────────────────────────

etw="${work}/oneline-empty-text"
mkdir -p "${etw}/.claude"
etsid="onelineemptytext-$$"
etgoal="${etw}/.claude/active-goal.${etsid}"
rm -f "${etgoal}"
eterr="$(submit_capture_stderr "${etsid}" '/goal consent push --' "${etw}")"
if [[ -f "${etgoal}" ]]; then
	no "EMPTY TEXT: /goal consent push -- must write nothing"
else
	ok "EMPTY TEXT: /goal consent push -- writes nothing"
fi
if printf '%s' "${eterr}" | rg -qi 'non-empty'; then
	ok "EMPTY TEXT: the rejection is reported on stderr"
else
	no "EMPTY TEXT: the rejection is reported on stderr (got: ${eterr})"
fi

# ── a later ` -- ` stays in the objective ───────────────────────────────────

ldw="${work}/oneline-later-dash"
mkdir -p "${ldw}/.claude"
ldsid="onelinelaterdash-$$"
ldgoal="${ldw}/.claude/active-goal.${ldsid}"
rm -f "${ldgoal}"
submit "${ldsid}" '/goal consent push -- Drive it -- carefully, per the plan' "${ldw}" >/dev/null
if rg -q '^CONSENT: push$' "${ldgoal}" 2>/dev/null; then
	ok "LATER DASH: only the text before the FIRST -- becomes the grant"
else
	no "LATER DASH: only the text before the FIRST -- becomes the grant (file: $(cat "${ldgoal}" 2>/dev/null))"
fi
if rg -qF -- 'Drive it -- carefully, per the plan' "${ldgoal}" 2>/dev/null; then
	ok "LATER DASH: a second -- stays inside the objective text"
else
	no "LATER DASH: a second -- stays inside the objective text"
fi

# ── word boundary: "consented" is not "consent " ────────────────────────────
#
# Same guard the plain arm already relies on (agent-hooks.md's "consented
# users" incident): a goal that merely STARTS WITH the letters of "consent"
# must not be read as the consent arm at all, one-line form included.

cuw="${work}/oneline-consented-users"
mkdir -p "${cuw}/.claude"
cusid="onelineconsentedusers-$$"
cugoal="${cuw}/.claude/active-goal.${cusid}"
rm -f "${cugoal}"
submit "${cusid}" '/goal consented users -- need a path' "${cuw}" >/dev/null
if rg -q '^CONSENT:' "${cugoal}" 2>/dev/null; then
	no "CONSENTED USERS: 'consented users -- ...' must not grant anything"
else
	ok "CONSENTED USERS: 'consented users -- ...' grants nothing"
fi
if rg -qF -- 'consented users -- need a path' "${cugoal}" 2>/dev/null; then
	ok "CONSENTED USERS: the whole line is kept as a plain objective"
else
	no "CONSENTED USERS: the whole line is kept as a plain objective (file: $(cat "${cugoal}" 2>/dev/null))"
fi


# ── revoke matched by command prefix only (#6714 review) ─────────────────────
# A one-line goal whose OBJECTIVE mentions revoke-consent must start the new
# goal with its grant, not strip the existing goal's consent.
rvw="${work}/oneline-revoke-word"
mkdir -p "${rvw}/.claude"
rvsid="onelinerevoke-$$"
rvgoal="${rvw}/.claude/active-goal.${rvsid}"
rm -f "${rvgoal}"
submit "${rvsid}" '/goal consent merge -- Old objective.' "${rvw}" >/dev/null
submit "${rvsid}" '/goal consent push -- Repair the revoke-consent command.' "${rvw}" >/dev/null
if rg -q 'Repair the revoke-consent command\.' "${rvgoal}" 2>/dev/null \
	&& rg -q '^CONSENT: push$' "${rvgoal}" 2>/dev/null \
	&& ! rg -q 'Old objective' "${rvgoal}" 2>/dev/null; then
	ok "REVOKE WORD: a goal mentioning revoke-consent starts the new goal with its grant"
else
	no "REVOKE WORD: a goal mentioning revoke-consent starts the new goal with its grant (got: $(tr '\n' '|' < "${rvgoal}" 2>/dev/null))"
fi
submit "${rvsid}" '/goal revoke-consent' "${rvw}" >/dev/null
if ! rg -q '^CONSENT:' "${rvgoal}" 2>/dev/null && rg -q 'Repair the revoke-consent command\.' "${rvgoal}" 2>/dev/null; then
	ok "REVOKE WORD: the real /goal revoke-consent command still clears the grant"
else
	no "REVOKE WORD: the real /goal revoke-consent command still clears the grant"
fi

# LAST line on purpose -- see the sibling companions.
goal_refresh_oneline_consent_cases_loaded=1
