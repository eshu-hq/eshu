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
	"${nw}/.agents/skills/eshu-issue-driver"

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
mkdir -p "${nw2}/.claude" "${nw2}/.agents/skills/golang-engineering"
submit "${nudge_sid}" '/goal ship the not-a-real-skill feature' "${nw2}" >/dev/null
n3="$(injected "$(submit "${nudge_sid}" 'carry on' "${nw2}")")"
if printf '%s' "${n3}" | rg -q 'load `'; then
	no "a name that is not a real skill gets no nudge"
else
	ok "a name that is not a real skill gets no nudge"
fi

# ── word boundary: a longer identifier does not fire the shorter skill id ───
nw3="${work}/skillnudge-boundary"
mkdir -p "${nw3}/.claude" "${nw3}/.agents/skills/golang-engineering"
submit "${nudge_sid}" '/goal read the golang-engineering-notes doc first' "${nw3}" >/dev/null
n4="$(injected "$(submit "${nudge_sid}" 'carry on' "${nw3}")")"
if printf '%s' "${n4}" | rg -q 'load `golang-engineering` now'; then
	no "a longer identifier does not fire a shorter skill id's nudge"
else
	ok "a longer identifier does not fire a shorter skill id's nudge"
fi

rm -f "${nudge_marker}"

# LAST line on purpose -- see the sibling companions.
goal_refresh_skill_nudge_cases_loaded=1
