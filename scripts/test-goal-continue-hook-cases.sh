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

# ── 5. the budget counts no-progress stops ─────────────────────────────────
#
# The budget bounded CONTINUATIONS per user message, so an agent doing real
# work shared a ceiling of three with an agent emitting status reports.
# Measured on a live six-lane run: every one of that session's eight prompt
# counters read 3, and the run that finally ended had made exactly one tool
# call since the previous nudge. The ceiling released it, not an escape --
# which reads like a clean finish and is not one.
#
# So progress resets the soft budget, and a hard ceiling still bounds the loop:
# an agent making one token call per turn cannot spin forever.

tx="${work}/transcript.jsonl"
: >"${tx}"

tx_payload() { # prompt_id
	PID="$1" SID="${sid}" CWD="${work}" TX="${tx}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": os.environ["PID"],
                  "stop_hook_active": False, "cwd": os.environ["CWD"],
                  "transcript_path": os.environ["TX"],
                  "hook_event_name": "Stop"}))
'
}
work_happened() { # append one assistant turn that actually called a tool
	printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{}}]}}' >>"${tx}"
}
said_something() { # append an assistant turn that only talked
	printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"here is a status report"}]}}' >>"${tx}"
}

printf 'Grind through six lanes.\n' >"${goal}"

# An agent that keeps working keeps getting handed the goal, well past three.
blocks=0
for _ in 1 2 3 4 5 6; do
	work_happened
	printf '%s' "$(run "$(tx_payload progress)")" | rg -q '"block"' && blocks=$((blocks + 1))
done
if [[ "${blocks}" -eq 6 ]]; then
	ok "progress between stops keeps the budget from running out"
else
	no "progress between stops keeps the budget from running out (blocked ${blocks}/6)"
fi

# An agent that only narrates still runs out at three -- the original bound,
# and the behaviour the hook was written for.
: >"${tx}"
noprog=0
for _ in 1 2 3 4 5; do
	said_something
	printf '%s' "$(run "$(tx_payload narrate)")" | rg -q '"block"' && noprog=$((noprog + 1))
done
if [[ "${noprog}" -eq 3 ]]; then
	ok "a stop with no tool calls still burns the budget, out at three"
else
	no "a stop with no tool calls still burns the budget, out at three (blocked ${noprog}/5)"
fi

# Mixed: two narrated stops then real work, and the budget is whole again.
: >"${tx}"
said_something; run "$(tx_payload mixed)" >/dev/null
said_something; run "$(tx_payload mixed)" >/dev/null
work_happened
check "work after two narrated stops restores the budget" block "$(run "$(tx_payload mixed)")"
said_something
check "and the restored budget is a full one" block "$(run "$(tx_payload mixed)")"

# The hard ceiling is what stops a token tool call per turn from looping
# forever. Set low here so the case is cheap; the default is 20.
: >"${tx}"
ceil=0
for _ in 1 2 3 4 5 6 7 8; do
	work_happened
	out_c="$(printf '%s' "$(tx_payload ceiling)" | CLAUDE_GOAL_MAX_TOTAL=4 \
		CLAUDE_GOAL_FILE="${goal}" bash "${HOOK}" 2>/dev/null)"
	printf '%s' "${out_c}" | rg -q '"block"' && ceil=$((ceil + 1))
done
if [[ "${ceil}" -eq 4 ]]; then
	ok "the hard ceiling bounds the loop even under continuous progress"
else
	no "the hard ceiling bounds the loop even under continuous progress (blocked ${ceil}/8)"
fi

# No transcript, or an unreadable one, must behave exactly as before: three and
# out. A progress signal that fails open would remove the bound entirely.
notx="$(printf '%s' "$(payload notx)" | CLAUDE_GOAL_FILE="${goal}" bash "${HOOK}" 2>/dev/null)"
check "a payload with no transcript_path still blocks" block "${notx}"
missing=0
for _ in 1 2 3 4 5; do
	out_m="$(PID=gone SID="${sid}" CWD="${work}" TX="${work}/nope.jsonl" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": os.environ["PID"],
                  "stop_hook_active": False, "cwd": os.environ["CWD"],
                  "transcript_path": os.environ["TX"], "hook_event_name": "Stop"}))
' | CLAUDE_GOAL_FILE="${goal}" bash "${HOOK}" 2>/dev/null)"
	printf '%s' "${out_m}" | rg -q '"block"' && missing=$((missing + 1))
done
if [[ "${missing}" -eq 3 ]]; then
	ok "a missing transcript falls back to the plain three-stop bound"
else
	no "a missing transcript falls back to the plain three-stop bound (blocked ${missing}/5)"
fi

# When the budget is what released the stop, say so. It looked like a clean
# finish on a live run, and nobody could tell the difference afterwards.
: >"${tx}"
for _ in 1 2 3; do said_something; run "$(tx_payload audit)" >/dev/null; done
said_something
audit_err="$(printf '%s' "$(tx_payload audit)" | CLAUDE_GOAL_FILE="${goal}" \
	bash "${HOOK}" 2>&1 >/dev/null)"
if printf '%s' "${audit_err}" | rg -qi 'budget|continuation'; then
	ok "the release-by-budget is announced on stderr"
else
	no "the release-by-budget is announced on stderr (got: ${audit_err})"
fi

# A counter written by the previous version holds a bare number. Reading it
# must not crash or silently reset the bound to zero.
: >"${tx}"
printf '2' >"${state_probe:=${TMPDIR:-/tmp}/claude-goal-nudge-${sid}-legacy}"
said_something
check "a legacy counter file is still honoured" block "$(run "$(tx_payload legacy)")"
said_something
legacy_out="$(run "$(tx_payload legacy)")"
if printf '%s' "${legacy_out}" | rg -q '"block"'; then
	no "a legacy counter of 2 leaves exactly one nudge"
else
	ok "a legacy counter of 2 leaves exactly one nudge"
fi
