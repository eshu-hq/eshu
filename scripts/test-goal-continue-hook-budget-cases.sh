#!/usr/bin/env bash
# Sourced by scripts/test-goal-continue-hook.sh -- not run on its own.
#
# The Stop mirror's BUDGET half: the progress-aware continuation budget, the
# transcript-scanning progress detector, and everything the review rounds
# turned up about them. Split from the consent/parallel-session companion when
# that file crossed the repo's 500-line cap -- the third split on this branch,
# and the reason the cap is worth having rather than an annoyance.
#
# A trigger path for the agent-canon gate; its sentinel is the last line,
# asserted by test-agent-hooks.sh along with every sibling. Append ABOVE it.

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

# The ceiling no longer bounds a WORKING agent -- it resets on progress, by the
# owner's decision, because being cut off mid-goal costs more than the residual
# risk. Even a deliberately tiny ceiling must not release a session that is
# making tool calls. What still bounds the loop is the soft budget below.
: >"${tx}"
ceil=0
for _ in 1 2 3 4 5 6 7 8; do
	work_happened
	out_c="$(printf '%s' "$(tx_payload ceiling)" | CLAUDE_GOAL_MAX_TOTAL=4 \
		CLAUDE_GOAL_FILE="${goal}" bash "${HOOK}" 2>/dev/null)"
	printf '%s' "${out_c}" | rg -q '"block"' && ceil=$((ceil + 1))
done
if [[ "${ceil}" -eq 8 ]]; then
	ok "even a tiny ceiling does not release a session that keeps working"
else
	no "even a tiny ceiling does not release a session that keeps working (blocked ${ceil}/8)"
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
if printf '%s' "${audit_err}" | rg -qi 'no progress'; then
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
# ── 8. the ceiling resets on progress too ──────────────────────────────────
#
# The owner's call, and it inverts what the ceiling was for. `total` used to
# increment unconditionally, so a session making real progress across one long
# user message was still released at 20 -- release-by-exhaustion moved from 3
# to 20 rather than removed, which is what both PR reviewers said and what the
# docs admitted. An agent that keeps working must never be handed back to the
# owner by the budget.
#
# What still bounds the loop is the SOFT budget: a stop with no tool calls
# since the last nudge burns one, and three of those release it. The residual
# risk is an agent making one token tool call per turn forever. That is
# deliberate and it is the owner's trade: being cut off mid-goal cost more than
# that risk does.
: >"${tx}"
long_run=0
for _ in $(seq 1 30); do
	work_happened
	printf '%s' "$(run "$(tx_payload longrun)")" | rg -q '"block"' && long_run=$((long_run + 1))
done
if [[ "${long_run}" -eq 30 ]]; then
	ok "a session making progress is never released by the ceiling"
else
	no "a session making progress is never released by the ceiling (blocked ${long_run}/30)"
fi

# The soft budget is what still bounds a stuck one, unchanged at three.
: >"${tx}"
stuck=0
for _ in 1 2 3 4 5; do
	said_something
	printf '%s' "$(run "$(tx_payload stuckrun)")" | rg -q '"block"' && stuck=$((stuck + 1))
done
if [[ "${stuck}" -eq 3 ]]; then
	ok "a stuck session is still released after three no-progress stops"
else
	no "a stuck session is still released after three no-progress stops (blocked ${stuck}/5)"
fi

# Mixed: a long working run then three narrated stops must still release, so
# the reset cannot be read as "the budget no longer applies".
: >"${tx}"
for _ in $(seq 1 25); do work_happened; run "$(tx_payload mixedlong)" >/dev/null; done
mixed=0
for _ in 1 2 3 4; do
	said_something
	printf '%s' "$(run "$(tx_payload mixedlong)")" | rg -q '"block"' && mixed=$((mixed + 1))
done
# Two, not three: the final WORKING stop reset the counters and then spent one
# itself, so the narrated phase starts at one. The point of the case is that a
# long working run does not buy immunity -- the budget still closes.
if [[ "${mixed}" -eq 2 ]]; then
	ok "after a long working run, narrated stops still release it"
else
	no "after a long working run, narrated stops still release it (blocked ${mixed}/4)"
fi

# The release has to be a HANDOFF, not a shrug. When the budget lets go, the
# owner is about to be interrupted, so the message must say why and what would
# have avoided it -- the agent never writing DONE or BLOCKED is the actual
# cause of most of these releases.
: >"${tx}"
for _ in 1 2 3; do said_something; run "$(tx_payload handoff)" >/dev/null; done
said_something
handoff_err="$(printf '%s' "$(tx_payload handoff)" | CLAUDE_GOAL_FILE="${goal}" \
	bash "${HOOK}" 2>&1 >/dev/null)"
if printf '%s' "${handoff_err}" | rg -qi 'no progress'; then
	ok "the release says it was no-progress stops that spent the budget"
else
	no "the release says it was no-progress stops that spent the budget (got: ${handoff_err})"
fi
if printf '%s' "${handoff_err}" | rg -q 'DONE' && printf '%s' "${handoff_err}" | rg -q 'BLOCKED'; then
	ok "the release names the two escapes the agent should have used"
else
	no "the release names the two escapes the agent should have used"
fi

# And the LAST nudge before release must warn, so the agent gets one chance to
# retire or block the goal itself rather than landing on the owner.
: >"${tx}"
said_something; run "$(tx_payload warn)" >/dev/null
said_something; run "$(tx_payload warn)" >/dev/null
said_something
last_out="$(run "$(tx_payload warn)")"
if printf '%s' "${last_out}" | rg -qi 'last continuation'; then
	ok "the final nudge warns that the next stop will be allowed"
else
	no "the final nudge warns that the next stop will be allowed"
fi

# LAST line on purpose -- see the sibling companions.
goal_hook_budget_cases_loaded=1
