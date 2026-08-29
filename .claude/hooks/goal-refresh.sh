#!/usr/bin/env bash
# UserPromptSubmit hook: keep an active goal from going stale.
#
# The problem this exists for. A long session drifts: the objective set an hour
# and forty turns ago gets summarized away by compaction, or simply falls out of
# the model's attention, and the session starts idling or asking the owner
# things its own rules already answer. Setting a goal once does not keep it
# alive -- it has to be restated.
#
# So this hook restates it. On EVERY prompt, if a goal is active, the goal text
# is injected back into context via `additionalContext`. That is the whole
# anti-staleness mechanism: the objective can never be more than one turn old,
# and it survives compaction because it is re-added after compaction too.
#
# It is also the PRODUCER. The Stop hook goal-continue.sh reads a goal file, and
# nothing wrote one -- so it sat inert on every machine it shipped to. A
# consumer without a producer is not a feature. A prompt beginning `/goal ` or
# `GOAL:` writes the file here, which makes this hook the single write path.
#
# Contract:
#   stdin  : {session_id, prompt_id, cwd, prompt, hook_event_name, ...}
#   stdout : {"hookSpecificOutput":{"hookEventName":"UserPromptSubmit",
#             "additionalContext":"..."}}  -- or nothing
#   exit 0 : always. This runs on every prompt the owner types; it must never
#            be the reason one fails to send.
#
# Deliberately NOT done here:
#   - no fuzzy goal inference. Only an explicit prefix sets a goal. Guessing
#     that some sentence was an objective is how a hook starts enforcing a goal
#     the owner never set.
#   - no `/loop` handling. Loop scheduling depends on turns ENDING; pairing it
#     with a Stop-block fights the scheduler.

set -uo pipefail

command -v python3 >/dev/null 2>&1 || exit 0
payload="$(cat)"
[ -n "${payload}" ] || exit 0
[ "${CLAUDE_GOAL_OFF:-0}" = "1" ] && exit 0

# Line-delimited so a cwd containing a space survives. Learned the hard way:
# an earlier parser collapsed spaces to underscores and silently pointed the
# worktree lookup at a path that does not exist.
{
	read -r session_id
	IFS= read -r cwd
	IFS= read -r prompt
} < <(printf '%s' "${payload}" | python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)
except Exception:
    print("-"); print("-"); print("")
    raise SystemExit(0)
def g(k, dflt=""):
    v = d.get(k)
    if v is None:
        return dflt
    return str(v).replace("\r", " ").replace("\n", " ")
print(g("session_id", "-"))
print(g("cwd", "-"))
print(g("prompt"))
' 2>/dev/null)

[ "${session_id:--}" = "-" ] && exit 0
[ "${cwd:--}" = "-" ] && exit 0

# The SAME ordered lookup the Stop hook uses. When these two disagree, a goal
# is enforced from a path that is never refreshed -- a $HOME-scoped goal would
# block turns while going stale, which is the worst of both halves.
#
# For WRITING a new goal there is no ambiguity: an explicit override wins,
# otherwise the worktree. The producer never writes $HOME, because a
# machine-wide goal file would be inherited by every concurrent session in
# every other worktree.
goal_write="${CLAUDE_GOAL_FILE:-${cwd}/.claude/active-goal}"
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

# ── producer ───────────────────────────────────────────────────────────────
# `/goal <text>` or `GOAL: <text>` sets it; `/goal done` or `/goal clear`
# retires it. The SESSION header is what stops a goal outliving the session
# that set it and nagging the next one about finished work.
case "${prompt}" in
	'/goal done'*|'/goal clear'*|'GOAL: done'*)
		retire="${goal_file:-${goal_write}}"
		if [ -f "${retire}" ]; then
			printf 'DONE\n' > "${retire}.tmp" 2>/dev/null &&
				cat "${retire}" >> "${retire}.tmp" 2>/dev/null &&
				mv "${retire}.tmp" "${retire}" 2>/dev/null || true
		fi
		exit 0
		;;
	'/goal '*|'GOAL: '*)
		goal_text="${prompt#*[ :]}"
		goal_text="${goal_text# }"
		if [ -n "${goal_text}" ]; then
			mkdir -p "$(dirname "${goal_write}")" 2>/dev/null || exit 0
			{
				printf 'SESSION: %s\n' "${session_id}"
				printf '%s\n' "${goal_text}"
			} > "${goal_write}" 2>/dev/null || exit 0
			goal_file="${goal_write}"
		fi
		;;
esac

# ── refresher ──────────────────────────────────────────────────────────────
[ -f "${goal_file}" ] || exit 0
goal="$(cat "${goal_file}" 2>/dev/null)"
[ -n "${goal}" ] || exit 0

first_line="$(printf '%s\n' "${goal}" | head -1)"
case "${first_line}" in
	DONE*|done*) exit 0 ;;
esac

# A goal belongs to the session that set it. An unheaded file is the owner's
# own hand-written goal and is honoured as-is.
case "${first_line}" in
	SESSION:*)
		owner="${first_line#SESSION:}"
		owner="${owner# }"
		[ "${owner}" = "${session_id}" ] || exit 0
		goal="$(printf '%s\n' "${goal}" | tail -n +2)"
		;;
esac
[ -n "${goal}" ] || exit 0

printf '%s' "${goal}" | python3 -c '
import json, sys
goal = sys.stdin.read().strip()
if not goal:
    raise SystemExit(0)
note = (
    "ACTIVE GOAL (restated each turn so it cannot go stale):\n"
    + goal
    + "\n\nKeep working it. Take the next concrete action yourself. Do not ask "
      "the owner something the code, a local doc, a loaded skill, or a "
      "Deep-tier model dispatch can settle -- their rules already cover it. "
      "Ask only for consent on an irreversible act (push, merge, deploy, "
      "delete, data mutation), or when complete evidence would still leave a "
      "product-taste call."
)
print(json.dumps({"hookSpecificOutput": {
    "hookEventName": "UserPromptSubmit",
    "additionalContext": note,
}}))
' 2>/dev/null || exit 0

exit 0
