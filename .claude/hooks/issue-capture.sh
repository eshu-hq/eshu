#!/usr/bin/env bash
# PostToolUse hook (Bash): an issue filed during a goal belongs to that goal.
#
# The failure this exists to stop: an agent working a goal files a GitHub issue
# for something it found, treats the filing as the deferral, and the owner meets
# "these were filed" at the end of the goal -- 100+ of them piled up. AGENTS.md
# already says every issue created during a goal joins the goal's backlog;
# nothing enforced it. This hook is the capture half. goal-continue.sh is the
# enforcement half (it refuses DONE while a captured issue is open) and
# goal-refresh.sh keeps the debt in front of the agent every turn.
#
# Contract:
#   stdin  : a Bash PostToolUse payload. Claude: tool_response is an object with
#            `stdout`. Muse: tool_response is a JSON-encoded STRING whose object
#            has `output`. Both recorded live (scripts/testdata/issue-capture/).
#   stdout : {"hookSpecificOutput":{"hookEventName":"PostToolUse",
#             "additionalContext":"..."}} when an issue was captured, else nothing
#   stderr : one line per captured issue, so the owner can see it; with no live
#            goal, one `no live goal ... not ledgered` line per issue instead
#   exit 0 : always. A capture must never be the reason a session breaks.
#
# Acts only inside an Eshu checkout, and only when there is a goal to attach the
# issue to (otherwise it says so on stderr and writes nothing). Which ledger, and why, is lib/goal-issues.sh (gi_capture_ledger);
# what counts as "created an issue" is lib/goal-issues-extract.py.

set -uo pipefail

command -v python3 >/dev/null 2>&1 || exit 0
[ "${CLAUDE_GOAL_OFF:-0}" = "1" ] && exit 0
payload="$(cat)"
[ -n "${payload}" ] || exit 0
# This runs after EVERY Bash call. A payload that never says "issues" cannot
# hold an issue URL or an issue-creating command, so skip the interpreter.
case "${payload}" in *issues*) ;; *) exit 0 ;; esac

libdir="$(dirname "${BASH_SOURCE[0]}")/lib"
# shellcheck source=.claude/hooks/lib/skill-nudge-lib.sh
. "${libdir}/skill-nudge-lib.sh" 2>/dev/null || exit 0
# shellcheck source=.claude/hooks/lib/goal-issues.sh
. "${libdir}/goal-issues.sh" 2>/dev/null || exit 0

extracted="$(printf '%s' "${payload}" | python3 "${libdir}/goal-issues-extract.py" 2>/dev/null)" || exit 0
[ -n "${extracted}" ] || exit 0
{
	IFS= read -r session_id
	IFS= read -r cwd
} <<<"${extracted}"
refs="$(printf '%s\n' "${extracted}" | tail -n +3)"
[ -n "${refs}" ] || exit 0

# Only an Eshu checkout has goals worth attaching to; anywhere else stay silent.
eshu_root_path "${cwd}" >/dev/null || exit 0
# No live goal: nothing to hold the issue to, and an orphan nobody claims would
# be adopted by the next unrelated goal. Not silent, though -- one stderr line
# per issue, so the owner can tell it was seen and deliberately not ledgered.
if ! gi_capture_ledger "${session_id}" "${cwd}"; then
	worktree="$(eshu_root_path "${cwd}")"
	while IFS= read -r ref; do
		[ -n "${ref}" ] || continue
		printf 'issue-capture: no live goal in %s; %s not ledgered\n' "${worktree}" "${ref}" >&2
	done <<<"${refs}"
	exit 0
fi

# shellcheck disable=SC2086 # refs are owner/repo#N tokens, one per line
added="$(python3 "${GI_PY}" capture "${GI_LEDGER}" ${refs} 2>/dev/null)" || exit 0
[ -n "${added}" ] || exit 0

case "${GI_KIND}" in
	goal) where="this goal's backlog" ;;
	lead) where="the backlog of the one live goal in this checkout (this session has no goal of its own)" ;;
	orphan) where="the orphan ledger, because more than one goal is live here and guessing would pin it on the wrong lead; the lead's next prompt claims it" ;;
esac

msg=""
while IFS= read -r ref; do
	[ -n "${ref}" ] || continue
	number="${ref#*#}"
	msg="${msg}Issue ${ref} is in ${where}. Work it now (dispatch a lane), or write \`DEFER: #${number} <ruling path>\` in ${GI_LEDGER} -- the path must be an existing, non-empty file that names #${number}. The Stop hook refuses DONE while it is open.
"
	printf 'issue-capture: %s -> %s (%s)\n' "${ref}" "${GI_LEDGER}" "${GI_KIND}" >&2
done <<<"${added}"

MSG="${msg%$'\n'}" python3 -c '
import json, os
print(json.dumps({"hookSpecificOutput": {
    "hookEventName": "PostToolUse",
    "additionalContext": os.environ["MSG"],
}}))
' 2>/dev/null || exit 0

exit 0
