#!/usr/bin/env bash
# Stop hook: if this session has a declared, unfinished goal, do not let the
# turn end silently -- hand the goal back to the agent and make it continue.
#
# The failure this exists to stop: an agent with a multi-step goal stops after
# one step, reports, and waits. The owner then has to type "continue" over and
# over. Prose in AGENTS.md did not fix that, because prose is advice the model
# can drift past. A Stop hook is the enforcement point.
#
# Contract (verified against a real captured Stop payload, not guessed):
#   stdin  : {session_id, prompt_id, cwd, transcript_path, stop_hook_active,
#             last_assistant_message, hook_event_name:"Stop", ...}
#   stdout : {"decision":"block","reason":"..."} to refuse the stop, or nothing
#   exit 0 : always. This hook must never be the reason a session breaks.
#
# Escape hatches, in order of precedence:
#   - no goal file, or an empty one              -> stop allowed
#   - goal file whose first line starts DONE     -> stop allowed
#   - first line `BLOCKED: <reason>`             -> stop allowed (see below)
#   - first line `BLOCKED: ... WATCH=<dead pid>` -> stop REFUSED
#   - CLAUDE_GOAL_OFF=1                          -> stop allowed
#   - budget spent for this prompt_id            -> stop allowed
#
# Tests: scripts/test-goal-continue-hook.sh (19 cases). Run it after any edit
# here -- a hook that can refuse to end a session is worth a regression suite.
# The agent-canon gate runs it in CI.
#
# The budget is keyed on prompt_id, so it is "at most N continuations per user
# message", not "N per session". A new user message gets a fresh budget, and a
# stuck agent can never spin forever on one prompt.

set -uo pipefail

MAX_NUDGES="${CLAUDE_GOAL_MAX_NUDGES:-3}"

payload="$(cat)"

# Degrade to silence rather than break the session if we cannot parse.
command -v python3 >/dev/null 2>&1 || exit 0
[ -n "${payload}" ] || exit 0
[ "${CLAUDE_GOAL_OFF:-0}" = "1" ] && exit 0

# Fields arrive one per line, NOT whitespace-split. An earlier version
# collapsed spaces to underscores so `read -r a b c d` would see four tokens,
# which silently corrupted `cwd`: a checkout under "/Users/you/My Projects"
# became "My_Projects", the worktree-scoped goal lookup missed, and the
# per-worktree feature quietly did nothing. Process substitution, not a
# here-string, for the same pipe-buffer reason as everywhere else in this repo.
{
	read -r session_id
	read -r prompt_id
	read -r stop_active
	IFS= read -r cwd
} < <(printf '%s' "${payload}" | python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)
except Exception:
    print("-"); print("-"); print("False"); print("-")
    raise SystemExit(0)
def g(k, dflt="-"):
    v = d.get(k)
    if v is None or v == "":
        return dflt
    # Only newlines are unrepresentable in a line-delimited field; spaces are
    # preserved, which is the entire point of this shape.
    return str(v).replace("\r", " ").replace("\n", " ")
print(g("session_id"))
print(g("prompt_id"))
print(g("stop_hook_active", "False"))
print(g("cwd"))
' 2>/dev/null)

[ "${session_id:--}" = "-" ] && exit 0

# Goal file: an explicit override, then the worktree this session is in, then a
# single per-user goal. The worktree form is what makes concurrent agents in
# different worktrees able to hold different goals at once.
goal_file=""
for candidate in \
	"${CLAUDE_GOAL_FILE:-}" \
	"${cwd}/.claude/active-goal" \
	"${HOME}/.claude/active-goal"; do
	[ -n "${candidate}" ] || continue
	if [ -f "${candidate}" ]; then
		goal_file="${candidate}"
		break
	fi
done
[ -n "${goal_file}" ] || exit 0

goal="$(sed -e 's/[[:space:]]*$//' "${goal_file}" 2>/dev/null)"
[ -n "${goal}" ] || exit 0

# An owner marks a goal finished by putting DONE on its first line. Checked
# before the budget so a finished goal never costs a nudge.
first_line="$(printf '%s\n' "${goal}" | head -1)"
case "${first_line}" in
	DONE*|done*) exit 0 ;;
esac

# BLOCKED: <reason> releases the stop when the work is genuinely waiting on
# something outside this machine -- a CI run, a remote queue, another person.
# Without it the hook nags for local action that does not exist, which teaches
# the agent to treat the nudge as noise.
#
# The agent writes this file, so a bare claim would be a self-issued permission
# slip. Two things keep it honest:
#
#   1. The reason must be non-empty, and it is echoed to stderr so the owner
#      reads the exact claim rather than just seeing the turn end.
#   2. `WATCH=<pid>` is checked. If the named waiter is DEAD the stop is
#      REFUSED -- a dead watcher means nothing will wake the agent, so waiting
#      is the bug rather than the excuse. Naming a live one is the strong form.
case "${first_line}" in
	BLOCKED:*|blocked:*)
		blocked_reason="${first_line#*:}"
		blocked_reason="${blocked_reason#"${blocked_reason%%[![:space:]]*}"}"
		if [ -n "${blocked_reason}" ]; then
			watch_pid=""
			case "${blocked_reason}" in
				*WATCH=*)
					watch_pid="${blocked_reason##*WATCH=}"
					watch_pid="${watch_pid%%[![:digit:]]*}"
					;;
			esac
			if [ -n "${watch_pid}" ] && ! kill -0 "${watch_pid}" 2>/dev/null; then
				printf 'goal-continue: BLOCKED claims watcher pid %s, which is not running.\n' \
					"${watch_pid}" >&2
				dead_watcher="${watch_pid}"
			else
				printf 'goal-continue: stop allowed, BLOCKED: %s\n' "${blocked_reason}" >&2
				exit 0
			fi
		fi
		;;
esac

state_dir="${TMPDIR:-/tmp}"
counter="${state_dir}/claude-goal-nudge-${session_id}-${prompt_id}"
count=0
[ -f "${counter}" ] && count="$(cat "${counter}" 2>/dev/null || echo 0)"
case "${count}" in
	''|*[!0-9]*) count=0 ;;
esac

if [ "${count}" -ge "${MAX_NUDGES}" ]; then
	exit 0
fi
printf '%s' "$((count + 1))" >"${counter}" 2>/dev/null || exit 0

remaining="$((MAX_NUDGES - count - 1))"

if [ -n "${dead_watcher:-}" ]; then
	reason_head="Your goal file claims BLOCKED with WATCH=${dead_watcher}, but that watcher process is NOT running. Nothing will wake you, so waiting is not an option -- re-arm the watcher or do the work now."
else
	reason_head="You are stopping with an active goal still open. Do not end the turn on a status report."
fi

reason="${reason_head}

ACTIVE GOAL (${goal_file}):
${goal}

Continue it now. Take the next concrete action yourself rather than describing what could be done, and do not ask the owner a question that the code, a local doc, or a cheap experiment can settle.

Stop for real only when one of these is true, and say which:
  - the goal is met -- then put DONE on the first line of ${goal_file}
  - you need consent for an irreversible act (push, merge, deploy, delete, data mutation, anything outward-facing)
  - you are blocked on something no action of yours can clear -- name it exactly

Continuations left for this message: ${remaining}."

printf '%s' "${reason}" | python3 -c '
import json, sys
print(json.dumps({"decision": "block", "reason": sys.stdin.read()}))
' 2>/dev/null || exit 0

exit 0
