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
# Not an escape, but the same file: `CONSENT: <acts>` (or CLAUDE_GOAL_CONSENT)
# records a permission the owner has already given, and REMOVES "I need consent
# for that" as a reason to stop for the acts it names.
#
# Tests: scripts/test-goal-continue-hook.sh -- run it after any edit here, and
# read its own tally rather than trusting a count written down elsewhere. A
# hook that can refuse to end a session is worth a regression suite, and the
# agent-canon gate runs this one in CI.
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

# Goal file: an explicit override, then this session's own goal in the worktree
# it is in, then the worktree's shared goal, then a single per-user goal. The
# worktree form is what makes concurrent agents in different worktrees able to
# hold different goals at once; the per-session form does the same for agents
# sharing ONE worktree.
#
# The per-session file comes FIRST because sessions are not per-checkout.
# Three agents in one clone shared a single `.claude/active-goal`, and the
# SESSION header -- there so a goal cannot outlive the session that set it --
# meant the two that did not own the file got a silent exit 0: no enforcement,
# and nothing saying so. Measured on a live machine: three sessions in one
# checkout, one goal file, one session actually held to it.
#
# The session id is sanitized before it reaches a path. It arrives in the hook
# payload rather than from the agent, but a value that becomes a filename is
# worth constraining wherever it came from.
sid_safe="$(printf '%s' "${session_id}" | tr -c 'A-Za-z0-9._-' '-')"
goal_file=""
for candidate in \
	"${CLAUDE_GOAL_FILE:-}" \
	"${cwd}/.claude/active-goal.${sid_safe}" \
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

# A goal belongs to the session that set it. goal-refresh.sh writes a
# `SESSION: <id>` header; without parsing it HERE, the header is read as
# ordinary goal text and a later session in the same checkout gets blocked with
# a previous session's stale goal -- the precise leak the header exists to stop.
# Producer and consumer have to agree on the format; implementing it on one
# side only is worse than not having it, because it reads as protection.
#
# An unheaded file is the owner's own hand-written goal and is honoured as-is,
# which keeps the manual workflow working.
case "${first_line}" in
	SESSION:*)
		goal_owner="${first_line#SESSION:}"
		goal_owner="${goal_owner# }"
		[ "${goal_owner}" = "${session_id}" ] || exit 0
		goal="$(printf '%s\n' "${goal}" | tail -n +2)"
		[ -n "${goal}" ] || exit 0
		first_line="$(printf '%s\n' "${goal}" | head -1)"
		case "${first_line}" in
			DONE*|done*) exit 0 ;;
		esac
		;;
esac

# CONSENT: <acts> is the owner writing down a permission the agent would
# otherwise stop to ask for. The refusal used to offer "you need consent for an
# irreversible act" as a legitimate reason to stop, unconditionally -- correct
# by default, and wrong once the owner has already said yes. An agent that had
# been told to go ahead still read that bullet, stopped, and got the same
# bullet handed back.
#
# The acts are echoed back to the agent rather than acted on here, because this
# hook grants nothing by itself: the permission is the owner's, written in the
# owner's file, and the hook only stops it from being forgotten. `CONSENT: all`
# is the blanket form. An empty `CONSENT:` grants nothing, the same way an
# empty `BLOCKED:` releases nothing -- the agent writes this file too.
consent=""
if printf '%s\n' "${goal}" | python3 -c '
import sys
sys.exit(0 if any(l.lstrip().lower().startswith("consent:") for l in sys.stdin) else 1)
' 2>/dev/null; then
	stripped=""
	while IFS= read -r line; do
		case "${line}" in
			[Cc][Oo][Nn][Ss][Ee][Nn][Tt]:*)
				acts="${line#*:}"
				acts="${acts#"${acts%%[![:space:]]*}"}"
				[ -n "${acts}" ] && consent="${consent:+${consent}, }${acts}"
				;;
			*)
				stripped="${stripped}${line}
"
				;;
		esac
	done < <(printf '%s\n' "${goal}")
	goal="${stripped%$'\n'}"
	[ -n "${goal}" ] || exit 0
	first_line="$(printf '%s\n' "${goal}" | head -1)"
	case "${first_line}" in
		DONE*|done*) exit 0 ;;
	esac
fi

# A launcher that already knows what its run may do can grant without editing a
# file that concurrent sessions in the same checkout share.
if [ -n "${CLAUDE_GOAL_CONSENT:-}" ]; then
	consent="${consent:+${consent}, }${CLAUDE_GOAL_CONSENT}"
fi

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

# The consent bullet is the one line of the refusal that changes with what the
# owner already granted. Blanket consent retires it; a named list narrows it to
# everything NOT on that list; no consent leaves it as it was.
consent_note=""
consent_bullet="
  - you need consent for an irreversible act (push, merge, deploy, delete, data mutation, anything outward-facing)"
if [ -n "${consent}" ]; then
	consent_note="

OWNER CONSENT ALREADY GRANTED for: ${consent}
Do those yourself now. Stopping to ask again for something on that list is not a valid stop -- the owner already answered."
	case "$(printf '%s' "${consent}" | tr '[:upper:]' '[:lower:]')" in
		*all*|*'*'*)
			consent_bullet=""
			;;
		*)
			consent_bullet="
  - you need consent for an irreversible act NOT on the granted list above"
			;;
	esac
fi

reason="${reason_head}

ACTIVE GOAL (${goal_file}):
${goal}${consent_note}

Continue it now. Take the next concrete action yourself rather than describing what could be done, and do not ask the owner a question that the code, a local doc, or a cheap experiment can settle.

Stop for real only when one of these is true, and say which:
  - the goal is met -- then put DONE on the first line of ${goal_file}${consent_bullet}
  - you are blocked on something no action of yours can clear -- name it exactly

Continuations left for this message: ${remaining}."

printf '%s' "${reason}" | python3 -c '
import json, sys
print(json.dumps({"decision": "block", "reason": sys.stdin.read()}))
' 2>/dev/null || exit 0

exit 0
