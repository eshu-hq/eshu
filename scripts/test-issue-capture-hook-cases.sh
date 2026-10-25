#!/usr/bin/env bash
# Sourced by scripts/test-issue-capture-hook.sh -- not run on its own.
#
# The READ side of the goal issue ledger: the Stop hook's DONE refusal, the
# DEFER escape and its check, the independence of that refusal from the goal's
# no-progress budget, fail-open on a slow or dead gh, and the per-turn
# restatement plus orphan merge in goal-refresh.sh.
#
# A trigger path for the agent-canon gate. Its sentinel is the last line,
# asserted by the parent suite. Append ABOVE it.

# shellcheck disable=SC2154,SC2034 # variables come from the sourcing suite; the sentinel is read there

stop_payload() { # sid cwd prompt_id [transcript]
	SID="$1" CWD="$2" PID_="$3" TX="${4:-}" python3 -c '
import json, os
d = {"session_id": os.environ["SID"], "prompt_id": os.environ["PID_"],
     "stop_hook_active": False, "cwd": os.environ["CWD"],
     "hook_event_name": "Stop"}
if os.environ["TX"]:
    d["transcript_path"] = os.environ["TX"]
print(json.dumps(d))
'
}

refresh_payload() { # sid cwd
	SID="$1" CWD="$2" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "refresh",
                  "cwd": os.environ["CWD"], "prompt": "carry on",
                  "hook_event_name": "UserPromptSubmit"}))
'
}

stop_out="" stop_err="" stop_rc=0
run_stop() { # payload [VAR=value ...]
	local payload="$1"
	shift
	stop_out="$(printf '%s' "${payload}" | env "$@" bash "${CONTINUE}" 2>"${work}/stop.err")"
	stop_rc=$?
	stop_err="$(cat "${work}/stop.err")"
}
verdict_of() { if rg -q '"decision"[[:space:]]*:[[:space:]]*"block"' <<<"$1"; then printf block; else printf allow; fi; }
reason_of() {
	printf '%s' "$1" | python3 -c '
import json, sys
try:
    print(json.load(sys.stdin).get("reason", ""))
except Exception:
    pass
' 2>/dev/null
}

seed() { # ledger line... (appends, one per argument)
	local f="$1"
	shift
	local l
	for l in "$@"; do printf '%s\n' "${l}" >>"${f}"; done
}
OPEN9101='eshu-hq/eshu#9101 2026-10-25T00:00:00Z open'
nudge_counters() { ls "${TMPDIR}"/claude-goal-nudge-* 2>/dev/null | wc -l | tr -d ' '; }

# ── 6. Stop: DONE is refused while an issue is open ──────────────────────────
# `/goal done` prepends DONE to the per-session file; the lookup then PASSES
# OVER that tombstone, so the gate has to find it again -- the layout the owner
# actually produces. Two further layouts reach it through CLAUDE_GOAL_FILE,
# which is honoured as retired rather than skipped.
sdone="sd-${run_tag}"

fx_reset
R="$(mk_repo stop-tomb)"
printf 'DONE\nSESSION: %s\nFinish the thing.\n' "${sdone}" >"${R}/.claude/active-goal.${sdone}"
seed "$(ledger_of "${R}/.claude/active-goal.${sdone}")" "${OPEN9101}"
run_stop "$(stop_payload "${sdone}" "${R}" p-tomb)"
[[ "$(verdict_of "${stop_out}")" == block ]] &&
	ok "DONE tombstone + open issue: the stop is refused" || no "DONE tombstone + open issue: the stop is refused (out=${stop_out:0:80})"
reason="$(reason_of "${stop_out}")"
rg -q 'eshu-hq/eshu#9101' <<<"${reason}" && rg -q 'DEFER: #9101' <<<"${reason}" &&
	ok "the refusal names the issue and both exits" || no "the refusal names the issue and both exits"
rg -q -- 'issue view 9101 -R eshu-hq/eshu --json state' "${FIXTURES}/calls.log" &&
	ok "the gate asks gh for the issue state" || no "the gate asks gh for the issue state"
has_line "$(ledger_of "${R}/.claude/active-goal.${sdone}")" '^eshu-hq/eshu#9101 .* open$' &&
	ok "an open issue stays open in the ledger" || no "an open issue stays open in the ledger"

fx_reset
R="$(mk_repo stop-first-line)"
printf 'DONE\nFinish the thing.\n' >"${R}/goal"
seed "$(ledger_of "${R}/goal")" "${OPEN9101}"
run_stop "$(stop_payload "${sdone}" "${R}" p-first)" CLAUDE_GOAL_FILE="${R}/goal"
[[ "$(verdict_of "${stop_out}")" == block ]] &&
	ok "first-line DONE (explicit goal file) + open issue: refused" || no "first-line DONE (explicit goal file) + open issue: refused"

fx_reset
R="$(mk_repo stop-header-done)"
printf 'SESSION: %s\nDONE\nFinish the thing.\n' "${sdone}" >"${R}/goal"
seed "$(ledger_of "${R}/goal")" "${OPEN9101}"
run_stop "$(stop_payload "${sdone}" "${R}" p-header)" CLAUDE_GOAL_FILE="${R}/goal"
[[ "$(verdict_of "${stop_out}")" == block ]] &&
	ok "DONE under the SESSION header + open issue: refused" || no "DONE under the SESSION header + open issue: refused"

# ── 7. closed issues release the stop and are rewritten ──────────────────────
fx_reset
fx_state 9101 CLOSED
R="$(mk_repo stop-closed)"
printf 'DONE\nSESSION: %s\nFinish.\n' "${sdone}" >"${R}/.claude/active-goal.${sdone}"
seed "$(ledger_of "${R}/.claude/active-goal.${sdone}")" "${OPEN9101}"
run_stop "$(stop_payload "${sdone}" "${R}" p-closed)"
[[ "$(verdict_of "${stop_out}")" == allow ]] &&
	ok "every issue closed: DONE is allowed" || no "every issue closed: DONE is allowed"
has_line "$(ledger_of "${R}/.claude/active-goal.${sdone}")" '^eshu-hq/eshu#9101 .* closed$' &&
	ok "a closed issue's ledger line is rewritten closed" || no "a closed issue's ledger line is rewritten closed"

fx_reset
fx_state 9101 CLOSED
R="$(mk_repo stop-mixed)"
printf 'DONE\nSESSION: %s\nFinish.\n' "${sdone}" >"${R}/.claude/active-goal.${sdone}"
seed "$(ledger_of "${R}/.claude/active-goal.${sdone}")" "${OPEN9101}" 'eshu-hq/eshu#9102 2026-10-25T00:00:00Z open'
run_stop "$(stop_payload "${sdone}" "${R}" p-mixed)"
reason="$(reason_of "${stop_out}")"
[[ "$(verdict_of "${stop_out}")" == block ]] && rg -q '#9102' <<<"${reason}" && ! rg -q 'eshu#9101' <<<"${reason}" &&
	ok "one closed, one open: only the open one is named" || no "one closed, one open: only the open one is named"

# ── 8. DEFER is a checkable escape ───────────────────────────────────────────
# `DEFER: #N <path>`: the path must exist, be non-empty, and mention #N. The
# agent writes this file, so a bare claim would be a self-issued permission
# slip -- the same reason BLOCKED needs a live WATCH pid.
defer_case() { # label path-arg file-content-or-NONE expect
	local label="$1" patharg="$2" content="$3" expect="$4" Rd
	fx_reset
	Rd="$(mk_repo "defer-$RANDOM")"
	printf 'DONE\nSESSION: %s\nFinish.\n' "${sdone}" >"${Rd}/.claude/active-goal.${sdone}"
	mkdir -p "${Rd}/docs"
	case "${content}" in
	NONE) ;;
	EMPTY) : >"${Rd}/docs/ruling.md" ;;
	*) printf '%s\n' "${content}" >"${Rd}/docs/ruling.md" ;;
	esac
	seed "$(ledger_of "${Rd}/.claude/active-goal.${sdone}")" "${OPEN9101}" "DEFER: #9101 ${patharg}"
	run_stop "$(stop_payload "${sdone}" "${Rd}" "p-${RANDOM}")"
	if [[ "$(verdict_of "${stop_out}")" == "${expect}" ]]; then
		ok "DEFER ${label}: ${expect}"
	else
		no "DEFER ${label}: wanted ${expect}, got $(verdict_of "${stop_out}")"
	fi
	DEFER_REPO="${Rd}"
}
defer_case "path missing" docs/ruling.md NONE block
rg -q 'ruling.md' <<<"$(reason_of "${stop_out}")" && ok "DEFER refusal names the failing path" || no "DEFER refusal names the failing path"
defer_case "path empty" docs/ruling.md EMPTY block
defer_case "path lacks #N" docs/ruling.md 'A ruling about issue 9101 without the hash.' block
defer_case "path mentions only #91011" docs/ruling.md 'Deferred #91011 for a good reason.' block
defer_case "path valid (relative to the worktree)" docs/ruling.md 'Ruling: #9101 is deferred to the next quarter.' allow
has_line "$(ledger_of "${DEFER_REPO}/.claude/active-goal.${sdone}")" '^eshu-hq/eshu#9101 .* deferred docs/ruling.md$' &&
	! rg -q '^DEFER:' "$(ledger_of "${DEFER_REPO}/.claude/active-goal.${sdone}")" &&
	ok "a valid DEFER rewrites the line deferred with its path and drops the DEFER line" ||
	no "a valid DEFER rewrites the line deferred with its path and drops the DEFER line"
defer_case "absolute path missing" "${work}/no-such-ruling.md" NONE block
printf 'Ruling #9101 deferred.\n' >"${work}/abs-ruling.md"
fx_reset
Rd="$(mk_repo defer-abs)"
printf 'DONE\nSESSION: %s\nFinish.\n' "${sdone}" >"${Rd}/.claude/active-goal.${sdone}"
seed "$(ledger_of "${Rd}/.claude/active-goal.${sdone}")" "${OPEN9101}" "DEFER: eshu-hq/eshu#9101 ${work}/abs-ruling.md"
run_stop "$(stop_payload "${sdone}" "${Rd}" p-abs)"
[[ "$(verdict_of "${stop_out}")" == allow ]] &&
	ok "DEFER owner/repo#N with an absolute path: allow" || no "DEFER owner/repo#N with an absolute path: allow"

# ── 9. fail-open: a slow or dead gh never wedges the session ─────────────────
fx_reset
fx_mode hang
R="$(mk_repo stop-hang)"
printf 'DONE\nSESSION: %s\nFinish.\n' "${sdone}" >"${R}/.claude/active-goal.${sdone}"
seed "$(ledger_of "${R}/.claude/active-goal.${sdone}")" "${OPEN9101}"
t0=$SECONDS
run_stop "$(stop_payload "${sdone}" "${R}" p-hang)" CLAUDE_GOAL_ISSUE_CALL_TIMEOUT=1 CLAUDE_GOAL_ISSUE_TOTAL_TIMEOUT=2
elapsed=$((SECONDS - t0))
[[ "$(verdict_of "${stop_out}")" == allow ]] && ok "gh hangs: the stop is allowed" || no "gh hangs: the stop is allowed"
rg -q 'goal-continue: NOT_CHECKED issues: .*eshu-hq/eshu#9101' <<<"${stop_err}" &&
	ok "gh hangs: NOT_CHECKED names the issue on stderr" || no "gh hangs: NOT_CHECKED names the issue on stderr (err=${stop_err:0:120})"
[[ "${elapsed}" -le 8 ]] && ok "gh hangs: bounded by the timeouts (${elapsed}s)" || no "gh hangs: bounded by the timeouts (${elapsed}s)"

fx_reset
fx_mode fail
R="$(mk_repo stop-dead)"
printf 'DONE\nSESSION: %s\nFinish.\n' "${sdone}" >"${R}/.claude/active-goal.${sdone}"
seed "$(ledger_of "${R}/.claude/active-goal.${sdone}")" "${OPEN9101}"
run_stop "$(stop_payload "${sdone}" "${R}" p-dead)"
[[ "$(verdict_of "${stop_out}")" == allow ]] && rg -q 'NOT_CHECKED issues' <<<"${stop_err}" &&
	ok "gh unreachable: allowed with NOT_CHECKED on stderr" || no "gh unreachable: allowed with NOT_CHECKED on stderr"
has_line "$(ledger_of "${R}/.claude/active-goal.${sdone}")" '^eshu-hq/eshu#9101 .* open$' &&
	ok "an unchecked issue stays open in the ledger" || no "an unchecked issue stays open in the ledger"

fx_reset
R="$(mk_repo stop-garbage)"
printf 'DONE\nSESSION: %s\nFinish.\n' "${sdone}" >"${R}/.claude/active-goal.${sdone}"
printf 'this is not a ledger line\n\n   \nowner/repo#notanumber x open\n' >"$(ledger_of "${R}/.claude/active-goal.${sdone}")"
run_stop "$(stop_payload "${sdone}" "${R}" p-garbage)"
[[ "${stop_rc}" -eq 0 && "$(verdict_of "${stop_out}")" == allow ]] &&
	ok "a garbage ledger never wedges the stop" || no "a garbage ledger never wedges the stop"

# ── 10. the refusal never spends, or hides behind, the goal's own budget ──────
# The DONE gate must not read or write the no-progress counter, or three refusals
# would exhaust it and the fourth stop would release OPEN issues silently.
fx_reset
R="$(mk_repo stop-budget)"
printf 'DONE\nSESSION: %s\nFinish.\n' "${sdone}" >"${R}/.claude/active-goal.${sdone}"
seed "$(ledger_of "${R}/.claude/active-goal.${sdone}")" "${OPEN9101}"
before_counters="$(nudge_counters)"
blocks=0
for _ in 1 2 3 4 5 6; do
	run_stop "$(stop_payload "${sdone}" "${R}" p-budget)"
	[[ "$(verdict_of "${stop_out}")" == block ]] && blocks=$((blocks + 1))
done
[[ "${blocks}" -eq 6 ]] && ok "six stops on one prompt_id with an open issue: all six refused (more than CLAUDE_GOAL_MAX_NUDGES)" ||
	no "six stops on one prompt_id with an open issue: all six refused (got ${blocks})"
[[ "$(nudge_counters)" == "${before_counters}" ]] &&
	ok "the issue refusal writes no goal nudge counter" || no "the issue refusal writes no goal nudge counter"

# An ACTIVE goal is refused as before and the gate stays off the network.
fx_reset
R="$(mk_repo stop-active)"
printf 'SESSION: %s\nKeep going.\n' "${sdone}" >"${R}/.claude/active-goal.${sdone}"
seed "$(ledger_of "${R}/.claude/active-goal.${sdone}")" "${OPEN9101}"
run_stop "$(stop_payload "${sdone}" "${R}" p-active)"
[[ "$(verdict_of "${stop_out}")" == block && "$(gh_calls)" == 0 ]] &&
	ok "an active goal blocks as before and makes no gh call" || no "an active goal blocks as before and makes no gh call (calls=$(gh_calls))"

# No ledger, DONE goal: the pre-existing allow, untouched, no gh call.
fx_reset
R="$(mk_repo stop-noledger)"
printf 'DONE\nSESSION: %s\nFinish.\n' "${sdone}" >"${R}/.claude/active-goal.${sdone}"
run_stop "$(stop_payload "${sdone}" "${R}" p-nol)"
[[ "$(verdict_of "${stop_out}")" == allow && "$(gh_calls)" == 0 ]] &&
	ok "DONE goal with no ledger: allowed, no gh call" || no "DONE goal with no ledger: allowed, no gh call"

# ── 11. refresh: restate open issues, never touching the network ─────────────
srf="rf-${run_tag}"
fx_reset
R="$(mk_repo refresh-restate)"
printf 'SESSION: %s\nRESTATEOBJECTIVE for the drive.\n' "${srf}" >"${R}/.claude/active-goal.${srf}"
seed "$(ledger_of "${R}/.claude/active-goal.${srf}")" \
	'eshu-hq/eshu#9201 2026-10-25T00:00:00Z open' \
	'eshu-hq/eshu#9202 2026-10-25T00:00:00Z closed' \
	'eshu-hq/eshu#9203 2026-10-25T00:00:00Z deferred docs/ruling.md'
rf_out="$(refresh_payload "${srf}" "${R}" | bash "${REFRESH}" 2>/dev/null)"
ctx="$(ctx_of "${rf_out}")"
rg -q 'RESTATEOBJECTIVE' <<<"${ctx}" && rg -q 'eshu-hq/eshu#9201' <<<"${ctx}" && rg -q 'DEFER: #9201' <<<"${ctx}" &&
	ok "refresh restates the open issue and its exits next to the goal" || no "refresh restates the open issue and its exits next to the goal"
! rg -q '#9202|#9203' <<<"${ctx}" && ok "refresh omits closed and deferred issues" || no "refresh omits closed and deferred issues"
[[ "$(gh_calls)" == 0 ]] && ok "refresh restates with no gh call" || no "refresh restates with no gh call ($(gh_calls))"

# Orphans: the lead's refresh merges them into its own ledger and restates.
fx_reset
R="$(mk_repo refresh-orphan)"
printf 'SESSION: %s\nORPHANOBJECTIVE.\n' "${srf}" >"${R}/.claude/active-goal.${srf}"
seed "$(ledger_of "${R}/.claude/active-goal.${srf}")" 'eshu-hq/eshu#9301 2026-10-25T00:00:00Z open'
seed "${R}/.claude/active-goal.orphan.issues" \
	'eshu-hq/eshu#9301 2026-10-25T00:00:00Z open' \
	'eshu-hq/eshu#9302 2026-10-25T00:01:00Z open'
rf_out="$(refresh_payload "${srf}" "${R}" | bash "${REFRESH}" 2>/dev/null)"
ctx="$(ctx_of "${rf_out}")"
L="$(ledger_of "${R}/.claude/active-goal.${srf}")"
has_line "${L}" '^eshu-hq/eshu#9302 ' && [[ "$(rg -c '^eshu-hq/eshu#9301 ' "${L}")" == 1 ]] &&
	ok "refresh merges the orphan ledger into the lead's, without duplicating" || no "refresh merges the orphan ledger into the lead's, without duplicating"
[[ ! -f "${R}/.claude/active-goal.orphan.issues" ]] && ok "the merged orphan ledger is consumed" || no "the merged orphan ledger is consumed"
rg -q '#9302' <<<"${ctx}" && ok "the merged orphan issue is restated on the same turn" || no "the merged orphan issue is restated on the same turn"

# A session with no goal of its own must not swallow the orphans.
fx_reset
R="$(mk_repo refresh-orphan-nogoal)"
seed "${R}/.claude/active-goal.orphan.issues" 'eshu-hq/eshu#9303 2026-10-25T00:01:00Z open'
rf_out="$(refresh_payload "${srf}" "${R}" | bash "${REFRESH}" 2>/dev/null)"
[[ -z "${rf_out}" && -f "${R}/.claude/active-goal.orphan.issues" ]] &&
	ok "a session with no goal leaves the orphan ledger alone" || no "a session with no goal leaves the orphan ledger alone"

# The whole loop: capture (two leads -> orphan), then the lead's refresh.
fx_reset
R="$(mk_repo loop)"
printf 'SESSION: lead-a\nLOOPGOAL A.\n' >"${R}/.claude/active-goal.lead-a"
printf 'SESSION: lead-b\nLOOPGOAL B.\n' >"${R}/.claude/active-goal.lead-b"
run_capture "$(post_payload "obs-${run_tag}" "${R}" 'gh issue create -R eshu-hq/eshu --title t' 'https://github.com/eshu-hq/eshu/issues/9401')"
rf_out="$(refresh_payload lead-a "${R}" | bash "${REFRESH}" 2>/dev/null)"
rg -q '#9401' <<<"$(ctx_of "${rf_out}")" && has_line "${R}/.claude/active-goal.lead-a.issues" '^eshu-hq/eshu#9401 ' &&
	ok "capture -> orphan -> lead refresh restates the issue" || no "capture -> orphan -> lead refresh restates the issue"

# ── 12. the refusal is loud: issues, count, escalation, DEFER echo ───────────
# Unbounded by design, so a stuck agent has to show: every refusal names the open
# issues and the consecutive count on stderr, and from the third no-progress
# refusal the block reason says to escalate. The count has its own state file.
ESCALATE='Escalate to arbiter-eshu for the DEFER ruling now.'
sloud="sl-${run_tag}"
fx_reset
R="$(mk_repo loud)"
printf 'DONE\nSESSION: %s\nFinish.\n' "${sloud}" >"${R}/.claude/active-goal.${sloud}"
seed "$(ledger_of "${R}/.claude/active-goal.${sloud}")" "${OPEN9101}" 'eshu-hq/eshu#9102 2026-10-25T00:00:00Z open'
before_counters="$(nudge_counters)"
run_stop "$(stop_payload "${sloud}" "${R}" p-loud)"
rg -q 'eshu-hq/eshu#9101' <<<"${stop_err}" && rg -q 'eshu-hq/eshu#9102' <<<"${stop_err}" && rg -q 'consecutive refusal 1\b' <<<"${stop_err}" &&
	ok "refusal 1: stderr names the open issues and count 1" || no "refusal 1: stderr names the open issues and count 1 (err=${stop_err:0:160})"
! rg -qF "${ESCALATE}" <<<"$(reason_of "${stop_out}")" && ok "refusal 1: no escalation sentence yet" || no "refusal 1: no escalation sentence yet"
run_stop "$(stop_payload "${sloud}" "${R}" p-loud)"
rg -q 'consecutive refusal 2\b' <<<"${stop_err}" && ok "refusal 2: stderr count is 2" || no "refusal 2: stderr count is 2 (err=${stop_err:0:160})"
! rg -qF "${ESCALATE}" <<<"$(reason_of "${stop_out}")" && ok "refusal 2: no escalation sentence yet" || no "refusal 2: no escalation sentence yet"
run_stop "$(stop_payload "${sloud}" "${R}" p-loud)"
rg -q 'consecutive refusal 3\b' <<<"${stop_err}" && rg -qF "${ESCALATE}" <<<"$(reason_of "${stop_out}")" &&
	ok "refusal 3 with no tool calls and no transcript: the reason adds the escalation sentence" ||
	no "refusal 3 with no tool calls and no transcript: the reason adds the escalation sentence"
run_stop "$(stop_payload "${sloud}" "${R}" p-loud-new)"
[[ "$(verdict_of "${stop_out}")" == block ]] && rg -qF "${ESCALATE}" <<<"$(reason_of "${stop_out}")" &&
	ok "refusal 4 (new prompt_id): still refused, still escalating; the count is the session's" ||
	no "refusal 4 (new prompt_id): still refused, still escalating"
[[ "$(nudge_counters)" == "${before_counters}" ]] &&
	ok "the refusal count lives in its own state file, never the goal's nudge counter" ||
	no "the refusal count lives in its own state file, never the goal's nudge counter"

# Tool calls since the last refusal reset the consecutive count.
fx_reset
R="$(mk_repo loud-progress)"
sprog="sp-${run_tag}"
printf 'DONE\nSESSION: %s\nFinish.\n' "${sprog}" >"${R}/.claude/active-goal.${sprog}"
seed "$(ledger_of "${R}/.claude/active-goal.${sprog}")" "${OPEN9101}"
txl="${work}/loud-transcript.jsonl"
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{}}]}}' >"${txl}"
for _ in 1 2 3; do run_stop "$(stop_payload "${sprog}" "${R}" p-prog "${txl}")"; done
rg -q 'consecutive refusal 3\b' <<<"${stop_err}" && rg -qF "${ESCALATE}" <<<"$(reason_of "${stop_out}")" &&
	ok "transcript present, no new tool calls: the third refusal escalates" || no "transcript present, no new tool calls: the third refusal escalates"
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{}}]}}' >>"${txl}"
run_stop "$(stop_payload "${sprog}" "${R}" p-prog "${txl}")"
rg -q 'consecutive refusal 1\b' <<<"${stop_err}" && ! rg -qF "${ESCALATE}" <<<"$(reason_of "${stop_out}")" &&
	ok "a stop after tool calls resets the count to 1 and drops the escalation" || no "a stop after tool calls resets the count to 1 and drops the escalation (err=${stop_err:0:160})"
run_stop "$(stop_payload "${sprog}" "${R}" p-prog "${txl}")"
rg -q 'consecutive refusal 2\b' <<<"${stop_err}" && ok "the count climbs again from the reset" || no "the count climbs again from the reset"

# Closing the issues ends the run: the next refusal starts at 1.
fx_state 9101 CLOSED
run_stop "$(stop_payload "${sprog}" "${R}" p-prog "${txl}")"
[[ "$(verdict_of "${stop_out}")" == allow ]] && ok "issue closed: the stop is allowed" || no "issue closed: the stop is allowed"
fx_state 9101 OPEN
seed "$(ledger_of "${R}/.claude/active-goal.${sprog}")" 'eshu-hq/eshu#9105 2026-10-25T00:00:00Z open'
run_stop "$(stop_payload "${sprog}" "${R}" p-prog "${txl}")"
rg -q 'consecutive refusal 1\b' <<<"${stop_err}" &&
	ok "after the issues cleared, a later refusal starts again at 1" || no "after the issues cleared, a later refusal starts again at 1 (err=${stop_err:0:160})"

# A transcript path that does not exist (Muse exec mode) is no progress.
fx_reset
R="$(mk_repo loud-missing)"
smis="sm-${run_tag}"
printf 'DONE\nSESSION: %s\nFinish.\n' "${smis}" >"${R}/.claude/active-goal.${smis}"
seed "$(ledger_of "${R}/.claude/active-goal.${smis}")" "${OPEN9101}"
for _ in 1 2 3; do run_stop "$(stop_payload "${smis}" "${R}" p-mis "${work}/no-such-transcript.jsonl")"; done
rg -qF "${ESCALATE}" <<<"$(reason_of "${stop_out}")" &&
	ok "a missing transcript counts as no progress: the third refusal escalates" || no "a missing transcript counts as no progress: the third refusal escalates"

# An honoured DEFER is echoed with its path, the way CONSENT is.
fx_reset
R="$(mk_repo defer-echo)"
sdef="sf-${run_tag}"
printf 'DONE\nSESSION: %s\nFinish.\n' "${sdef}" >"${R}/.claude/active-goal.${sdef}"
mkdir -p "${R}/docs"
printf 'Ruling: #9101 is deferred.\n' >"${R}/docs/ruling.md"
seed "$(ledger_of "${R}/.claude/active-goal.${sdef}")" "${OPEN9101}" "DEFER: #9101 docs/ruling.md"
run_stop "$(stop_payload "${sdef}" "${R}" p-defer-echo)"
rg -q 'DEFER honoured.*eshu-hq/eshu#9101.*docs/ruling.md' <<<"${stop_err}" &&
	ok "an honoured DEFER is echoed to stderr with its path" || no "an honoured DEFER is echoed to stderr with its path (err=${stop_err:0:160})"
fx_reset
R="$(mk_repo defer-echo-refused)"
printf 'DONE\nSESSION: %s\nFinish.\n' "${sdef}" >"${R}/.claude/active-goal.${sdef}"
seed "$(ledger_of "${R}/.claude/active-goal.${sdef}")" "${OPEN9101}" "DEFER: #9101 docs/missing.md"
run_stop "$(stop_payload "${sdef}" "${R}" p-defer-echo2)"
! rg -q 'DEFER honoured' <<<"${stop_err}" && ok "a refused DEFER is not echoed as honoured" || no "a refused DEFER is not echoed as honoured"

# ── 13. DEFER cannot point at the ledger or a goal file ──────────────────────
# Every ledger record names its own issue, and the agent writes the ledger and
# the goal file: accepting either as a "ruling" is a self-issued permission slip.
# A path that resolves to the ledger, its lock or temp files, or any
# .claude/active-goal* file is refused, symlinks included.
self_defer_case() { # label setup-fn-name
	local label="$1" setup="$2" Rs g l
	fx_reset
	Rs="$(mk_repo "selfdefer-$RANDOM")"
	g="${Rs}/.claude/active-goal.${sdone}"
	l="$(ledger_of "${g}")"
	printf 'DONE\nSESSION: %s\nFinish; this goal file mentions #9101.\n' "${sdone}" >"${g}"
	seed "${l}" "${OPEN9101}"
	DEFER_PATH="$("${setup}" "${Rs}" "${g}" "${l}")"
	seed "${l}" "DEFER: #9101 ${DEFER_PATH}"
	run_stop "$(stop_payload "${sdone}" "${Rs}" "p-${RANDOM}")"
	if [[ "$(verdict_of "${stop_out}")" == block ]] && ! rg -q 'DEFER honoured' <<<"${stop_err}" &&
		rg -q 'not a ruling' <<<"$(reason_of "${stop_out}")" &&
		has_line "${l}" '^eshu-hq/eshu#9101 .* open$'; then
		ok "DEFER ${label}: refused, the stop stays blocked, the record stays open"
	else
		no "DEFER ${label}: wanted a refusal naming 'not a ruling' (verdict=$(verdict_of "${stop_out}") err=${stop_err:0:120})"
	fi
}
sd_ledger() { printf '%s' ".claude/active-goal.${sdone}.issues"; }
sd_goal() { printf '%s' ".claude/active-goal.${sdone}"; }
sd_lock() { printf 'held #9101\n' >"$3.lock"; printf '%s' ".claude/active-goal.${sdone}.issues.lock"; }
sd_tmp() { printf 'scratch #9101\n' >"$3.tmp.123"; printf '%s' ".claude/active-goal.${sdone}.issues.tmp.123"; }
sd_other_goal() { printf 'Other goal #9101\n' >"$1/.claude/active-goal.other"; printf '%s' ".claude/active-goal.other"; }
sd_abs_ledger() { printf '%s' "$3"; }
sd_symlink() { mkdir -p "$1/docs"; ln -s "$3" "$1/docs/ruling.md"; printf '%s' "docs/ruling.md"; }
sd_dotdot() { mkdir -p "$1/docs"; printf '%s' "docs/../.claude/active-goal.${sdone}.issues"; }
self_defer_case "naming the ledger itself" sd_ledger
self_defer_case "naming the ledger by absolute path" sd_abs_ledger
self_defer_case "naming the goal file" sd_goal
self_defer_case "naming the ledger's lock file" sd_lock
self_defer_case "naming a ledger temp file" sd_tmp
self_defer_case "naming another active-goal file" sd_other_goal
self_defer_case "naming a symlink that resolves to the ledger" sd_symlink
self_defer_case "naming the ledger through .." sd_dotdot

# ── 14. the total cap bounds a whole check; the lock serialises writers ──────
# One hung call is bounded by the per-call cap alone, hiding a missing total cap.
# Eight hung issues are 8 s at 1 s each; only the 3 s total cap keeps this under
# 5 s, and the calls never made must still be reported.
fx_reset
fx_mode hang
R="$(mk_repo stop-many-hung)"
printf 'DONE\nSESSION: %s\nFinish.\n' "${sdone}" >"${R}/.claude/active-goal.${sdone}"
for n in 9601 9602 9603 9604 9605 9606 9607 9608; do
	seed "$(ledger_of "${R}/.claude/active-goal.${sdone}")" "eshu-hq/eshu#${n} 2026-10-25T00:00:00Z open"
done
t0=$SECONDS
run_stop "$(stop_payload "${sdone}" "${R}" p-many)" CLAUDE_GOAL_ISSUE_CALL_TIMEOUT=1 CLAUDE_GOAL_ISSUE_TOTAL_TIMEOUT=3
elapsed=$((SECONDS - t0))
[[ "$(verdict_of "${stop_out}")" == allow && "${elapsed}" -le 5 ]] &&
	ok "8 hung issues: bounded by the total cap (${elapsed}s, not 8 x the call cap)" || no "8 hung issues: bounded by the total cap (${elapsed}s, verdict=$(verdict_of "${stop_out}"))"
all_named=1
for n in 9601 9602 9603 9604 9605 9606 9607 9608; do rg -q "NOT_CHECKED issues: .*eshu-hq/eshu#${n}" <<<"${stop_err}" || all_named=0; done
[[ "${all_named}" == 1 ]] && ok "8 hung issues: every one is NOT_CHECKED, including those never asked" || no "8 hung issues: every one is NOT_CHECKED (err=${stop_err:0:200})"
[[ "$(gh_calls)" -le 4 ]] && ok "8 hung issues: gh is not called once the total cap is spent ($(gh_calls) calls)" || no "8 hung issues: gh is not called once the total cap is spent ($(gh_calls) calls)"

# Hold the ledger's lock from outside, then capture: it has to wait for the lock
# and lose neither the line already there nor the new one.
fx_reset
R="$(mk_repo lock-held)"
printf 'SESSION: %s\nGoal.\n' "${lead}" >"${R}/.claude/active-goal.${lead}"
LL="$(ledger_of "${R}/.claude/active-goal.${lead}")"
seed "${LL}" 'eshu-hq/eshu#9701 2026-10-25T00:00:00Z open'
LK="${LL}.lock" HELD="${work}/lock-held.flag" python3 -c '
import fcntl, os, time
with open(os.environ["LK"], "a") as fh:
    fcntl.flock(fh, fcntl.LOCK_EX)
    open(os.environ["HELD"], "w").close()
    time.sleep(3)
' &
holder=$!
for _ in $(seq 1 50); do [[ -f "${work}/lock-held.flag" ]] && break; sleep 0.1; done
t0=$SECONDS
run_capture "$(post_payload "${lead}" "${R}" 'gh issue create -R eshu-hq/eshu --title t' 'https://github.com/eshu-hq/eshu/issues/9702')"
elapsed=$((SECONDS - t0))
wait "${holder}" 2>/dev/null
[[ "${elapsed}" -ge 2 ]] && ok "capture waits for the held ledger lock (${elapsed}s)" || no "capture waits for the held ledger lock (${elapsed}s)"
has_line "${LL}" '^eshu-hq/eshu#9701 ' && has_line "${LL}" '^eshu-hq/eshu#9702 ' &&
	ok "the writer behind the lock loses no line" || no "the writer behind the lock loses no line"

# ── 15. an unheaded DONE is gated too ────────────────────────────────────────
# The agent writes its goal file, so an unheaded DONE cannot be an exit: it would
# drop the SESSION header, put DONE first, and file into a ledger nothing reads.
# Gated at the per-session, shared and per-user paths; a DONE headed for another
# session is still that session's. The owner's exit is the sidecar.
unheaded_done_case() { # label goal-path-fn
	local label="$1" gfn="$2" Ru g l
	fx_reset
	Ru="$(mk_repo "unheaded-$RANDOM")"
	g="$("${gfn}" "${Ru}")"
	printf 'DONE\nFinish.\n' >"${g}"
	l="$(ledger_of "${g}")"
	seed "${l}" "${OPEN9101}"
	run_stop "$(stop_payload "${sdone}" "${Ru}" "p-${RANDOM}")"
	if [[ "$(verdict_of "${stop_out}")" == block ]] && rg -q 'eshu-hq/eshu#9101' <<<"$(reason_of "${stop_out}")"; then
		ok "unheaded DONE ${label} + open issue: the stop is refused"
	else
		no "unheaded DONE ${label} + open issue: the stop is refused (verdict=$(verdict_of "${stop_out}"))"
	fi
	sed -i.bak 's/ open$/ closed/' "${l}" && rm -f "${l}.bak"
	run_stop "$(stop_payload "${sdone}" "${Ru}" "p-${RANDOM}")"
	[[ "$(verdict_of "${stop_out}")" == allow ]] &&
		ok "unheaded DONE ${label}: marking the record closed in the sidecar releases the stop" ||
		no "unheaded DONE ${label}: marking the record closed in the sidecar releases the stop"
}
ud_session() { printf '%s' "$1/.claude/active-goal.${sdone}"; }
ud_shared() { printf '%s' "$1/.claude/active-goal"; }
ud_home() { mkdir -p "${HOME}/.claude"; printf '%s' "${HOME}/.claude/active-goal"; }
unheaded_done_case "at the per-session path" ud_session
unheaded_done_case "at the shared path" ud_shared
unheaded_done_case "at the per-user path" ud_home
rm -f "${HOME}/.claude/active-goal" "${HOME}/.claude/active-goal.issues" "${HOME}/.claude/active-goal.issues.lock"

fx_reset
R="$(mk_repo foreign-done)"
printf 'DONE\nSESSION: somebody-else\nTheir goal.\n' >"${R}/.claude/active-goal"
seed "$(ledger_of "${R}/.claude/active-goal")" "${OPEN9101}"
run_stop "$(stop_payload "${sdone}" "${R}" p-foreign-done)"
[[ "$(verdict_of "${stop_out}")" == allow ]] &&
	ok "a DONE headed for another session is that session's: this stop is not gated" ||
	no "a DONE headed for another session is that session's: this stop is not gated"

# LAST line on purpose -- the parent suite asserts the companion ran to here.
issue_capture_cases_loaded=1
