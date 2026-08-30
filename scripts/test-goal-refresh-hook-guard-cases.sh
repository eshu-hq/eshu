#!/usr/bin/env bash
# Sourced by scripts/test-goal-refresh-hook.sh -- not run on its own.
#
# The second companion of the refresh mirror. The first one crossed the repo's
# 500-line cap at 504 lines, in a branch whose commit 8 says "every file this
# branch splits or creates is under it" -- so the file created by a split to
# honour the cap went over it, one commit after that sentence was rewritten to
# be accurate. Split by topic rather than by line count: consent cases stay in
# the first companion, the write guards and concurrency cases are here.
#
# A trigger path for the agent-canon gate, and its sentinel is the last line,
# asserted by test-agent-hooks.sh along with every sibling. Append ABOVE it.

# ── concurrent sessions in one checkout ────────────────────────────────────
#
# Sessions are not per-checkout. Three agents in one clone shared a single
# `.claude/active-goal`: the last `/goal` to run overwrote the others, and the
# SESSION header then denied the losers any enforcement at all -- silently.
# The producer writes `.claude/active-goal.<session_id>`, so parallel sessions
# hold parallel goals.

pw="${work}/parallel"
mkdir -p "${pw}/.claude"
sid_a="${sid}-a"
sid_b="${sid}-b"

submit "${sid_a}" '/goal lane A: land the parser train' "${pw}" >/dev/null
submit "${sid_b}" '/goal lane B: chase the flaky gate' "${pw}" >/dev/null

if [[ -f "${pw}/.claude/active-goal.${sid_a}" && -f "${pw}/.claude/active-goal.${sid_b}" ]]; then
	ok "two sessions in one checkout get two goal files"
else
	no "two sessions in one checkout get two goal files"
fi

# The shared file is the owner's hand-written form. The producer must not
# create or touch it, or the next hand-written goal is somebody's leftovers.
if [[ -f "${pw}/.claude/active-goal" ]]; then
	no "the producer leaves the checkout-shared goal file alone"
else
	ok "the producer leaves the checkout-shared goal file alone"
fi

inj_a="$(injected "$(submit "${sid_a}" 'carry on' "${pw}")")"
inj_b="$(injected "$(submit "${sid_b}" 'carry on' "${pw}")")"
if printf '%s' "${inj_a}" | rg -q 'lane A' && ! printf '%s' "${inj_a}" | rg -q 'lane B'; then
	ok "session A is refreshed with only its own goal"
else
	no "session A is refreshed with only its own goal"
fi
if printf '%s' "${inj_b}" | rg -q 'lane B' && ! printf '%s' "${inj_b}" | rg -q 'lane A'; then
	ok "session B is refreshed with only its own goal"
else
	no "session B is refreshed with only its own goal"
fi

# Retirement is per session too. One agent finishing must not release the other.
submit "${sid_a}" '/goal done' "${pw}" >/dev/null
if [[ -z "$(submit "${sid_a}" 'anything' "${pw}")" ]]; then
	ok "/goal done retires only the caller's goal"
else
	no "/goal done retires only the caller's goal"
fi
if printf '%s' "$(injected "$(submit "${sid_b}" 'anything' "${pw}")")" | rg -q 'lane B'; then
	ok "the other session's goal survives that retirement"
else
	no "the other session's goal survives that retirement"
fi

# END TO END: the file the producer wrote for THIS session is the file the Stop
# hook enforces for it -- while a second session's goal sits beside it.
par_stop="$(SID="${sid_b}" CWD="${pw}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "par-e2e",
                  "stop_hook_active": False, "cwd": os.environ["CWD"],
                  "hook_event_name": "Stop"}))
' | bash "${CONTINUE}" 2>/dev/null)"
if printf '%s' "${par_stop}" | rg -q 'lane B' && ! printf '%s' "${par_stop}" | rg -q 'lane A'; then
	ok "END TO END: the Stop hook enforces each session's own goal"
else
	no "END TO END: the Stop hook enforces each session's own goal"
fi

# ── what the reviewers proved wrong ────────────────────────────────────────

# The consent arm matched on a bare prefix, so an ordinary objective beginning
# with the word "consented" was swallowed by it: the goal update was dropped
# and a garbage CONSENT value written in its place.
bw="${work}/boundary"
mkdir -p "${bw}/.claude"
submit "${sid}" '/goal ship the release' "${bw}" >/dev/null
submit "${sid}" '/goal consented users need a migration path' "${bw}" >/dev/null
bfile="${bw}/.claude/active-goal.${sid}"
if rg -q '^CONSENT: ed users' "${bfile}" 2>/dev/null; then
	no "an objective starting with 'consented' is not read as a grant"
else
	ok "an objective starting with 'consented' is not read as a grant"
fi
if rg -q 'consented users need a migration path' "${bfile}" 2>/dev/null; then
	ok "that objective is stored as the goal it is"
else
	no "that objective is stored as the goal it is"
fi

# `/goal consent` resolved the READ path and wrote to it. With no worktree goal
# file that path is $HOME/.claude/active-goal -- a machine-wide file every
# concurrent session in every other worktree falls through to. The producer's
# own comment forbids writing it.
hw="${work}/homeless"
fake_h="${work}/fakehome2"
mkdir -p "${hw}/.claude" "${fake_h}/.claude"
printf 'A hand-written machine-wide objective.\n' >"${fake_h}/.claude/active-goal"
SID="${sid}" PROMPT='/goal consent push, merge, deploy' CWD="${hw}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "homewrite",
                  "cwd": os.environ["CWD"], "prompt": os.environ["PROMPT"],
                  "hook_event_name": "UserPromptSubmit"}))
' | HOME="${fake_h}" bash "${REFRESH}" >/dev/null 2>&1
if rg -q '^CONSENT:' "${fake_h}/.claude/active-goal" 2>/dev/null; then
	no "the consent producer never writes the machine-wide goal file"
else
	ok "the consent producer never writes the machine-wide goal file"
fi

# A consent that goes nowhere is the exact loop this whole change exists to
# close: the owner types it, nothing records it, the next Stop asks again.
nw="${work}/nogoal"
mkdir -p "${nw}/.claude"
noop_err="$(SID="${sid}" PROMPT='/goal consent push' CWD="${nw}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "noopc",
                  "cwd": os.environ["CWD"], "prompt": os.environ["PROMPT"],
                  "hook_event_name": "UserPromptSubmit"}))
' | HOME="${work}/no-such-home" bash "${REFRESH}" 2>&1 >/dev/null)"
if printf '%s' "${noop_err}" | rg -qi 'consent'; then
	ok "a consent with no goal file to write says so on stderr"
else
	no "a consent with no goal file to write says so on stderr (got: ${noop_err})"
fi

# The ownership guard had no case that executed its false branch: every
# existing case gave each session its own file, so deleting the guard left both
# mirrors green. These two run a session AT a file it does not own.
fw="${work}/foreign"
mkdir -p "${fw}/.claude"
printf 'SESSION: someone-else\nTheir objective.\n' >"${fw}/.claude/active-goal"
submit "${sid}" '/goal done' "${fw}" >/dev/null
if [[ "$(head -1 "${fw}/.claude/active-goal")" == "SESSION: someone-else" ]]; then
	ok "/goal done does not retire a file this session does not own"
else
	no "/goal done does not retire a file this session does not own"
fi
submit "${sid}" '/goal consent force-push, delete branches' "${fw}" >/dev/null
if rg -q '^CONSENT:' "${fw}/.claude/active-goal" 2>/dev/null; then
	no "/goal consent does not amend a file this session does not own"
else
	ok "/goal consent does not amend a file this session does not own"
fi

# ── round-three findings ───────────────────────────────────────────────────

# The ownership guard treated an UNHEADED file as owned, and an unheaded file
# is exactly the shared hand-written case it exists to protect. Any session in
# the checkout could amend or retire the owner's own goal.
uw="${work}/unheaded"
mkdir -p "${uw}/.claude"
printf 'Owner hand-written objective.\n' >"${uw}/.claude/active-goal"
submit "sess-stranger" '/goal consent deploy to prod' "${uw}" >/dev/null
if rg -q '^CONSENT:' "${uw}/.claude/active-goal" 2>/dev/null; then
	no "an unheaded shared goal is not amended by a passing session"
else
	ok "an unheaded shared goal is not amended by a passing session"
fi
submit "sess-stranger" '/goal done' "${uw}" >/dev/null
if [[ "$(head -1 "${uw}/.claude/active-goal")" == DONE* ]]; then
	no "an unheaded shared goal is not retired by a passing session"
else
	ok "an unheaded shared goal is not retired by a passing session"
fi

# The guard read line 1 only, and the consent feature made line 1 something a
# CONSENT line can occupy. A CONSENT line above the header hid it: the write
# went through AND normalized the file to SESSION-first, erasing the trace.
cw2="${work}/consent-above"
mkdir -p "${cw2}/.claude"
printf 'CONSENT: read-only\nSESSION: sess-owner\nTheir private objective.\n' \
	>"${cw2}/.claude/active-goal"
submit "sess-intruder" '/goal consent force-push, delete branches' "${cw2}" >/dev/null
if rg -q 'force-push' "${cw2}/.claude/active-goal" 2>/dev/null; then
	no "a CONSENT line above the header does not defeat the ownership guard"
else
	ok "a CONSENT line above the header does not defeat the ownership guard"
fi
if [[ "$(head -1 "${cw2}/.claude/active-goal")" == "CONSENT: read-only" ]]; then
	ok "the intruder's write does not reorder somebody else's file"
else
	no "the intruder's write does not reorder somebody else's file"
fi

# This hook runs on EVERY prompt the owner types, so a stray diagnostic is
# noise on every turn. The Stop mirror pins this for its hook; the higher
# traffic one had no such assertion, and three stderr lines were just added.
qw="${work}/quiet"
mkdir -p "${qw}/.claude"
submit "${sid}" '/goal keep quiet' "${qw}" >/dev/null
quiet_err="$(SID="${sid}" PROMPT='an ordinary prompt' CWD="${qw}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "quiet",
                  "cwd": os.environ["CWD"], "prompt": os.environ["PROMPT"],
                  "hook_event_name": "UserPromptSubmit"}))
' | bash "${REFRESH}" 2>&1 >/dev/null)"
if [[ -z "${quiet_err}" ]]; then
	ok "the refresher writes nothing to stderr on the ordinary path"
else
	no "the refresher writes nothing to stderr on the ordinary path (got: ${quiet_err})"
fi

# The launcher-side grant had no case at all: deleting the branch that reads it
# left the whole suite green, so it could stop reaching the restatement without
# anything noticing.
ew="${work}/envconsent"
mkdir -p "${ew}/.claude"
submit "${sid}" '/goal ship it' "${ew}" >/dev/null
env_inj="$(injected "$(SID="${sid}" PROMPT='carry on' CWD="${ew}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "envc",
                  "cwd": os.environ["CWD"], "prompt": os.environ["PROMPT"],
                  "hook_event_name": "UserPromptSubmit"}))
' | CLAUDE_GOAL_CONSENT='pr-open' bash "${REFRESH}" 2>/dev/null)")"
# `pr-open` appears in no fallback text, and `already granted` is the marker
# only the consent branch emits. The first version of this case grepped for
# `push` -- which the NO-consent sentence also contains ("Ask only for consent
# on an irreversible act (push, merge, deploy, ...)") -- so it passed with the
# branch deleted: the vacuous-test defect it was written to prevent, inside it.
if printf '%s' "${env_inj}" | rg -qi 'already granted' &&
	printf '%s' "${env_inj}" | rg -q 'pr-open'; then
	ok "CLAUDE_GOAL_CONSENT reaches the per-turn restatement"
else
	no "CLAUDE_GOAL_CONSENT reaches the per-turn restatement"
fi

# The consent arm got a $HOME guard and the retire arm did not, so `/goal done`
# from a worktree with no goal of its own still retired the MACHINE-WIDE goal
# for every session on the machine -- silently. The reasoning the consent guard
# was written with applies identically to a retirement.
hw2="${work}/homeretire"
fh2="${work}/fakehome3"
mkdir -p "${hw2}/.claude" "${fh2}/.claude"
printf 'SESSION: %s\nMachine-wide objective.\n' "${sid}" >"${fh2}/.claude/active-goal"
retire_err="$(SID="${sid}" PROMPT='/goal done' CWD="${hw2}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "homeretire",
                  "cwd": os.environ["CWD"], "prompt": os.environ["PROMPT"],
                  "hook_event_name": "UserPromptSubmit"}))
' | HOME="${fh2}" bash "${REFRESH}" 2>&1 >/dev/null)"
if [[ "$(head -1 "${fh2}/.claude/active-goal")" == DONE* ]]; then
	no "/goal done does not retire the machine-wide goal file"
else
	ok "/goal done does not retire the machine-wide goal file"
fi
if printf '%s' "${retire_err}" | rg -qi 'machine-wide'; then
	ok "the refused retirement says so on stderr"
else
	no "the refused retirement says so on stderr (got: ${retire_err})"
fi

# The two hooks claim the SAME ordered lookup, and that was true of the
# candidate list but not of the guard above it: goal-refresh exited early when
# the payload carried no cwd, while the Stop hook went on to resolve and
# enforce the $HOME goal. Enforced but never refreshed -- blocking turns on an
# objective it never restates, the worst of both halves. cwd is needed to WRITE
# a goal, not to read one, so the guard belongs on the producer arm.
nc_home="${work}/nocwdhome"
mkdir -p "${nc_home}/.claude"
printf 'SESSION: %s\nHome-scoped objective with no cwd.\n' "${sid}" \
	>"${nc_home}/.claude/active-goal"
nocwd_out="$(SID="${sid}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "nocwd",
                  "prompt": "carry on", "hook_event_name": "UserPromptSubmit"}))
' | HOME="${nc_home}" bash "${REFRESH}" 2>/dev/null)"
if printf '%s' "$(injected "${nocwd_out}")" | rg -q 'Home-scoped objective with no cwd'; then
	ok "a payload with no cwd still refreshes the \$HOME goal"
else
	no "a payload with no cwd still refreshes the \$HOME goal"
fi

# And the Stop hook must agree on that same payload -- the parity this PR
# claims, now true without exception.
nocwd_stop="$(SID="${sid}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "nocwdstop",
                  "stop_hook_active": False, "hook_event_name": "Stop"}))
' | HOME="${nc_home}" bash "${CONTINUE}" 2>/dev/null)"
if printf '%s' "${nocwd_stop}" | rg -q '"decision"[[:space:]]*:[[:space:]]*"block"'; then
	ok "PARITY: both hooks act on the same cwd-less payload"
else
	no "PARITY: both hooks act on the same cwd-less payload"
fi

# Writing still needs a cwd: `goal_write` interpolates it, so a producer prompt
# with no cwd must refuse rather than write to a path built from a dash.
nocwd_write="$(SID="${sid}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "nocwdwrite",
                  "prompt": "/goal set something", "hook_event_name": "UserPromptSubmit"}))
' | HOME="${nc_home}" bash "${REFRESH}" 2>/dev/null)"
if [[ -e "-/.claude" ]] || [[ -e "./-" ]]; then
	no "a cwd-less /goal writes no goal file"
else
	ok "a cwd-less /goal writes no goal file"
fi
if printf '%s' "$(injected "${nocwd_write}")" | rg -q 'set something'; then
	no "a cwd-less /goal does not become the goal"
else
	ok "a cwd-less /goal does not become the goal"
fi


# The two write arms disagreed about the SAME situation: with no goal file
# anywhere, the consent arm said "no goal file for this session" and the retire
# arm said the file "belongs to another session or to the owner" -- of a path
# that belongs to neither, being this session's own write target, simply absent.
# The `-z` branch is unreachable from the retire arm, which passes a defaulted
# path, so a nonexistence condition was reported as an ownership refusal: the
# divergence the shared helper was extracted to end, one level further down.
mw="${work}/missing"
mkdir -p "${mw}/.claude"
for action in done 'consent push'; do
	miss_err="$(SID="${sid}" PROMPT="/goal ${action}" CWD="${mw}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "missing",
                  "cwd": os.environ["CWD"], "prompt": os.environ["PROMPT"],
                  "hook_event_name": "UserPromptSubmit"}))
' | HOME="${work}/no-home-at-all" bash "${REFRESH}" 2>&1 >/dev/null)"
	if printf '%s' "${miss_err}" | rg -qi 'no goal file'; then
		ok "/goal ${action} with no goal file reports nonexistence, not ownership"
	else
		no "/goal ${action} with no goal file reports nonexistence, not ownership (got: ${miss_err})"
	fi
done

# A refusal that names the right condition can still send the owner the wrong
# way. Both remedies were consent-shaped: `/goal done` against a machine-wide
# goal was told to "set a goal here first", and following that advice creates a
# NEW worktree goal instead of retiring the one they meant.
rw="${work}/remedy"
rh="${work}/remedyhome"
mkdir -p "${rw}/.claude" "${rh}/.claude"
printf 'SESSION: %s\nMachine-wide objective.\n' "${sid}" >"${rh}/.claude/active-goal"
remedy_err="$(SID="${sid}" PROMPT='/goal done' CWD="${rw}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "remedy",
                  "cwd": os.environ["CWD"], "prompt": os.environ["PROMPT"],
                  "hook_event_name": "UserPromptSubmit"}))
' | HOME="${rh}" bash "${REFRESH}" 2>&1 >/dev/null)"
if printf '%s' "${remedy_err}" | rg -qi 'set a goal here first|set one with'; then
	no "a refused retirement is not told to set a goal"
else
	ok "a refused retirement is not told to set a goal"
fi
missing_retire="$(SID="${sid}" PROMPT='/goal done' CWD="${work}/no-goal-dir" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "remedy2",
                  "cwd": os.environ["CWD"], "prompt": os.environ["PROMPT"],
                  "hook_event_name": "UserPromptSubmit"}))
' | HOME="${work}/no-home-either" bash "${REFRESH}" 2>&1 >/dev/null)"
if printf '%s' "${missing_retire}" | rg -qi 'nothing to retire'; then
	ok "retiring a goal that does not exist says there is nothing to retire"
else
	no "retiring a goal that does not exist says there is nothing to retire (got: ${missing_retire})"
fi
# The consent arm keeps the remedy that fits IT.
missing_consent="$(SID="${sid}" PROMPT='/goal consent push' CWD="${work}/no-goal-dir" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "remedy3",
                  "cwd": os.environ["CWD"], "prompt": os.environ["PROMPT"],
                  "hook_event_name": "UserPromptSubmit"}))
' | HOME="${work}/no-home-either" bash "${REFRESH}" 2>&1 >/dev/null)"
if printf '%s' "${missing_consent}" | rg -qi 'set one with'; then
	ok "a consent with nothing to attach to is still told to set a goal"
else
	no "a consent with nothing to attach to is still told to set a goal"
fi

# The LAST line of this file, and it must stay last: the parent asserts it, so
# it means "this companion ran to the end". Set at the top it only meant "began
# loading", and a truncated file still reported a clean pass. Then the move was
# undone by appending cases BELOW it in the same commit, which put the residual
# straight back -- so there is now a case asserting this assignment is the final
# non-blank line. Append ABOVE it.
# The two hooks have to agree about what a BLANKET grant means. The Stop hook
# retires the consent bullet outright for `all`; the refresher went on telling
# the agent to ask for anything "NOT on it", which under a blanket grant is
# nothing at all -- the same file saying two things to the same agent.
bl="${work}/blanket"
mkdir -p "${bl}/.claude"
submit "${sid}" '/goal ship the thing' "${bl}" >/dev/null
submit "${sid}" '/goal consent all' "${bl}" >/dev/null
bl_inj="$(injected "$(submit "${sid}" 'carry on' "${bl}")")"
if printf '%s' "${bl_inj}" | rg -q 'NOT on it'; then
	no "a blanket grant is not described as a list to check against"
else
	ok "a blanket grant is not described as a list to check against"
fi
if printf '%s' "${bl_inj}" | rg -qi 'blanket'; then
	ok "a blanket grant says so"
else
	no "a blanket grant says so"
fi
# A named grant keeps the list wording, so the distinction cannot be lost by
# making both branches generic.
nm="${work}/named"
mkdir -p "${nm}/.claude"
submit "${sid}" '/goal ship the other thing' "${nm}" >/dev/null
submit "${sid}" '/goal consent push' "${nm}" >/dev/null
nm_inj="$(injected "$(submit "${sid}" 'carry on' "${nm}")")"
if printf '%s' "${nm_inj}" | rg -q 'NOT on it'; then
	ok "a named grant still says to ask for acts not on the list"
else
	no "a named grant still says to ask for acts not on the list"
fi

# LAST line on purpose -- see the sibling companions.
goal_refresh_guard_cases_loaded=1
