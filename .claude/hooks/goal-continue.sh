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
# The budget is keyed on prompt_id, so it bounds work per user message, not per
# session. It counts stops that made NO PROGRESS -- real tool calls between
# stops reset it -- with a hard per-message ceiling behind it, so a working
# agent is never released by exhaustion and a stuck one still cannot spin.

set -uo pipefail

MAX_NUDGES="${CLAUDE_GOAL_MAX_NUDGES:-3}"
# Hard ceiling on nudges per user message, whatever the agent is doing. The
# soft budget above resets on progress, so something has to bound the loop.
MAX_TOTAL="${CLAUDE_GOAL_MAX_TOTAL:-20}"

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
	IFS= read -r transcript
} < <(printf '%s' "${payload}" | python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)
except Exception:
    print("-"); print("-"); print("False"); print("-"); print("-")
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
print(g("transcript_path"))
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
# The header rules, applied ONCE and only after CONSENT lines are stripped.
#
# They used to run before the strip, and a CONSENT line above the header then
# shadowed the ownership check. Applying them a SECOND time after the strip
# fixed that and introduced a worse defect: a goal whose BODY started a line
# with `SESSION:` was read by the second pass as an ownership header, the id
# did not match, and the stop was silently ALLOWED -- enforcement off, no
# output, and dependent on whether an unrelated CONSENT line was present.
# Stripping first and parsing once closes both without a second layer.
# Returns non-zero when the stop should be allowed.
apply_session_header() {
	case "${first_line}" in
		SESSION:*)
			goal_owner="${first_line#SESSION:}"
			goal_owner="${goal_owner# }"
			[ "${goal_owner}" = "${session_id}" ] || return 1
			goal="$(printf '%s\n' "${goal}" | tail -n +2)"
			[ -n "${goal}" ] || return 1
			first_line="$(printf '%s\n' "${goal}" | head -1)"
			;;
	esac
	case "${first_line}" in
		DONE*|done*) return 1 ;;
	esac
	return 0
}

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
		# lstrip first: the python detector above uses .lstrip(), and a shell
		# arm that rejected leading whitespace made the refresher and the Stop
		# hook read the same file two different ways.
		trimmed="${line#"${line%%[![:space:]]*}"}"
		case "${trimmed}" in
			[Cc][Oo][Nn][Ss][Ee][Nn][Tt]:*)
				acts="${trimmed#*:}"
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
fi

# A launcher that already knows what its run may do can grant without editing a
# file that concurrent sessions in the same checkout share.
if [ -n "${CLAUDE_GOAL_CONSENT:-}" ]; then
	consent="${consent:+${consent}, }${CLAUDE_GOAL_CONSENT}"
fi

apply_session_header || exit 0

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

# The budget used to count CONTINUATIONS, which put an agent grinding through
# six lanes on the same ceiling of three as an agent emitting status reports.
# Measured on a live run: all eight of that session's prompt counters read 3,
# and the run that ended had made one tool call since the previous nudge. The
# ceiling released it -- which looks exactly like a clean finish afterwards.
#
# So the soft budget counts NO-PROGRESS stops and real work resets it, while a
# hard ceiling still bounds the loop: an agent making one token call per turn
# cannot spin forever. Progress is tool calls appended to the transcript since
# the last nudge, read from a stored byte offset rather than by re-reading the
# file -- these run to 8MB.
state_dir="${TMPDIR:-/tmp}"
# sid_safe, not the raw id: the goal path was sanitized and this one was not,
# so a session id containing a slash made the counter write fail and `|| exit 0`
# ALLOWED the stop -- enforcement silently off, which is the exact failure the
# sanitizing exists to prevent. prompt_id gets the same treatment for the same
# reason.
pid_safe="$(printf '%s' "${prompt_id}" | tr -c 'A-Za-z0-9._-' '-')"
counter="${state_dir}/claude-goal-nudge-${sid_safe}-${pid_safe}"
count=0
total=0
offset=0
if [ -f "${counter}" ]; then
	# `count total offset`. A file written by the previous version holds a bare
	# number; reading it must not crash or silently reset the bound to zero.
	read -r count total offset <"${counter}" 2>/dev/null || true
	case "${count}" in ''|*[!0-9]*) count=0 ;; esac
	case "${total}" in ''|*[!0-9]*) total="${count}" ;; esac
	case "${offset}" in ''|*[!0-9]*) offset=0 ;; esac
fi

# Tool calls in the bytes appended since the last nudge. A transcript that is
# missing or unreadable reports no progress, which leaves the original
# three-stop bound in force -- a progress signal that failed OPEN would remove
# the bound entirely.
#
# The offset is per (session, prompt_id), so the FIRST stop of each user
# message still reads the whole file -- measured at 159-172ms on a real 9.9MB
# transcript -- and only the stops after it read a tail. That first read is
# behaviourally a no-op (the count is already 0 on a fresh counter), so it buys
# nothing; it is left alone because seeding the offset from the file size would
# make work done BEFORE the first nudge of a message invisible to it.
progress=0
new_offset="${offset}"
if [ -n "${transcript:-}" ] && [ "${transcript}" != "-" ] && [ -f "${transcript}" ]; then
	{
		read -r progress
		read -r new_offset
	} < <(TX="${transcript}" OFF="${offset}" python3 -c '
import os, re, sys
path = os.environ["TX"]
try:
    off = int(os.environ.get("OFF") or 0)
    size = os.path.getsize(path)
    if off > size:  # transcript replaced or truncated: rescan from the start
        off = 0
    with open(path, "rb") as fh:
        fh.seek(off)
        tail = fh.read()
    # Tolerate whitespace from the transcript writer. Pinning the exact
    # compact spelling meant one space after a colon silently restored the
    # pre-change behaviour: real work invisible, the agent released by
    # exhaustion, the mirror still green.
    print(len(re.findall(rb"\"type\"\s*:\s*\"tool_use\"", tail)))
    print(size)
except Exception:
    print(0); print(os.environ.get("OFF") or 0)
' 2>/dev/null)
	case "${progress}" in ''|*[!0-9]*) progress=0 ;; esac
	case "${new_offset}" in ''|*[!0-9]*) new_offset="${offset}" ;; esac
fi

[ "${progress}" -gt 0 ] && count=0

if [ "${count}" -ge "${MAX_NUDGES}" ] || [ "${total}" -ge "${MAX_TOTAL}" ]; then
	# Say why. On the live run this released a session mid-goal and read as a
	# clean finish; nobody could tell the two apart afterwards.
	printf 'goal-continue: stop allowed, continuation budget spent for this message (%s no-progress stops, %s total).\n' \
		"${count}" "${total}" >&2
	printf '%s %s %s' "${count}" "${total}" "${new_offset}" >"${counter}" 2>/dev/null || true
	exit 0
fi
printf '%s %s %s' "$((count + 1))" "$((total + 1))" "${new_offset}" >"${counter}" 2>/dev/null || exit 0

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
	printf 'goal-continue: consent honoured, not asking again for: %s\n' \
		"${consent}" >&2
	consent_note="

OWNER CONSENT ALREADY GRANTED for: ${consent}
Do those yourself now. Stopping to ask again for something on that list is not a valid stop -- the owner already answered."
	# Whole tokens, split on commas. A substring test read `install deps` as
	# blanket consent -- it contains "all" -- and silently retired the entire
	# irreversible-act stop reason, delete and deploy included. So do "call",
	# "allow", "fallback" and "recall".
	consent_blanket=0
	consent_ifs="${IFS-}"
	IFS=','
	# Globbing off around the split: the token being tested for is `*`, and an
	# unquoted expansion of it would be replaced by the filenames in the
	# working directory before the comparison ever ran.
	set -f
	for consent_tok in ${consent}; do
		consent_tok="$(printf '%s' "${consent_tok}" |
			tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')"
		case "${consent_tok}" in
			all|'*') consent_blanket=1 ;;
		esac
	done
	set +f
	IFS="${consent_ifs}"
	if [ "${consent_blanket}" -eq 1 ]; then
		consent_bullet=""
	else
		consent_bullet="
  - you need consent for an irreversible act NOT on the granted list above"
	fi
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
