#!/usr/bin/env bash
# Sourced by scripts/test-goal-refresh-hook.sh -- not run on its own.
#
# A goal that names a project skill by id is worth little if that skill's
# content never made it into context. This proves the nudge goal-refresh.sh
# now appends to its per-turn restatement: named-and-unloaded gets a nudge,
# named-and-loaded gets none, and a name that is not a real skill gets none
# either. It reuses the exact marker skill-nudge.sh and skill-loaded.sh already
# read and write, keyed on the first 12 characters of the session id.
#
# A trigger path for the agent-canon gate in specs/ci-gates.v1.yaml, so editing
# it alone still selects the gate that runs it. The sentinel is what makes the
# parent notice if it ever stops being sourced: a trigger makes a gate RUN, it
# cannot make a gate FAIL.

nudge_sid="skillnudge-$$"
nudge_sid12="$(printf '%s' "${nudge_sid}" | cut -c1-12)"
nudge_marker="/tmp/claude-skill-loaded-${nudge_sid12}-golang-engineering"
rm -f "${nudge_marker}"

nw="${work}/skillnudge"
mkdir -p "${nw}/.claude" "${nw}/.agents/skills/golang-engineering" \
	"${nw}/.agents/skills/eshu-issue-driver" "${nw}/.agents/skills/eshu-code-review"

# ── named, unloaded: nudge ──────────────────────────────────────────────────
submit "${nudge_sid}" '/goal fix the bug per the golang-engineering skill' "${nw}" >/dev/null
n1="$(injected "$(submit "${nudge_sid}" 'carry on' "${nw}")")"
if printf '%s' "${n1}" | rg -q 'load `golang-engineering` now'; then
	ok "a named, unloaded skill gets the nudge"
else
	no "a named, unloaded skill gets the nudge (got: ${n1})"
fi

# ── named, loaded: no nudge ─────────────────────────────────────────────────
touch "${nudge_marker}"
n2="$(injected "$(submit "${nudge_sid}" 'carry on again' "${nw}")")"
if printf '%s' "${n2}" | rg -q 'load `golang-engineering` now'; then
	no "a named, loaded skill gets no nudge"
else
	ok "a named, loaded skill gets no nudge"
fi
rm -f "${nudge_marker}"

# ── not a real skill: no nudge ──────────────────────────────────────────────
nw2="${work}/skillnudge-notaskill"
mkdir -p "${nw2}/.claude" "${nw2}/.agents/skills/golang-engineering" \
	"${nw2}/.agents/skills/eshu-code-review"
submit "${nudge_sid}" '/goal ship the not-a-real-skill feature' "${nw2}" >/dev/null
n3="$(injected "$(submit "${nudge_sid}" 'carry on' "${nw2}")")"
if printf '%s' "${n3}" | rg -q 'load `'; then
	no "a name that is not a real skill gets no nudge"
else
	ok "a name that is not a real skill gets no nudge"
fi

# ── word boundary: a longer identifier does not fire the shorter skill id ───
nw3="${work}/skillnudge-boundary"
mkdir -p "${nw3}/.claude" "${nw3}/.agents/skills/golang-engineering" \
	"${nw3}/.agents/skills/eshu-code-review"
submit "${nudge_sid}" '/goal read the golang-engineering-notes doc first' "${nw3}" >/dev/null
n4="$(injected "$(submit "${nudge_sid}" 'carry on' "${nw3}")")"
if printf '%s' "${n4}" | rg -q 'load `golang-engineering` now'; then
	no "a longer identifier does not fire a shorter skill id's nudge"
else
	ok "a longer identifier does not fire a shorter skill id's nudge"
fi

rm -f "${nudge_marker}"

# ── cwd one level below the root: the scan must still find .agents/skills ───
#
# codex#4018964366: goal-refresh.sh used to scan "${cwd}/.agents/skills"
# directly, which only exists AT the project root. A cwd of "<repo>/go" (or
# any deeper worktree subdirectory) has no .agents/skills of its own, so the
# nudge silently never fired there -- exactly the gap skill-nudge.sh's
# eshu_root walk-up already closed for the PreToolUse hook.
nw4="${work}/skillnudge-subdir"
mkdir -p "${nw4}/.agents/skills/golang-engineering" \
	"${nw4}/.agents/skills/eshu-code-review" "${nw4}/go/.claude"
submit "${nudge_sid}" '/goal fix the bug per the golang-engineering skill' "${nw4}/go" >/dev/null
n5="$(injected "$(submit "${nudge_sid}" 'carry on' "${nw4}/go")")"
if printf '%s' "${n5}" | rg -q 'load `golang-engineering` now'; then
	ok "a cwd one level below the root still gets the nudge"
else
	no "a cwd one level below the root still gets the nudge (got: ${n5})"
fi
rm -f "${nudge_marker}"

# ── cwd-less: silent, never an error ────────────────────────────────────────
#
# The hook supports a payload with no "cwd" at all (goal-refresh.sh's
# have_cwd=0 path, e.g. a $HOME-only goal read with no worktree in play).
# There is nothing to walk up from, so the nudge must stay silent rather than
# resolve the wrong root or error -- documented as the "nothing to nudge here"
# outcome, not a bug. A goal is still refreshed via the $HOME fallback so this
# actually exercises the have_cwd=0 branch instead of exiting before reaching it.
cwdless_sid="skillnudge-cwdless-$$"
cwdless_home="${work}/skillnudge-cwdless-home"
mkdir -p "${cwdless_home}/.claude"
printf 'SESSION: %s\nfix the bug per the golang-engineering skill\n' \
	"${cwdless_sid}" >"${cwdless_home}/.claude/active-goal"
cwdless_payload="$(SID="${cwdless_sid}" PROMPT='carry on' python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "p1",
                  "prompt": os.environ["PROMPT"],
                  "hook_event_name": "UserPromptSubmit"}))
')"
cwdless_err="$(printf '%s' "${cwdless_payload}" | HOME="${cwdless_home}" bash "${REFRESH}" 2>&1 >/dev/null)"
cwdless_out="$(printf '%s' "${cwdless_payload}" | HOME="${cwdless_home}" bash "${REFRESH}" 2>/dev/null)"
if [[ -z "${cwdless_err}" ]]; then
	ok "a cwd-less payload emits no error while nudging"
else
	no "a cwd-less payload emits no error while nudging (got: ${cwdless_err})"
fi
if printf '%s' "$(injected "${cwdless_out}")" | rg -q 'fix the bug per the golang-engineering skill' &&
	! printf '%s' "$(injected "${cwdless_out}")" | rg -q 'load `golang-engineering` now'; then
	ok "a cwd-less payload still refreshes the goal but fires no skill nudge"
else
	no "a cwd-less payload still refreshes the goal but fires no skill nudge (got: ${cwdless_out})"
fi

# LAST line on purpose -- see the sibling companions.
goal_refresh_skill_nudge_cases_loaded=1
