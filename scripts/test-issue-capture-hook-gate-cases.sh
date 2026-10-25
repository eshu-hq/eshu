#!/usr/bin/env bash
# Sourced by scripts/test-issue-capture-hook-cases.sh -- not run on its own.
#
# The Stop gate's hardening cases: a DEFER cannot name the ledger or a goal file
# (by path, case variant, symlink or hardlink), the 30 s total cap bounds a check,
# and the ledger lock serialises writers. Split out of the sibling companion when
# it reached the 500-line cap.
#
# A trigger path for the agent-canon gate. Its sentinel is the last line,
# asserted by the sibling companion. Append ABOVE it.

# shellcheck disable=SC2154,SC2034 # variables come from the sourcing suite; the sentinel is read there

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
# File identity, not spelling: a hardlink shares the ledger's inode, and on a
# case-insensitive volume (macOS default) `.CLAUDE/ACTIVE-GOAL...` is the same file.
sd_hardlink() { mkdir -p "$1/docs"; ln "$3" "$1/docs/ruling.md"; printf '%s' "docs/ruling.md"; }
sd_case_dir() { printf '%s' ".CLAUDE/active-goal.${sdone}.issues"; }
sd_case_name() { printf '%s' ".claude/ACTIVE-GOAL.${sdone}.issues"; }
sd_case_tmp() { printf 'scratch #9101\n' >"$3.tmp.123"; printf '%s' ".claude/active-goal.${sdone}.issues.TMP.123"; }
self_defer_case "naming a hardlink to the ledger" sd_hardlink
probe_dir="$(mk_repo case-probe)"
if [[ -d "${probe_dir}/.CLAUDE" ]]; then
	self_defer_case "naming the ledger with the directory in another case" sd_case_dir
	self_defer_case "naming the ledger with the file name in another case" sd_case_name
	self_defer_case "naming a ledger temp file in another case" sd_case_tmp
else
	ok "SKIPPED (case-sensitive filesystem, a case variant is a different file): ledger named in another case"
fi

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

# LAST line on purpose -- the sibling companion asserts this file ran to here.
issue_capture_gate_cases_loaded=1
