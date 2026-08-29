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

goal_file="${work}/.claude/active-goal"

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
if rg -q 'survive a spacey path' "${spacey}/.claude/active-goal" 2>/dev/null; then
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

printf '\ngoal-refresh hook mirror: %s passed, %s failed\n' "${passed}" "${failed}"
[[ "${failed}" -eq 0 ]]
