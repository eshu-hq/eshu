#!/usr/bin/env bash
# Sourced by scripts/test-goal-continue-hook.sh -- not run on its own.
#
# The mirror outgrew one file at the repo's 500-line cap, so the later suites
# live here: pre-granted consent, concurrent sessions in one checkout, and the
# progress-aware continuation budget. Sourced rather than executed so the
# helpers (run, check, ok/no) and the single pass/fail tally stay shared; a
# second process would report a second tally, and a suite whose count is
# printed twice is a suite nobody reads.
#
# This file is a trigger path for the agent-canon gate, so editing it alone
# still selects the gate that runs it.
#
# The sentinel below is what makes the parent notice if this file stops being
# sourced. Deleting the source line used to leave the suite reporting "26
# passed, 0 failed" and exiting 0, with 47 assertions silently gone -- a
# trigger path makes the gate RUN, it cannot make the gate FAIL.

# ── 3. pre-granted consent ─────────────────────────────────────────────────
#
# The hook told every stopping agent that "you need consent for an irreversible
# act" is a legitimate reason to end the turn. That is right by default and
# wrong once the owner has already said yes: the agent finished six lanes,
# stopped to ask for permission to push, was handed the same list back, and
# stopped again -- so the owner typed consent by hand into a session that had
# already been told to continue. `CONSENT: <acts>` in the goal file is that
# permission written down where the hook can read it.

printf 'CONSENT: push, pr-open\nOpen the six-PR train.\n' >"${goal}"
cout="$(run "$(payload k1)")"
check "a goal with CONSENT still blocks (the work is not done)" block "${cout}"

if printf '%s' "${cout}" | rg 'push, pr-open' >/dev/null; then
	ok "the refusal names the acts the owner already consented to"
else
	no "the refusal names the acts the owner already consented to"
fi

# The whole point: the consent bullet must stop reading as an open invitation
# to stop for the acts that were already granted.
if printf '%s' "${cout}" | rg -i 'already granted|already consented' >/dev/null; then
	ok "the refusal says the granted acts are not a reason to stop"
else
	no "the refusal says the granted acts are not a reason to stop"
fi

if printf '%s' "${cout}" | rg 'CONSENT:' >/dev/null; then
	no "the CONSENT line is stripped from the quoted goal text"
else
	ok "the CONSENT line is stripped from the quoted goal text"
fi

# Blanket consent retires the bullet entirely rather than narrowing it.
printf 'CONSENT: all\nShip it.\n' >"${goal}"
allout="$(run "$(payload k2)")"
check "CONSENT: all still blocks on unfinished work" block "${allout}"
if printf '%s' "${allout}" | rg -i 'you need consent for an irreversible act' >/dev/null; then
	no "CONSENT: all removes the generic consent stop reason"
else
	ok "CONSENT: all removes the generic consent stop reason"
fi

# Control: with no consent declared the bullet must survive untouched. Without
# this, a bug that always drops the bullet would look like a pass above.
printf 'Plain goal, nobody consented to anything.\n' >"${goal}"
plain="$(run "$(payload k3)")"
if printf '%s' "${plain}" | rg -i 'you need consent for an irreversible act' >/dev/null; then
	ok "with no CONSENT the consent stop reason is still offered"
else
	no "with no CONSENT the consent stop reason is still offered"
fi

# An empty claim grants nothing. The agent writes this file, so `CONSENT:` with
# no acts after it must fail closed exactly like `BLOCKED:` with no reason.
printf 'CONSENT:\nDo the thing.\n' >"${goal}"
empty="$(run "$(payload k4)")"
if printf '%s' "${empty}" | rg -i 'you need consent for an irreversible act' >/dev/null; then
	ok "an empty CONSENT: grants nothing"
else
	no "an empty CONSENT: grants nothing"
fi

# The env form is for a launcher that already knows what the run is allowed to
# do, without writing it into a file another session shares.
printf 'Env-granted goal.\n' >"${goal}"
envout="$(printf '%s' "$(payload k5)" | CLAUDE_GOAL_CONSENT='push' \
	CLAUDE_GOAL_FILE="${goal}" bash "${HOOK}" 2>/dev/null)"
if printf '%s' "${envout}" | rg -i 'already granted|already consented' >/dev/null; then
	ok "CLAUDE_GOAL_CONSENT grants without a goal-file line"
else
	no "CLAUDE_GOAL_CONSENT grants without a goal-file line"
fi

# CONSENT sits above the objective, so it must not shadow the two escapes that
# are read off the first line.
printf 'CONSENT: push\nDONE\nFinished.\n' >"${goal}"
check "DONE beneath a CONSENT line allows the stop" allow "$(run "$(payload k6)")"

printf 'CONSENT: push\nBLOCKED: waiting on a human reviewer\nMore work.\n' >"${goal}"
check "BLOCKED beneath a CONSENT line allows the stop" allow "$(run "$(payload k7)")"

# ── 4. concurrent sessions in one checkout ─────────────────────────────────
#
# The goal file was per-CHECKOUT, and sessions are not. Three agents running in
# the same clone shared one `.claude/active-goal`, and the SESSION header --
# which exists so a goal cannot outlive the session that set it -- meant the
# two sessions that did not own the file got a silent exit 0: no enforcement,
# and no signal that there was none. Measured on this machine: three live
# sessions in one checkout, one goal, one of them enforced.
#
# `.claude/active-goal.<session_id>` is looked up FIRST, so concurrent sessions
# hold separate goals with no contention. The shared file stays as the
# hand-written form and is still header-checked.

par="${work}/parallel"
mkdir -p "${par}/.claude"
sid_a="${sid}-a"
sid_b="${sid}-b"

stop_in() { # session_id prompt_id cwd
	SID="$1" PID="$2" CWD="$3" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": os.environ["PID"],
                  "stop_hook_active": False, "cwd": os.environ["CWD"],
                  "hook_event_name": "Stop"}))
' | bash "${HOOK}" 2>/dev/null
}

printf 'SESSION: %s\nSession B owns the shared file.\n' "${sid_b}" >"${par}/.claude/active-goal"
printf 'SESSION: %s\nSession A has its own goal.\n' "${sid_a}" >"${par}/.claude/active-goal.${sid_a}"

a_out="$(stop_in "${sid_a}" par1 "${par}")"
check "a session with its own goal file blocks" block "${a_out}"
if printf '%s' "${a_out}" | rg -q 'Session A has its own goal'; then
	ok "the per-session goal file is preferred over the shared one"
else
	no "the per-session goal file is preferred over the shared one"
fi
if printf '%s' "${a_out}" | rg -q 'Session B owns the shared file'; then
	no "another session's goal is never handed to this one"
else
	ok "another session's goal is never handed to this one"
fi

b_out="$(stop_in "${sid_b}" par2 "${par}")"
check "the session that owns the shared file still blocks on it" block "${b_out}"
if printf '%s' "${b_out}" | rg -q 'Session B owns the shared file'; then
	ok "the shared file still works for the session that owns it"
else
	no "the shared file still works for the session that owns it"
fi

# A third session in the same checkout, holding no goal of its own, must still
# be allowed to stop rather than inheriting either of theirs.
check "a third session in the same checkout may stop" allow \
	"$(stop_in "${sid}-c" par3 "${par}")"

# An explicit override still outranks the per-session file, or the escape hatch
# stops being an escape hatch.
printf 'Explicit override goal.\n' >"${goal}"
ovr="$(printf '%s' "$(SID="${sid_a}" CWD="${par}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "par4",
                  "stop_hook_active": False, "cwd": os.environ["CWD"],
                  "hook_event_name": "Stop"}))
')" | CLAUDE_GOAL_FILE="${goal}" bash "${HOOK}" 2>/dev/null)"
if printf '%s' "${ovr}" | rg -q 'Explicit override goal'; then
	ok "CLAUDE_GOAL_FILE still outranks the per-session file"
else
	no "CLAUDE_GOAL_FILE still outranks the per-session file"
fi

# DONE in a per-session file retires only that session's goal.
printf 'SESSION: %s\nDONE\nFinished.\n' "${sid_a}" >"${par}/.claude/active-goal.${sid_a}"
check "DONE in a per-session file allows that session to stop" allow \
	"$(stop_in "${sid_a}" par5 "${par}")"
check "the other session is unaffected by that DONE" block \
	"$(stop_in "${sid_b}" par6 "${par}")"

# LAST line on purpose -- see the sibling companions.
goal_hook_cases_loaded=1
