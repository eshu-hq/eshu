#!/usr/bin/env bash
# Behavioural mirror for .claude/hooks/goal-refresh.sh (UserPromptSubmit).
#
# This hook does two jobs, and the second one is why it exists:
#   PRODUCER   -- `/goal <text>` writes the goal file. Nothing else did, which
#                 is why the Stop hook shipped inert to every machine.
#   REFRESHER  -- every prompt re-injects the goal as additionalContext, so a
#                 long session cannot drift off it and compaction cannot
#                 summarize it away.
#
# The end-to-end case at the bottom is the one that matters: producer output
# must actually satisfy the consumer. Testing the two halves separately is what
# let a consumer-with-no-producer pass review in the first place.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REFRESH="${repo_root}/.claude/hooks/goal-refresh.sh"
CONTINUE="${repo_root}/.claude/hooks/goal-continue.sh"

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
mkdir -p "${work}/.claude"
passed=0
failed=0
sid="refresh-$$"

ok() { printf 'ok - %s\n' "$1"; passed=$((passed + 1)); }
no() { printf 'not ok - %s\n' "$1"; failed=$((failed + 1)); }

# Payload built by python so a cwd containing a space is JSON-escaped properly.
submit() { # session_id prompt cwd
	SID="$1" PROMPT="$2" CWD="$3" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "p1",
                  "cwd": os.environ["CWD"], "prompt": os.environ["PROMPT"],
                  "hook_event_name": "UserPromptSubmit"}))
' | bash "${REFRESH}" 2>/dev/null
}

injected() { # output -> prints the additionalContext, or nothing
	printf '%s' "$1" | python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)
except Exception:
    raise SystemExit(0)
print(d.get("hookSpecificOutput", {}).get("additionalContext", ""))
' 2>/dev/null
}

[[ -f "${REFRESH}" ]] || { printf 'not ok - hook missing at %s\n' "${REFRESH}"; exit 1; }
# if/else rather than `|| no`, so a PASS increments the tally too -- otherwise
# the printed count silently under-reports what was actually checked.
if [[ -x "${REFRESH}" ]]; then ok "hook is executable"; else no "hook is executable"; fi
if bash -n "${REFRESH}" 2>/dev/null; then ok "hook parses"; else no "hook parses"; fi

# The producer writes THIS session's goal file, not the checkout-shared one:
# concurrent sessions in one clone must not overwrite each other.
goal_file="${work}/.claude/active-goal.${sid}"

# ── producer ───────────────────────────────────────────────────────────────

rm -f "${goal_file}"
out="$(submit "${sid}" '/goal ship the refresh hook' "${work}")"
if [[ -f "${goal_file}" ]]; then
	ok "/goal writes the goal file"
else
	no "/goal writes the goal file"
fi

if [[ "$(head -1 "${goal_file}" 2>/dev/null)" == "SESSION: ${sid}" ]]; then
	ok "the goal file carries a SESSION header"
else
	no "the goal file carries a SESSION header (got: $(head -1 "${goal_file}" 2>/dev/null))"
fi

if rg -q 'ship the refresh hook' "${goal_file}" 2>/dev/null; then
	ok "the goal text is stored"
else
	no "the goal text is stored"
fi

rm -f "${goal_file}"
submit "${sid}" 'just a normal question' "${work}" >/dev/null
if [[ -f "${goal_file}" ]]; then
	no "a non-goal prompt must not create a goal file"
else
	ok "a non-goal prompt creates no goal file"
fi

rm -f "${goal_file}"
submit "${sid}" 'GOAL: use the alternate prefix' "${work}" >/dev/null
if rg -q 'use the alternate prefix' "${goal_file}" 2>/dev/null; then
	ok "the GOAL: prefix also sets a goal"
else
	no "the GOAL: prefix also sets a goal"
fi

# ── refresher: the anti-staleness core ─────────────────────────────────────

rm -f "${goal_file}"
submit "${sid}" '/goal keep the objective alive' "${work}" >/dev/null
ctx="$(injected "$(submit "${sid}" 'a later, unrelated prompt' "${work}")")"
if printf '%s' "${ctx}" | rg -q 'keep the objective alive'; then
	ok "a later prompt re-injects the goal (this is the staleness fix)"
else
	no "a later prompt re-injects the goal"
fi

if printf '%s' "${ctx}" | rg -q 'Deep-tier|do not ask the owner|Do not ask'; then
	ok "the injected context carries the do-not-ask-the-owner rule"
else
	no "the injected context carries the do-not-ask-the-owner rule"
fi

# Ten turns later it must still be there — staleness is a function of turns.
for i in 1 2 3 4 5 6 7 8 9 10; do
	late="$(injected "$(submit "${sid}" "turn ${i}" "${work}")")"
done
if printf '%s' "${late}" | rg -q 'keep the objective alive'; then
	ok "still injected after ten further turns"
else
	no "still injected after ten further turns"
fi

# ── scope and retirement ───────────────────────────────────────────────────

other="$(submit "someone-else" 'what next?' "${work}")"
if [[ -z "${other}" ]]; then
	ok "a different session gets no injection (no cross-session leak)"
else
	no "a different session gets no injection (no cross-session leak)"
fi

submit "${sid}" '/goal done' "${work}" >/dev/null
if [[ "$(head -1 "${goal_file}" 2>/dev/null)" == DONE* ]]; then
	ok "/goal done retires the goal"
else
	no "/goal done retires the goal"
fi
after="$(submit "${sid}" 'anything' "${work}")"
if [[ -z "${after}" ]]; then
	ok "a retired goal stops being injected"
else
	no "a retired goal stops being injected"
fi

# ── a cwd containing a space ───────────────────────────────────────────────

spacey="${work}/My Projects/eshu"
mkdir -p "${spacey}/.claude"
submit "${sid}" '/goal survive a spacey path' "${spacey}" >/dev/null
if rg -q 'survive a spacey path' "${spacey}/.claude/active-goal.${sid}" 2>/dev/null; then
	ok "a cwd containing a space still gets its own goal file"
else
	no "a cwd containing a space still gets its own goal file"
fi

# ── fail-open ──────────────────────────────────────────────────────────────

printf 'not json' | bash "${REFRESH}" >/dev/null 2>&1
[[ $? -eq 0 ]] && ok "malformed JSON exits 0" || no "malformed JSON exits 0"
printf '' | bash "${REFRESH}" >/dev/null 2>&1
[[ $? -eq 0 ]] && ok "empty stdin exits 0" || no "empty stdin exits 0"
out_off="$(SID="${sid}" PROMPT='anything' CWD="${work}" CLAUDE_GOAL_OFF=1 python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "p1",
                  "cwd": os.environ["CWD"], "prompt": os.environ["PROMPT"],
                  "hook_event_name": "UserPromptSubmit"}))
' | CLAUDE_GOAL_OFF=1 bash "${REFRESH}" 2>/dev/null)"
[[ -z "${out_off}" ]] && ok "CLAUDE_GOAL_OFF=1 silences it" || no "CLAUDE_GOAL_OFF=1 silences it"

# ── END TO END: producer output must satisfy the consumer ──────────────────
#
# The bug this whole change exists to fix was that these two halves never met.
# Testing them separately is exactly what let that ship.

e2e="${work}/e2e"
mkdir -p "${e2e}/.claude"
submit "${sid}" '/goal finish the end to end wiring' "${e2e}" >/dev/null
stop_out="$(SID="${sid}" CWD="${e2e}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "e2e",
                  "stop_hook_active": False, "cwd": os.environ["CWD"],
                  "hook_event_name": "Stop"}))
' | bash "${CONTINUE}" 2>/dev/null)"
if printf '%s' "${stop_out}" | rg -q '"decision"[[:space:]]*:[[:space:]]*"block"'; then
	ok "END TO END: a goal set by /goal makes the Stop hook block"
else
	no "END TO END: a goal set by /goal makes the Stop hook block"
fi
if printf '%s' "${stop_out}" | rg -q 'finish the end to end wiring'; then
	ok "END TO END: the Stop hook hands back the goal the producer wrote"
else
	no "END TO END: the Stop hook hands back the goal the producer wrote"
fi

# The Stop hook falls back to ${HOME}/.claude/active-goal. If this hook does not
# look there too, such a goal is ENFORCED but never REFRESHED -- blocking turns
# while going stale, the worst of both halves. Uses a fake HOME so the real one
# is never touched.
fake_home="${work}/fakehome"
mkdir -p "${fake_home}/.claude"
printf 'SESSION: %s\nHome-scoped objective.\n' "${sid}" >"${fake_home}/.claude/active-goal"
empty_cwd="${work}/no-goal-here"
mkdir -p "${empty_cwd}/.claude"
home_out="$(SID="${sid}" PROMPT='a plain prompt' CWD="${empty_cwd}" HOME="${fake_home}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "homeonly",
                  "cwd": os.environ["CWD"], "prompt": os.environ["PROMPT"],
                  "hook_event_name": "UserPromptSubmit"}))
' | HOME="${fake_home}" bash "${REFRESH}" 2>/dev/null)"
if printf '%s' "$(injected "${home_out}")" | rg -q 'Home-scoped objective'; then
	ok "a \$HOME-only goal is still refreshed (lookup matches the Stop hook)"
else
	no "a \$HOME-only goal is still refreshed (lookup matches the Stop hook)"
fi

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

# ── concurrent sessions in one checkout ────────────────────────────────────
#
# Sessions are not per-checkout. Three agents in one clone shared a single
# `.claude/active-goal`: the last `/goal` to run overwrote the others, and the
# SESSION header then denied the losers any enforcement at all -- silently.
# The producer writes `.claude/active-goal.<session_id>`, so parallel sessions
# hold parallel goals.

pw="${work}/parallel"
mkdir -p "${pw}/.claude"
sid_a="${sid}-a"
sid_b="${sid}-b"

submit "${sid_a}" '/goal lane A: land the parser train' "${pw}" >/dev/null
submit "${sid_b}" '/goal lane B: chase the flaky gate' "${pw}" >/dev/null

if [[ -f "${pw}/.claude/active-goal.${sid_a}" && -f "${pw}/.claude/active-goal.${sid_b}" ]]; then
	ok "two sessions in one checkout get two goal files"
else
	no "two sessions in one checkout get two goal files"
fi

# The shared file is the owner's hand-written form. The producer must not
# create or touch it, or the next hand-written goal is somebody's leftovers.
if [[ -f "${pw}/.claude/active-goal" ]]; then
	no "the producer leaves the checkout-shared goal file alone"
else
	ok "the producer leaves the checkout-shared goal file alone"
fi

inj_a="$(injected "$(submit "${sid_a}" 'carry on' "${pw}")")"
inj_b="$(injected "$(submit "${sid_b}" 'carry on' "${pw}")")"
if printf '%s' "${inj_a}" | rg -q 'lane A' && ! printf '%s' "${inj_a}" | rg -q 'lane B'; then
	ok "session A is refreshed with only its own goal"
else
	no "session A is refreshed with only its own goal"
fi
if printf '%s' "${inj_b}" | rg -q 'lane B' && ! printf '%s' "${inj_b}" | rg -q 'lane A'; then
	ok "session B is refreshed with only its own goal"
else
	no "session B is refreshed with only its own goal"
fi

# Retirement is per session too. One agent finishing must not release the other.
submit "${sid_a}" '/goal done' "${pw}" >/dev/null
if [[ -z "$(submit "${sid_a}" 'anything' "${pw}")" ]]; then
	ok "/goal done retires only the caller's goal"
else
	no "/goal done retires only the caller's goal"
fi
if printf '%s' "$(injected "$(submit "${sid_b}" 'anything' "${pw}")")" | rg -q 'lane B'; then
	ok "the other session's goal survives that retirement"
else
	no "the other session's goal survives that retirement"
fi

# END TO END: the file the producer wrote for THIS session is the file the Stop
# hook enforces for it -- while a second session's goal sits beside it.
par_stop="$(SID="${sid_b}" CWD="${pw}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "par-e2e",
                  "stop_hook_active": False, "cwd": os.environ["CWD"],
                  "hook_event_name": "Stop"}))
' | bash "${CONTINUE}" 2>/dev/null)"
if printf '%s' "${par_stop}" | rg -q 'lane B' && ! printf '%s' "${par_stop}" | rg -q 'lane A'; then
	ok "END TO END: the Stop hook enforces each session's own goal"
else
	no "END TO END: the Stop hook enforces each session's own goal"
fi

printf '\ngoal-refresh hook mirror: %s passed, %s failed\n' "${passed}" "${failed}"
[[ "${failed}" -eq 0 ]]
