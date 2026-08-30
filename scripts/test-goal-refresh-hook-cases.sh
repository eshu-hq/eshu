#!/usr/bin/env bash
# Sourced by scripts/test-goal-refresh-hook.sh -- not run on its own.
#
# Same split, and the same reason, as the Stop mirror's companion: the suite
# outgrew the repo's 500-line cap. This PR split one mirror for exactly that
# reason and let the sibling cross it, which is the kind of thing no gate here
# catches -- the filelength linter is Go-only and the markdown cap is scoped to
# go/**. Sourced rather than executed so the helpers and the single tally stay
# shared.
#
# A trigger path for the agent-canon gate in specs/ci-gates.v1.yaml, so editing
# it alone still selects the gate that runs it. The sentinel is what makes the
# parent notice if it ever stops being sourced: a trigger makes a gate RUN, it
# cannot make a gate FAIL.

# ── consent ────────────────────────────────────────────────────────────────
#
# Consent typed into a chat window dies with the turn it was typed in. The
# owner said "yes, push" and the next Stop handed the agent the same "you need
# consent for an irreversible act" bullet, so it stopped again. `/goal consent`
# writes that answer into the goal file, where the refresher restates it every
# turn and the Stop hook can read it.

cw="${work}/consent"
mkdir -p "${cw}/.claude"
cgoal="${cw}/.claude/active-goal.${sid}"
submit "${sid}" '/goal open the six-PR train' "${cw}" >/dev/null
submit "${sid}" '/goal consent push, pr-open' "${cw}" >/dev/null

if rg -q '^CONSENT: push, pr-open$' "${cgoal}" 2>/dev/null; then
	ok "/goal consent writes a CONSENT line"
else
	no "/goal consent writes a CONSENT line"
fi

# The objective and the permission are separate facts. Granting one must not
# erase the other -- `/goal consent ...` matching the generic `/goal <text>`
# producer would silently replace the goal with the word "consent".
if rg -q 'open the six-PR train' "${cgoal}" 2>/dev/null; then
	ok "/goal consent leaves the objective intact"
else
	no "/goal consent leaves the objective intact"
fi

cinj="$(injected "$(submit "${sid}" 'carry on' "${cw}")")"
if printf '%s' "${cinj}" | rg -q 'push, pr-open'; then
	ok "the refresher restates the granted acts every turn"
else
	no "the refresher restates the granted acts every turn"
fi
if printf '%s' "${cinj}" | rg -qi 'already granted|already consented'; then
	ok "the refresher says the granted acts need no further asking"
else
	no "the refresher says the granted acts need no further asking"
fi

# Consent is revocable, or it is not consent.
submit "${sid}" '/goal revoke-consent' "${cw}" >/dev/null
if rg -q '^CONSENT:' "${cgoal}" 2>/dev/null; then
	no "/goal revoke-consent removes the CONSENT line"
else
	ok "/goal revoke-consent removes the CONSENT line"
fi
if rg -q 'open the six-PR train' "${cgoal}" 2>/dev/null; then
	ok "/goal revoke-consent leaves the objective intact"
else
	no "/goal revoke-consent leaves the objective intact"
fi

# An empty grant is not a grant.
submit "${sid}" '/goal consent' "${cw}" >/dev/null
if rg -q '^CONSENT:' "${cgoal}" 2>/dev/null; then
	no "/goal consent with no acts grants nothing"
else
	ok "/goal consent with no acts grants nothing"
fi

# END TO END for the consent half: what the producer writes has to be what the
# Stop hook reads. The two halves of the goal feature already shipped without
# ever meeting once; this is the same seam.
submit "${sid}" '/goal consent push' "${cw}" >/dev/null
consent_stop="$(SID="${sid}" CWD="${cw}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "consent-e2e",
                  "stop_hook_active": False, "cwd": os.environ["CWD"],
                  "hook_event_name": "Stop"}))
' | bash "${CONTINUE}" 2>/dev/null)"
if printf '%s' "${consent_stop}" | rg -qi 'already granted|already consented'; then
	ok "END TO END: /goal consent is honoured by the Stop hook"
else
	no "END TO END: /goal consent is honoured by the Stop hook"
fi

# The blanket test on THIS side was untested against the defect it mirrors.
# Reverting it to `"all" in act.lower()` -- the original substring defect, on
# the half fixed last -- left the whole suite green, because every case here
# used values that are unambiguous either way. The Stop mirror has five cases
# that catch exactly this; this is the refresher's.
sub="${work}/substring"
mkdir -p "${sub}/.claude"
submit "${sid}" '/goal ship the thing' "${sub}" >/dev/null
submit "${sid}" '/goal consent install deps' "${sub}" >/dev/null
sub_inj="$(injected "$(submit "${sid}" 'carry on' "${sub}")")"
if printf '%s' "${sub_inj}" | rg -q 'NOT on it'; then
	ok "CONSENT: install deps is NAMED here too, not blanket"
else
	no "CONSENT: install deps is NAMED here too, not blanket"
fi
if printf '%s' "${sub_inj}" | rg -qi 'blanket'; then
	no "a narrow grant containing the letters of all is not called blanket"
else
	ok "a narrow grant containing the letters of all is not called blanket"
fi

# LAST line on purpose -- see the sibling companions.
goal_refresh_cases_loaded=1
