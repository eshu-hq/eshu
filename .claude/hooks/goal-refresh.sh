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
#   prompts: `/goal <text>` and `GOAL: <text>` set the goal; `/goal done` and
#            `/goal clear` retire it; `/goal consent <acts>` and
#            `/goal revoke-consent` edit only the CONSENT line, and are matched
#            ABOVE the generic arm so an objective beginning with the word
#            "consent" is not read as a grant.
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
# NOT a top-level guard. cwd is needed to WRITE a goal -- `goal_write`
# interpolates it -- and not to read one. Exiting here meant a $HOME goal was
# ENFORCED by the Stop hook and never refreshed by this one: blocking turns on
# an objective it never restated. The producer arm carries the guard instead,
# so the two hooks resolve a cwd-less payload identically.
have_cwd=1
[ "${cwd:--}" = "-" ] && have_cwd=0

# A RETIRED candidate must not shadow an active one. `/goal done` leaves a DONE
# tombstone at the per-session path, and an existence-only lookup then selects
# it ahead of a live checkout-shared goal forever -- the session goes
# unenforced and unrefreshed with nothing saying so. Reported by codex against
# the opened PR.
#
# Retired means: the first line that is neither blank nor a CONSENT: line is
# DONE, or is a SESSION: header whose next such line is DONE. Same rule both
# hooks apply to the file they finally select, applied one step earlier so the
# lookup can pass over it.
goal_file_retired() { # path
	local line seen_header=0 head_line
	[ -f "$1" ] || return 1
	while IFS= read -r line; do
		head_line="${line#"${line%%[![:space:]]*}"}"
		case "${head_line}" in
			[Cc][Oo][Nn][Ss][Ee][Nn][Tt]:*|'') continue ;;
		esac
		case "${head_line}" in
			DONE*|done*) return 0 ;;
			SESSION:*)
				if [ "${seen_header}" -eq 0 ]; then
					seen_header=1
					continue
				fi
				return 1
				;;
			*) return 1 ;;
		esac
	done <"$1"
	return 1
}

# The SAME ordered lookup the Stop hook uses. When these two disagree, a goal
# is enforced from a path that is never refreshed -- a $HOME-scoped goal would
# block turns while going stale, which is the worst of both halves.
#
# For WRITING a new goal there is no ambiguity: an explicit override wins,
# otherwise this session's own file in the worktree. The producer never writes
# $HOME, because a machine-wide goal file would be inherited by every
# concurrent session in every other worktree.
#
# The per-session file comes first, and is what `/goal` WRITES. Sessions are
# not per-checkout: three agents in one clone shared a single
# `.claude/active-goal`, so the last `/goal` won and the SESSION header then
# denied the losers any enforcement at all -- silently. Parallel sessions need
# parallel goals, and the checkout-shared file stays the owner's hand-written
# form, which the producer never touches.
#
# The session id is sanitized before it becomes part of a path.
sid_safe="$(printf '%s' "${session_id}" | tr -c 'A-Za-z0-9._-' '-')"
cwd_prefix="${cwd:--}"
goal_write="${CLAUDE_GOAL_FILE:-${cwd_prefix}/.claude/active-goal.${sid_safe}}"
goal_file=""
for candidate in \
	"${CLAUDE_GOAL_FILE:-}" \
	"${cwd_prefix}/.claude/active-goal.${sid_safe}" \
	"${cwd_prefix}/.claude/active-goal" \
	"${HOME}/.claude/active-goal"; do
	[ -n "${candidate}" ] || continue
	# With no cwd the two worktree candidates are paths built from a dash.
	case "${candidate}" in
		"-/.claude/"*) continue ;;
	esac
	if [ -f "${candidate}" ]; then
		# A retired candidate is passed over, so an active one further down the
		# list can be found -- but NOT an explicit CLAUDE_GOAL_FILE. The owner
		# naming a file means that file; falling through from it to a different
		# goal would be the override silently selecting something else. A
		# retired override is honoured as retired, which the DONE checks below
		# then act on.
		if [ "${candidate}" != "${CLAUDE_GOAL_FILE:-}" ] &&
			goal_file_retired "${candidate}"; then
			continue
		fi
		goal_file="${candidate}"
		break
	fi
done

# A resolved goal file may belong to somebody else: a session with no goal of
# its own still RESOLVES the checkout-shared file, and retiring or amending it
# from here would edit another agent's objective. Writes below use this.
#
# Two holes a review found by execution, both fixed here. The default arm used
# to return OWNED for a file with no `SESSION:` header -- which is precisely
# the shared, hand-written case the guard exists to protect, so any session in
# the checkout could amend or retire the owner's own goal. And it read line 1
# only, which stopped being the header line the moment CONSENT lines could sit
# above it: a `CONSENT:` first line hid the check entirely, and the write went
# through AND reordered somebody else's file.
#
# So: owned means this session's own write target, or a `SESSION:` header
# naming this session, found past any CONSENT lines. An unheaded file is the
# owner's -- read it, enforce it, never write it.
owns_goal_file() { # path
	local line header=""
	[ -f "$1" ] || return 1
	case "$1" in
		"${goal_write}") return 0 ;;
	esac
	while IFS= read -r line; do
		case "${line#"${line%%[![:space:]]*}"}" in
			[Cc][Oo][Nn][Ss][Ee][Nn][Tt]:*) continue ;;
		esac
		header="${line}"
		break
	done <"$1"
	case "${header}" in
		SESSION:*)
			header="${header#SESSION:}"
			[ "${header# }" = "${session_id}" ]
			;;
		*) return 1 ;;
	esac
}

# May this session WRITE the file it resolved? Both write arms ask here, rather
# than one of them carrying a guard the other lacks -- which is exactly how the
# consent arm ended up refusing $HOME while the retire arm silently prepended
# DONE to it, retiring the machine-wide goal for every session on the machine.
# Prints the reason it refuses; the caller decides whether to exit.
writable_goal_target() { # path action
	# The remedy has to fit the ACTION, not just the condition. Both messages
	# were consent-shaped, so `/goal done` against a machine-wide goal was told
	# to "set a goal here first" -- advice that creates a new goal instead of
	# retiring the one the owner meant.
	local remedy_missing="Set one with /goal <text>." remedy_elsewhere="set a goal here first"
	if [ "$2" = retirement ]; then
		remedy_missing="There is nothing to retire."
		remedy_elsewhere="retire it from the session that set it, or edit the file yourself"
	fi
	if [ -z "$1" ]; then
		printf 'goal-refresh: no goal file for this session, %s not recorded. %s\n' \
			"$2" "${remedy_missing}" >&2
		return 1
	fi
	# Never the machine-wide file: a goal written there is inherited by every
	# concurrent session in every other worktree, so retiring or amending it
	# reaches all of them. An explicit CLAUDE_GOAL_FILE is the owner naming a
	# target on purpose.
	if [ -z "${CLAUDE_GOAL_FILE:-}" ] && [ "$1" = "${HOME}/.claude/active-goal" ]; then
		printf 'goal-refresh: refusing to write %s into the machine-wide goal file %s from this worktree; %s.\n' \
			"$2" "$1" "${remedy_elsewhere}" >&2
		return 1
	fi
	if [ ! -f "$1" ]; then
		printf 'goal-refresh: no goal file for this session, %s not recorded. %s\n' \
			"$2" "${remedy_missing}" >&2
		return 1
	fi
	if ! owns_goal_file "$1"; then
		printf 'goal-refresh: %s belongs to another session or to the owner, %s not recorded.\n' \
			"$1" "$2" >&2
		return 1
	fi
	return 0
}

# ── producer ───────────────────────────────────────────────────────────────
# `/goal <text>` or `GOAL: <text>` sets it; `/goal done` or `/goal clear`
# retires it. The SESSION header is what stops a goal outliving the session
# that set it and nagging the next one about finished work.
case "${prompt}" in
	'/goal done'*|'/goal clear'*|'GOAL: done'*)
		retire="${goal_file:-${goal_write}}"
		if writable_goal_target "${retire}" retirement; then
			printf 'DONE\n' > "${retire}.tmp" 2>/dev/null &&
				cat "${retire}" >> "${retire}.tmp" 2>/dev/null &&
				mv "${retire}.tmp" "${retire}" 2>/dev/null || true
		fi
		exit 0
		;;
	'/goal consent'|'/goal consent '*|'GOAL: consent'|'GOAL: consent '*|\
	'/goal revoke-consent'|'/goal revoke-consent '*|\
	'GOAL: revoke-consent'|'GOAL: revoke-consent '*)
		# Consent the owner typed in chat dies with that turn. Written into the
		# goal file it survives compaction, the next Stop, and the next session
		# that adopts the file. This case has to sit ABOVE the generic `/goal
		# <text>` producer: otherwise "/goal consent push" is read as a new
		# objective and silently replaces the actual one with the word
		# "consent" -- granting nothing and losing the goal in the same move.
		# The word boundary above matters: a bare `'/goal consent'*` prefix
		# swallowed "/goal consented users need a migration path", dropped the
		# objective the owner was actually setting, and wrote the remainder as
		# a grant.
		#
		# Every rejection below says so on stderr. A consent that silently goes
		# nowhere is the exact loop this feature exists to close -- the owner
		# types it, nothing records it, the next Stop asks again.
		target="${goal_file:-}"
		writable_goal_target "${target}" consent || exit 0
		case "${prompt}" in
			*revoke-consent*) acts="" ;;
			*)
				acts="${prompt#*consent}"
				acts="${acts# }"
				;;
		esac
		ACTS="${acts}" TARGET="${target}" python3 -c '
import os, sys
target = os.environ["TARGET"]
acts = os.environ["ACTS"].strip()
try:
    lines = open(target).read().splitlines()
except Exception:
    raise SystemExit(0)
kept, meta_zone, saw_header_meta = [], True, False
for l in lines:
    if meta_zone:
        if l.lstrip().lower().startswith("consent:"):
            continue
        # lstrip, like every sibling parser in these two hooks. Testing the RAW
        # line meant an indented header ended the metadata zone here while the
        # others still treated it as one -- the leading-whitespace disagreement
        # this suite already pins as a defect for the Stop hook.
        if not l.lstrip().startswith("SESSION:") or saw_header_meta:
            meta_zone = False
        else:
            saw_header_meta = True
    kept.append(l)
if acts:
    # Under the SESSION header, so the header stays first and the DONE /
    # BLOCKED escapes the Stop hook reads off the first line are unaffected.
    at = 1 if kept and kept[0].lstrip().startswith("SESSION:") else 0
    kept.insert(at, "CONSENT: " + acts)
try:
    with open(target + ".tmp", "w") as fh:
        fh.write("\n".join(kept) + "\n")
    os.replace(target + ".tmp", target)
except Exception:
    pass
' 2>/dev/null || true
		exit 0
		;;
	'/goal '*|'GOAL: '*)
		# Writing is what needs a cwd; without one there is no worktree to
		# write the goal into, and CLAUDE_GOAL_FILE is the way to name a
		# target anyway.
		[ "${have_cwd}" = "1" ] || [ -n "${CLAUDE_GOAL_FILE:-}" ] || exit 0
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

# The ORDER here is the contract, and it must match goal-continue.sh exactly:
# DONE, then strip CONSENT, then SESSION, then DONE again beneath the header.
#
# It used to run DONE and SESSION on the RAW first line and strip CONSENT last,
# inside the python below. Two PR reviewers, reading the diff cold, found what
# that costs. A `CONSENT:` line above the header made the ownership check see a
# consent line instead of a header, treat the file as unheaded, and inject
# ANOTHER session's objective together with a grant that was never made to this
# session. And a `SESSION:` header above a `DONE` meant the retirement was
# never seen at all, so `/goal done` did not retire a goal for the refresher —
# it went on restating a finished objective indefinitely.
#
# Both fall out of one asymmetry: the Stop hook was hardened for this file
# format and its sibling, parsing the same format, was never checked for the
# same shape. scripts/test-goal-refresh-hook-parity-cases.sh now runs every
# layout against BOTH hooks and asserts they decide alike.
first_line="$(printf '%s\n' "${goal}" | head -1)"
case "${first_line}" in
	DONE*|done*) exit 0 ;;
esac

# CONSENT lines are metadata, not the objective and not a header. They are held
# aside for the restatement below and must not be able to stand in for either.
#
# And metadata is a LEADING BLOCK. Matching the pattern anywhere deleted body
# lines that merely discuss the format -- the same defect as the `SESSION:`
# filter, in the sibling field. The first ordinary line ends the metadata.
goal_body=""
goal_meta=1
goal_seen_header=0
while IFS= read -r goal_line; do
	if [ "${goal_meta}" -eq 1 ]; then
		case "${goal_line#"${goal_line%%[![:space:]]*}"}" in
			[Cc][Oo][Nn][Ss][Ee][Nn][Tt]:*) continue ;;
			SESSION:*)
				# First one only; see the sibling comment in goal-continue.sh.
				[ "${goal_seen_header}" -eq 1 ] && goal_meta=0
				goal_seen_header=1
				;;
			*) goal_meta=0 ;;
		esac
	fi
	goal_body="${goal_body}${goal_line}
"
done < <(printf '%s\n' "${goal}")
goal_body="${goal_body%$'\n'}"
[ -n "${goal_body}" ] || exit 0

# A goal belongs to the session that set it. An unheaded file is the owner's
# own hand-written goal and is honoured as-is.
header_line="$(printf '%s\n' "${goal_body}" | head -1)"
case "${header_line}" in
	SESSION:*)
		owner="${header_line#SESSION:}"
		owner="${owner# }"
		[ "${owner}" = "${session_id}" ] || exit 0
		# Drop the header from the text the agent sees, from BOTH views: the
		# body drives the DONE re-check, the full goal still carries the
		# CONSENT lines the restatement needs.
		#
		# The full view must drop THE HEADER -- the first SESSION: line -- and
		# nothing else. A filter (`rg -v '^[[:space:]]*SESSION:'`) deleted every
		# such line anywhere in the body, so a goal that discusses the SESSION
		# header, which goals in this repo routinely do, was silently truncated
		# before the agent ever saw it. Not a leak: data loss, with the agent
		# working from a shortened objective and nothing saying so.
		goal_body="$(printf '%s\n' "${goal_body}" | tail -n +2)"
		goal="$(printf '%s\n' "${goal}" | python3 -c '
import sys
dropped = False
for line in sys.stdin.read().splitlines():
    if not dropped and line.lstrip().startswith("SESSION:"):
        dropped = True
        continue
    print(line)
' 2>/dev/null)"
		;;
esac
[ -n "${goal_body}" ] || exit 0

# DONE beneath the header, which the raw first-line check above cannot see.
case "$(printf '%s\n' "${goal_body}" | head -1)" in
	DONE*|done*) exit 0 ;;
esac
[ -n "${goal}" ] || exit 0

printf '%s' "${goal}" | CONSENT_ENV="${CLAUDE_GOAL_CONSENT:-}" python3 -c '
import json, os, sys
lines = sys.stdin.read().splitlines()
# CONSENT: <acts> is a permission the owner already gave. It is restated
# separately from the objective, because an act the owner has consented to is
# no longer something to stop and ask about -- and the ask-only-for-consent
# sentence below reads as an invitation to do exactly that.
# Leading metadata only, for the same reason the shell view above stops at the
# first ordinary line: a body line discussing the consent format is objective
# text, not a grant, and reading it as one both truncates the goal and tells
# the agent the owner permitted something they never mentioned.
meta = []
seen_header = False
for line in lines:
    stripped_line = line.lstrip()
    low = stripped_line.lower()
    if low.startswith("consent:"):
        meta.append(stripped_line)
        continue
    if stripped_line.startswith("SESSION:") and not seen_header:
        seen_header = True
        continue
    break
acts = [m.split(":", 1)[1].strip() for m in meta if m.split(":", 1)[1].strip()]
env_acts = os.environ.get("CONSENT_ENV", "").strip()
if env_acts:
    acts.append(env_acts)
kept_lines, in_meta, saw_header = [], True, False
for line in lines:
    if in_meta:
        bare = line.lstrip()
        if bare.lower().startswith("consent:"):
            continue
        if bare.startswith("SESSION:") and not saw_header:
            saw_header = True
        else:
            in_meta = False
    kept_lines.append(line)
goal = "\n".join(kept_lines).strip()
if not goal:
    raise SystemExit(0)
granted = ", ".join(acts)
# Blanket is a whole token, the same test the Stop hook applies -- a substring
# match read "install deps" as blanket consent there, and the two halves have
# to agree about what a grant means or the same file says two things.
blanket = any(tok.strip().lower() in ("all", "*")
              for act in acts for tok in act.split(","))
if granted and blanket:
    consent_note = (
        "\n\nOWNER CONSENT ALREADY GRANTED, blanket, for: " + granted
        + ". Carry out the irreversible acts this goal needs; do not stop to "
          "ask for them. Ask only when complete evidence would still leave a "
          "product-taste call."
    )
elif granted:
    consent_note = (
        "\n\nOWNER CONSENT ALREADY GRANTED for: " + granted
        + ". Carry those out yourself when the work reaches them; do not stop "
          "to ask again for anything on that list. Ask only for an "
          "irreversible act NOT on it, or when complete evidence would still "
          "leave a product-taste call."
    )
else:
    consent_note = (
        " Ask only for consent on an irreversible act (push, merge, deploy, "
        "delete, data mutation), or when complete evidence would still leave a "
        "product-taste call."
    )
note = (
    "ACTIVE GOAL (restated each turn so it cannot go stale):\n"
    + goal
    + "\n\nKeep working it. Take the next concrete action yourself. Do not ask "
      "the owner something the code, a local doc, a loaded skill, or a "
      "Deep-tier model dispatch can settle -- their rules already cover it."
    + consent_note
)
print(json.dumps({"hookSpecificOutput": {
    "hookEventName": "UserPromptSubmit",
    "additionalContext": note,
}}))
' 2>/dev/null || exit 0

exit 0
