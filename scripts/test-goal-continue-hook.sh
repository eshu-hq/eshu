#!/usr/bin/env bash
# Behavioural mirror for .claude/hooks/goal-continue.sh (the Stop hook).
#
# This hook can REFUSE to end a session, so its failure mode is the most
# annoying one available: a wedged turn the owner has to break out of. Every
# fail-open path below is therefore load-bearing -- no goal file, an empty one,
# malformed JSON, empty stdin, or a missing python3 must all allow the stop.
#
# The suite is split in two halves:
#   1. core        -- the nudge itself, the budget, and every fail-open path.
#   2. blocked     -- the BLOCKED: escape and its liveness witness.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOOK="${repo_root}/.claude/hooks/goal-continue.sh"

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
mkdir -p "${work}/.claude"
goal="${work}/goal"
passed=0
failed=0
sid="goalhook-$$"

payload() { # prompt_id
	printf '{"session_id":"%s","prompt_id":"%s","stop_hook_active":false,"cwd":"%s","hook_event_name":"Stop"}' \
		"${sid}" "$1" "${work}"
}

run() { printf '%s' "$1" | CLAUDE_GOAL_FILE="${goal}" bash "${HOOK}" 2>/dev/null; }

ok() { printf 'ok - %s\n' "$1"; passed=$((passed + 1)); }
no() { printf 'not ok - %s\n' "$1"; failed=$((failed + 1)); }

check() { # label expect(block|allow) output
	local label="$1" expect="$2" out="$3" got
	if printf '%s' "${out}" | rg '"decision"[[:space:]]*:[[:space:]]*"block"' >/dev/null; then
		got=block
	else
		got=allow
	fi
	if [[ "${got}" == "${expect}" ]]; then
		ok "${label} (${got})"
	else
		no "${label} (got ${got}, want ${expect})"
	fi
}

[[ -f "${HOOK}" ]] || { printf 'not ok - hook missing at %s\n' "${HOOK}"; exit 1; }
[[ -x "${HOOK}" ]] || no "hook is executable"
bash -n "${HOOK}" || no "hook parses"

# ── 1. core ────────────────────────────────────────────────────────────────

rm -f "${goal}"
check "no goal file allows the stop" allow "$(run "$(payload p1)")"

: >"${goal}"
check "empty goal file allows the stop" allow "$(run "$(payload p2)")"

printf 'Drive PR 6310 to merged.\n' >"${goal}"
out="$(run "$(payload p3)")"
check "an active goal blocks the stop" block "${out}"

if printf '%s' "${out}" | rg 'Drive PR 6310' >/dev/null; then
	ok "the block reason quotes the goal text"
else
	no "the block reason quotes the goal text"
fi

if printf '%s' "${out}" | python3 -c 'import json,sys; json.load(sys.stdin)' 2>/dev/null; then
	ok "stdout is valid JSON"
else
	no "stdout is valid JSON"
fi

printf 'DONE\nDrive PR 6310 to merged.\n' >"${goal}"
check "DONE on the first line allows the stop" allow "$(run "$(payload p4)")"

# The budget is keyed on prompt_id, so it bounds continuations per USER
# MESSAGE. Without that a stuck agent could spin on one prompt forever.
printf 'Keep going.\n' >"${goal}"
blocks=0
for _ in 1 2 3 4 5; do
	printf '%s' "$(run "$(payload budget)")" | rg '"block"' >/dev/null && blocks=$((blocks + 1))
done
if [[ "${blocks}" -eq 3 ]]; then
	ok "the budget stops after 3 nudges for one prompt_id"
else
	no "the budget stops after 3 nudges for one prompt_id (got ${blocks})"
fi

check "a new prompt_id gets a fresh budget" block "$(run "$(payload fresh)")"

check "CLAUDE_GOAL_OFF=1 allows the stop" allow \
	"$(printf '%s' "$(payload p7)" | CLAUDE_GOAL_OFF=1 CLAUDE_GOAL_FILE="${goal}" bash "${HOOK}" 2>/dev/null)"
check "malformed JSON allows the stop" allow \
	"$(printf 'not json' | CLAUDE_GOAL_FILE="${goal}" bash "${HOOK}" 2>/dev/null)"
check "empty stdin allows the stop" allow \
	"$(printf '' | CLAUDE_GOAL_FILE="${goal}" bash "${HOOK}" 2>/dev/null)"

printf '%s' "$(payload p10)" | CLAUDE_GOAL_FILE="${goal}" bash "${HOOK}" >/dev/null 2>&1
rc=$?
if [[ "${rc}" -eq 0 ]]; then
	ok "the hook exits 0 even when it blocks"
else
	no "the hook exits 0 even when it blocks (got ${rc})"
fi

# ── 2. the BLOCKED escape ──────────────────────────────────────────────────
#
# The agent writes the goal file, so a bare "I am blocked" would be a
# self-issued permission slip. The escape is only worth having because the
# claim is checkable: a named watcher pid must actually be running.

printf 'Do the thing.\n' >"${goal}"
check "a plain goal still blocks (control)" block "$(run "$(payload c1)")"

printf 'BLOCKED: waiting on CI runners\nDo the thing.\n' >"${goal}"
check "BLOCKED with a reason allows the stop" allow "$(run "$(payload b1)")"

printf 'BLOCKED:\nDo the thing.\n' >"${goal}"
check "BLOCKED with an empty reason still blocks" block "$(run "$(payload b2)")"

sleep 300 &
live_pid=$!
printf 'BLOCKED: waiting on CI WATCH=%s\nDo the thing.\n' "${live_pid}" >"${goal}"
check "BLOCKED naming a LIVE watcher allows the stop" allow "$(run "$(payload b3)")"

kill "${live_pid}" 2>/dev/null
wait "${live_pid}" 2>/dev/null
printf 'BLOCKED: waiting on CI WATCH=%s\nDo the thing.\n' "${live_pid}" >"${goal}"
dead_out="$(run "$(payload b4)")"
check "BLOCKED naming a DEAD watcher still blocks" block "${dead_out}"

# A dead watcher means nothing will wake the agent, so the refusal has to say
# so -- otherwise the agent re-reads the same goal and stops again.
if printf '%s' "${dead_out}" | rg -i 'watcher' >/dev/null; then
	ok "the dead-watcher refusal names the watcher"
else
	no "the dead-watcher refusal names the watcher"
fi

printf 'BLOCKED: waiting on GitHub runners\nDo the thing.\n' >"${goal}"
audit="$(printf '%s' "$(payload b5)" | CLAUDE_GOAL_FILE="${goal}" bash "${HOOK}" 2>&1 >/dev/null)"
if printf '%s' "${audit}" | rg 'waiting on GitHub runners' >/dev/null; then
	ok "the claimed BLOCKED reason is echoed for audit"
else
	no "the claimed BLOCKED reason is echoed for audit"
fi

# A worktree path containing a space must still resolve its own goal file. An
# earlier version collapsed spaces to underscores to survive a whitespace-split
# `read`, which silently pointed the lookup at a path that does not exist -- so
# the per-worktree feature did nothing, without an error.
spacey="${work}/My Projects/eshu"
mkdir -p "${spacey}/.claude"
printf 'Worktree goal behind a space.\n' >"${spacey}/.claude/active-goal"
# Use the run-unique session id, not a literal. The nudge budget is keyed on
# (session_id, prompt_id) and its counter lives in TMPDIR, so a hardcoded id
# passes the first three runs on a machine and fails every run after -- while
# staying green forever on a fresh CI runner.
spacey_payload="$(SPACEY="${spacey}" SID="${sid}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "spacey",
                  "stop_hook_active": False,
                  "cwd": os.environ["SPACEY"], "hook_event_name": "Stop"}))
')"
spacey_out="$(printf '%s' "${spacey_payload}" | bash "${HOOK}" 2>/dev/null)"
if printf '%s' "${spacey_out}" | rg 'Worktree goal behind a space' >/dev/null; then
	ok "a worktree path containing a space still finds its goal file"
else
	no "a worktree path containing a space still finds its goal file"
fi

# On the ordinary blocking path the hook must write NOTHING to stderr. Every
# other case here pipes stderr to /dev/null, so none of them can see stray
# output -- which is exactly how 15 lines of orphaned parser survived a review
# and emitted two "command not found" lines on every Stop. The decision was
# right; the noise was somewhere nobody looked.
printf 'Quiet-path goal.\n' >"${goal}"
quiet_err="$(printf '%s' "$(payload quiet)" | CLAUDE_GOAL_FILE="${goal}" \
	bash "${HOOK}" 2>&1 >/dev/null)"
if [[ -z "${quiet_err}" ]]; then
	ok "the hook writes nothing to stderr on the plain blocking path"
else
	no "the hook writes nothing to stderr on the plain blocking path (got: ${quiet_err})"
fi

# A goal file carries `SESSION: <id>` when goal-refresh.sh wrote it. The Stop
# hook must compare that against its OWN session, or a later session in the same
# checkout is handed a previous session's stale goal -- the exact leak the header
# exists to prevent. The header was added to the producer first and the consumer
# read it as ordinary goal text, so this case failed silently in between.
printf 'SESSION: %s\nGoal owned by this very session.\n' "${sid}" >"${goal}"
check "a goal headed with MY session id still blocks" block "$(run "$(payload own)")"

printf 'SESSION: some-other-session\nGoal owned by somebody else.\n' >"${goal}"
xout="$(run "$(payload foreign)")"
check "a goal owned by ANOTHER session allows the stop" allow "${xout}"
if printf '%s' "${xout}" | rg 'Goal owned by somebody else' >/dev/null; then
	no "a foreign goal must never be handed back"
else
	ok "a foreign goal is never handed back"
fi

# The header must not leak into the text the agent is shown.
printf 'SESSION: %s\nThe actual objective.\n' "${sid}" >"${goal}"
hout="$(run "$(payload hdr)")"
if printf '%s' "${hout}" | rg 'SESSION:' >/dev/null; then
	no "the SESSION header is stripped before the goal is quoted"
else
	ok "the SESSION header is stripped before the goal is quoted"
fi

# DONE under a header must still retire the goal.
printf 'SESSION: %s\nDONE\nFinished objective.\n' "${sid}" >"${goal}"
check "DONE beneath a SESSION header allows the stop" allow "$(run "$(payload hdrdone)")"

printf '\ngoal-continue hook mirror: %s passed, %s failed\n' "${passed}" "${failed}"
[[ "${failed}" -eq 0 ]]
