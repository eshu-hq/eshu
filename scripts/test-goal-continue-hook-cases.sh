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
goal_hook_cases_loaded=1

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

# ── 6. what the reviewers proved wrong ─────────────────────────────────────
#
# Three defects found by execution, not by reading. Each case below is the
# probe that found one.

# A substring test read "install deps" as blanket consent, because it contains
# "all". Every value in between the two the suite happened to test -- `all` and
# `push` -- was undefended, and the discriminator has to be whole tokens.
for narrow in 'install deps' 'call the API' 'allow push' 'fallback to compose' 'recall the baseline'; do
	printf 'CONSENT: %s\nDo the work.\n' "${narrow}" >"${goal}"
	nout="$(run "$(payload "narrow-${narrow%% *}")")"
	if printf '%s' "${nout}" | rg -qi 'you need consent for an irreversible act'; then
		ok "CONSENT: ${narrow} narrows rather than going blanket"
	else
		no "CONSENT: ${narrow} narrows rather than going blanket"
	fi
done

# The blanket forms must still be blanket, including inside a list.
for blanket in 'all' '*' 'push, all' 'ALL'; do
	printf 'CONSENT: %s\nDo the work.\n' "${blanket}" >"${goal}"
	bout="$(run "$(payload "blanket-${blanket}")")"
	if printf '%s' "${bout}" | rg -qi 'you need consent for an irreversible act'; then
		no "CONSENT: ${blanket} is still blanket"
	else
		ok "CONSENT: ${blanket} is still blanket"
	fi
done

# A CONSENT line above a SESSION header shadowed the ownership escape: the
# stripping re-checked DONE but not SESSION, so a foreign session was handed
# another session's goal AND its consent grant. Verbatim the leak the header
# exists to stop.
printf 'CONSENT: push\nSESSION: somebody-else\nTheir private objective.\n' >"${goal}"
shadow="$(run "$(payload shadow)")"
check "a CONSENT line cannot shadow the SESSION escape" allow "${shadow}"
if printf '%s' "${shadow}" | rg -q 'Their private objective'; then
	no "a foreign goal is not leaked through a CONSENT line"
else
	ok "a foreign goal is not leaked through a CONSENT line"
fi

# Consent above MY session header must still work.
printf 'CONSENT: push\nSESSION: %s\nMy objective.\n' "${sid}" >"${goal}"
mine="$(run "$(payload mineconsent)")"
check "a CONSENT line above my own SESSION header still blocks" block "${mine}"
if printf '%s' "${mine}" | rg -qi 'already granted'; then
	ok "consent above my own header is honoured"
else
	no "consent above my own header is honoured"
fi

# The two sides of the format disagreed on leading whitespace: the refresher
# stripped an indented CONSENT line and the Stop hook did not, so the same file
# meant two different things to the producer and the consumer.
printf '  CONSENT: push\nDo the work.\n' >"${goal}"
indent="$(run "$(payload indent)")"
if printf '%s' "${indent}" | rg -q 'CONSENT:'; then
	no "an indented CONSENT line is read the same as a flush one"
else
	ok "an indented CONSENT line is read the same as a flush one"
fi

# Honouring consent removes the requirement to ask before push, merge, deploy
# and delete. The strictly weaker BLOCKED escape echoes its claim to stderr
# precisely because the agent writes the file; this one echoed nothing.
printf 'CONSENT: push, merge, deploy\nDo the work.\n' >"${goal}"
cerr="$(printf '%s' "$(payload consenterr)" | CLAUDE_GOAL_FILE="${goal}" \
	bash "${HOOK}" 2>&1 >/dev/null)"
if printf '%s' "${cerr}" | rg -q 'push, merge, deploy'; then
	ok "honouring consent is announced on stderr with the acts"
else
	no "honouring consent is announced on stderr with the acts (got: ${cerr})"
fi

# ── 7. what round three proved wrong ───────────────────────────────────────

# The progress detector was pinned to one JSON spelling. A serializer that ever
# wrote `"type": "tool_use"` -- one space -- would silently restore the exact
# behaviour the budget change exists to remove: an agent doing real work
# released by exhaustion, with a green mirror and no stderr.
: >"${tx}"
spaced_blocks=0
for _ in 1 2 3 4 5; do
	printf '%s\n' '{"type": "assistant", "message": {"content": [{"type": "tool_use", "name": "Bash"}]}}' >>"${tx}"
	printf '%s' "$(run "$(tx_payload spaced)")" | rg -q '"block"' && spaced_blocks=$((spaced_blocks + 1))
done
if [[ "${spaced_blocks}" -eq 5 ]]; then
	ok "progress is detected whatever spacing the transcript uses"
else
	no "progress is detected whatever spacing the transcript uses (blocked ${spaced_blocks}/5)"
fi

# The counter path was built from the RAW session id while the goal path used
# the sanitized one, so a session id with a slash made the counter write fail
# and `|| exit 0` allowed the stop -- enforcement silently off, which is the
# failure the sanitizing exists to prevent.
printf 'A goal for a session with an awkward id.\n' >"${goal}"
# Run-unique, not a literal: the counter is keyed on (session_id, prompt_id)
# and lives in TMPDIR, so a hardcoded id passes three runs on a machine and
# fails every run after -- the trap this suite already documents once.
slash_out="$(SID="${sid}/awkward" CWD="${work}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "slash",
                  "stop_hook_active": False, "cwd": os.environ["CWD"],
                  "hook_event_name": "Stop"}))
' | CLAUDE_GOAL_FILE="${goal}" bash "${HOOK}" 2>/dev/null)"
check "a session id containing a slash still blocks" block "${slash_out}"

# A transcript that QUOTES the pattern -- an agent reading a transcript, or
# this very diff -- must not earn progress credit. JSON escaping is what makes
# that safe: the embedded copy serializes with backslashes and cannot match.
# Pinned here because it is the property the whole heuristic rests on.
: >"${tx}"
quoted_blocks=0
for _ in 1 2 3 4 5; do
	printf '%s\n' '{"type":"user","message":{"content":[{"type":"tool_result","content":"the pattern is {\"type\":\"tool_use\"} in the file"}]}}' >>"${tx}"
	printf '%s' "$(run "$(tx_payload quoted)")" | rg -q '"block"' && quoted_blocks=$((quoted_blocks + 1))
done
if [[ "${quoted_blocks}" -eq 3 ]]; then
	ok "a transcript quoting the pattern earns no progress credit"
else
	no "a transcript quoting the pattern earns no progress credit (blocked ${quoted_blocks}/5)"
fi

# Applying the header rules twice made the hook consume a header twice: a goal
# whose BODY starts a line with `SESSION:` was read by the second pass as an
# ownership header, the id did not match, and the stop was SILENTLY ALLOWED --
# enforcement off, no output. Worse, it depended on whether a CONSENT line was
# present, so the same file enforced or did not depending on an unrelated line.
printf 'SESSION: %s\nCONSENT: push\nSESSION: is the header we document\nmore goal\n' \
	"${sid}" >"${goal}"
body_out="$(run "$(payload bodyheader)")"
check "a body line starting SESSION: does not disable the hook" block "${body_out}"
if printf '%s' "${body_out}" | rg -q 'is the header we document'; then
	ok "that body line survives into the quoted goal"
else
	no "that body line survives into the quoted goal"
fi

# The control the defect was found with: same file, no CONSENT line. Both must
# behave identically now.
printf 'SESSION: %s\nSESSION: is the header we document\nmore goal\n' "${sid}" >"${goal}"
check "the same goal without a CONSENT line behaves identically" block \
	"$(run "$(payload bodyheader2)")"
