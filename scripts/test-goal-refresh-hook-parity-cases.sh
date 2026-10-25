#!/usr/bin/env bash
# Sourced by scripts/test-goal-refresh-hook.sh -- not run on its own.
#
# The CROSS-HOOK parity matrix: every goal-file layout run against BOTH hooks,
# asserting they agree on the decision rather than on output shape (one injects
# context, the other refuses a stop -- different outputs, same question).
#
# The question both must answer identically: does THIS session own an active,
# unfinished goal here, and what is its text.
#
# This file exists because two PR reviewers, reading the diff cold, found in
# minutes what eleven rounds of ours did not: goal-continue.sh was hardened for
# the CONSENT-above-SESSION layout and goal-refresh.sh was never checked for the
# same shape. Both hooks parse the same file format in opposite orders. Testing
# each side separately is exactly what let that through -- every case here was
# green on both hooks individually while the two disagreed with each other.
#
# A trigger path for the agent-canon gate. Its sentinel is the last line,
# asserted by test-agent-hooks.sh along with every sibling. Append ABOVE it.

# refresh_decision <session> <cwd> -> "none" or the goal text it would restate
refresh_decision() {
	local out
	out="$(SID="$1" CWD="$2" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "parity",
                  "cwd": os.environ["CWD"], "prompt": "carry on",
                  "hook_event_name": "UserPromptSubmit"}))
' | bash "${REFRESH}" 2>/dev/null)"
	printf '%s' "${out}" | python3 -c '
import json, sys
raw = sys.stdin.read().strip()
if not raw:
    print("none"); raise SystemExit
try:
    d = json.loads(raw)
except Exception:
    print("none"); raise SystemExit
ctx = d.get("hookSpecificOutput", {}).get("additionalContext", "")
if not ctx:
    print("none"); raise SystemExit
# the goal text only: between the header line and the standing instructions
body = ctx.split("cannot go stale):", 1)[-1]
for tail in ("Keep working it", "OWNER CONSENT ALREADY GRANTED"):
    body = body.split(tail, 1)[0]
print(body.strip() or "none")
' 2>/dev/null
}

# stop_decision <session> <cwd> <probe-id> -> "none" or the goal text handed back
#
# The probe id must be UNIQUE per call and must come from the CALLER. The nudge
# counter is keyed on (session_id, prompt_id) in TMPDIR, so a shared id means
# the fourth probe reads a spent budget and the hook allows the stop -- which
# this harness reads as "the Stop hook does not enforce this layout". That trap
# caught this matrix twice: first with a literal id, then with a counter
# incremented INSIDE the function, which runs in a command-substitution
# subshell and so reset to 1 on every call. Both times it invented five
# disagreements that do not exist.
stop_decision() {
	local out
	out="$(SID="$1" CWD="$2" PROBE="$3" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "parity-"+os.environ["PROBE"],
                  "stop_hook_active": False, "cwd": os.environ["CWD"],
                  "hook_event_name": "Stop"}))
' | bash "${CONTINUE}" 2>/dev/null)"
	printf '%s' "${out}" | python3 -c '
import json, sys
raw = sys.stdin.read().strip()
if not raw:
    print("none"); raise SystemExit
try:
    d = json.loads(raw)
except Exception:
    print("none"); raise SystemExit
reason = d.get("reason", "")
if not reason:
    print("none"); raise SystemExit
body = reason.split("ACTIVE GOAL (", 1)[-1].split("):", 1)[-1]
for tail in ("Continue it now", "OWNER CONSENT ALREADY GRANTED"):
    body = body.split(tail, 1)[0]
print(body.strip() or "none")
' 2>/dev/null
}

# parity_case <label> <session> <expect: enforce|silent> <marker> <file-content>
#
# `marker` is a distinctive string from the objective. When a layout should
# enforce, BOTH hooks must carry it; when it should be silent, NEITHER may
# mention it and neither may emit anything at all.
parity_case() {
	local label="$1" session="$2" expect="$3" marker="$4" content="$5"
	local dir r s r_has s_has
	dir="$(mktemp -d)"
	mkdir -p "${dir}/.claude"
	printf '%s' "${content}" >"${dir}/.claude/active-goal"
	r="$(refresh_decision "${session}" "${dir}")"
	s="$(stop_decision "${session}" "${dir}" "${marker}")"
	rm -rf "${dir}"

	case "${r}" in *"${marker}"*) r_has=yes ;; *) r_has=no ;; esac
	case "${s}" in *"${marker}"*) s_has=yes ;; *) s_has=no ;; esac

	if [[ "${r_has}" != "${s_has}" ]]; then
		no "PARITY ${label}: hooks disagree on the DECISION (refresher=${r_has} stop=${s_has}) -- same file, same session"
		return
	fi
	# Deciding alike is not enough: they must show the agent the SAME GOAL. A
	# decision-only assertion passes while one hook silently truncates the
	# objective, which is exactly the defect that got past the first version of
	# this matrix -- a filter meant to drop the header line deleted every
	# SESSION: line in the body.
	if [[ "${r_has}" == yes && "${r}" != "${s}" ]]; then
		no "PARITY ${label}: same decision, DIFFERENT goal text -- refresher=[${r}] stop=[${s}]"
		return
	fi
	case "${expect}" in
		enforce)
			if [[ "${r_has}" == yes ]]; then
				ok "PARITY ${label}: both enforce"
			else
				no "PARITY ${label}: both silent, expected both to enforce"
			fi
			;;
		silent)
			if [[ "${r_has}" == no && "${r}" == none && "${s}" == none ]]; then
				ok "PARITY ${label}: both silent"
			else
				no "PARITY ${label}: expected both silent (refresher=${r:0:40} stop=${s:0:40})"
			fi
			;;
	esac
}

# The round-5 ordering matrix, pointed at BOTH hooks instead of one. `me` is
# this session; `somebody-else` is not.
#
# RUN-UNIQUE, via the suite's own `sid` (which carries $$). A literal here is
# stable across runs, so the TMPDIR nudge counters survive between invocations
# and the enforce cases -- the only ones that spend budget -- start failing on
# the fourth run of the day while passing in isolation. That is this trap's
# third bite in one sitting: literal prompt id, then a counter incremented in a
# command-substitution subshell, now a stable session id across runs. The suite
# already documents it once for the same reason.
me="parity-${sid}"

parity_case "plain own goal" "${me}" enforce "PLAINGOAL" \
	"SESSION: ${me}
PLAINGOAL objective.
"
parity_case "DONE first, unheaded" "${me}" silent "UNHEADEDDONE" \
	"DONE
UNHEADEDDONE objective.
"
parity_case "SESSION own + DONE under the header" "${me}" silent "HEADEDDONE" \
	"SESSION: ${me}
DONE
HEADEDDONE objective.
"
parity_case "SESSION foreign" "${me}" silent "FOREIGNGOAL" \
	"SESSION: somebody-else
FOREIGNGOAL objective.
"
parity_case "CONSENT above own SESSION" "${me}" enforce "CONSENTOWN" \
	"CONSENT: push
SESSION: ${me}
CONSENTOWN objective.
"
parity_case "CONSENT above foreign SESSION" "${me}" silent "CONSENTFOREIGN" \
	"CONSENT: push
SESSION: somebody-else
CONSENTFOREIGN objective.
"
parity_case "CONSENT above DONE" "${me}" silent "CONSENTDONE" \
	"CONSENT: push
DONE
CONSENTDONE objective.
"
parity_case "CONSENT above own SESSION above DONE" "${me}" silent "CONSENTHEADEDDONE" \
	"CONSENT: push
SESSION: ${me}
DONE
CONSENTHEADEDDONE objective.
"
parity_case "body line starting SESSION:" "${me}" enforce "BODYHEADER" \
	"SESSION: ${me}
BODYHEADER objective.
SESSION: is documented above
"
parity_case "body carrying SESSION: lines, indented and flush" "${me}" enforce "BODYSESSIONS" \
	"SESSION: ${me}
BODYSESSIONS objective.
SESSION: doc A is discussed here
  SESSION: doc B, indented
trailing line.
"
parity_case "only CONSENT lines, no objective" "${me}" silent "NOTHINGHERE" \
	"CONSENT: push
"
parity_case "unheaded owner goal" "${me}" enforce "UNHEADEDACTIVE" \
	"UNHEADEDACTIVE objective.
"

# A retired per-session file must not shadow an ACTIVE shared goal: the session
# retired its own objective, the owner's shared one is still live, and both
# hooks must reach it. Reported by codex as "no output from either hook"; what
# actually happened was worse -- the refresher injected the retired goal.
tomb="${work}/tombstone"
mkdir -p "${tomb}/.claude"
printf 'SESSION: %s\nDONE\nRETIRED objective.\n' "${me}" >"${tomb}/.claude/active-goal.${me}"
printf 'SHAREDACTIVE objective.\n' >"${tomb}/.claude/active-goal"
tomb_r="$(refresh_decision "${me}" "${tomb}")"
tomb_s="$(stop_decision "${me}" "${tomb}" tombstone)"
if rg -q 'RETIRED' <<<"${tomb_r}"; then
	no "a retired per-session goal is never restated"
else
	ok "a retired per-session goal is never restated"
fi
if rg -q 'SHAREDACTIVE' <<<"${tomb_r}" && rg -q 'SHAREDACTIVE' <<<"${tomb_s}"; then
	ok "a retired per-session goal falls through to the active shared goal, in both hooks"
else
	no "a retired per-session goal falls through to the active shared goal, in both hooks (refresher=${tomb_r:0:60} stop=${tomb_s:0:60})"
fi

# An explicit CLAUDE_GOAL_FILE naming a RETIRED goal is honoured as retired
# rather than falling through to another file: the owner named that file. This
# is also the path that exercises the post-strip DONE re-check -- with the
# lookup skip handling every discovered candidate, dropping that re-check left
# the suite green, which is an untested guard by any other name.
ovr="${work}/override"
mkdir -p "${ovr}/.claude"
printf 'SESSION: %s\nDONE\nOVERRIDEDONE objective.\n' "${me}" >"${ovr}/retired-goal"
printf 'DECOY active objective.\n' >"${ovr}/.claude/active-goal"
ovr_inj="$(injected "$(SID="${me}" PROMPT='carry on' CWD="${ovr}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "override-retired",
                  "cwd": os.environ["CWD"], "prompt": os.environ["PROMPT"],
                  "hook_event_name": "UserPromptSubmit"}))
' | CLAUDE_GOAL_FILE="${ovr}/retired-goal" bash "${REFRESH}" 2>/dev/null)")"
if [[ -z "${ovr_inj}" ]]; then
	ok "an explicit override naming a retired goal stays retired"
else
	no "an explicit override naming a retired goal stays retired (got: ${ovr_inj:0:60})"
fi
if rg -q 'DECOY' <<<"${ovr_inj}"; then
	no "an explicit override never falls through to another goal file"
else
	ok "an explicit override never falls through to another goal file"
fi

# ── the goal issue ledger: three readers, one resolver ──────────────────────
#
# issue-capture.sh, goal-continue.sh and goal-refresh.sh all find a session's
# ledger through lib/goal-issues.sh, so they must agree layout by layout:
#   refresher  restates an open ledger issue iff the goal is active for the session
#   capture    files a new issue into that same ledger (kind "goal") iff the
#              resolver finds an active goal for the session
#   Stop       refuses DONE for an open ledger issue iff the goal was retired and
#              is this session's or unheaded (a DONE headed for ANOTHER session
#              is that session's)
# gh is a fake on PATH; the refresher is also asserted to make NO call.
ic_work="${work}/issue-parity"
mkdir -p "${ic_work}/bin" "${ic_work}/fx"
cp "${repo_root}/scripts/lib/test-issue-capture-fake-gh.sh" "${ic_work}/bin/gh"
chmod +x "${ic_work}/bin/gh"

# issue_parity_case <label> <refresher: yes|no> <capture-into-goal: yes|no> <stop: block|allow> <file-content>
issue_parity_case() {
	local label="$1" want_r="$2" want_c="$3" want_s="$4" content="$5"
	local dir ctx cap_in got_r got_c got_s ledger out
	dir="$(mktemp -d)"
	mkdir -p "${dir}/.claude" "${dir}/.agents/skills/eshu-code-review"
	printf '%s' "${content}" >"${dir}/.claude/active-goal"
	ledger="${dir}/.claude/active-goal.issues"
	printf 'eshu-hq/eshu#7777 2026-10-25T00:00:00Z open\n' >"${ledger}"
	: >"${ic_work}/fx/calls.log"

	ctx="$(injected "$(SID="${me}" CWD="${dir}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "ip", "cwd": os.environ["CWD"],
                  "prompt": "carry on", "hook_event_name": "UserPromptSubmit"}))
' | PATH="${ic_work}/bin:${PATH}" FIXTURES="${ic_work}/fx" bash "${REFRESH}" 2>/dev/null)")"
	got_r=no
	rg -q 'eshu-hq/eshu#7777' <<<"${ctx}" && got_r=yes
	[[ "$(wc -l <"${ic_work}/fx/calls.log" | tr -d ' ')" == 0 ]] || got_r="${got_r}+network"

	cap_in="$(SID="${me}" CWD="${dir}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "cwd": os.environ["CWD"],
                  "tool_input": {"command": "gh issue create -R eshu-hq/eshu --title t"},
                  "tool_response": {"stdout": "https://github.com/eshu-hq/eshu/issues/7778"}}))
')"
	printf '%s' "${cap_in}" | PATH="${ic_work}/bin:${PATH}" FIXTURES="${ic_work}/fx" \
		bash "${repo_root}/.claude/hooks/issue-capture.sh" >/dev/null 2>&1
	got_c=no
	rg -q '^eshu-hq/eshu#7778 ' "${ledger}" && got_c=yes

	out="$(SID="${me}" CWD="${dir}" python3 -c '
import json, os
print(json.dumps({"session_id": os.environ["SID"], "prompt_id": "ips-" + os.environ["SID"],
                  "stop_hook_active": False, "cwd": os.environ["CWD"], "hook_event_name": "Stop"}))
' | PATH="${ic_work}/bin:${PATH}" FIXTURES="${ic_work}/fx" bash "${CONTINUE}" 2>/dev/null)"
	got_s=allow
	rg -q '"decision"[[:space:]]*:[[:space:]]*"block"' <<<"${out}" && got_s=block
	rm -rf "${dir}"

	# An ACTIVE goal also blocks the Stop hook (the goal itself); what this case
	# pins is the ISSUE refusal, so an active layout only has to agree it blocks.
	if [[ "${got_r}" == "${want_r}" && "${got_c}" == "${want_c}" && "${got_s}" == "${want_s}" ]]; then
		ok "ISSUE LEDGER PARITY ${label}: refresher=${got_r} capture-into-goal=${got_c} stop=${got_s}"
	else
		no "ISSUE LEDGER PARITY ${label}: refresher=${got_r} capture=${got_c} stop=${got_s} (want ${want_r}/${want_c}/${want_s})"
	fi
}
ime="issue-${sid}"
me="${ime}"
issue_parity_case "own goal, active" yes yes block \
	"SESSION: ${me}
ISSUEPLAIN objective.
"
issue_parity_case "unheaded owner goal, active" yes yes block \
	"ISSUEUNHEADED objective.
"
issue_parity_case "CONSENT above own SESSION, active" yes yes block \
	"CONSENT: push
SESSION: ${me}
ISSUECONSENT objective.
"
# A shared goal headed for another session is not THIS session's goal (refresher
# silent, Stop allows), but it is the one live goal in the checkout, so capture
# files into it through the single-lead fallback rather than dropping the issue.
issue_parity_case "foreign SESSION (single-lead fallback)" no yes allow \
	"SESSION: somebody-else
ISSUEFOREIGN objective.
"
issue_parity_case "own SESSION, DONE beneath it (retired)" no no block \
	"SESSION: ${me}
DONE
ISSUEHEADEDDONE objective.
"
issue_parity_case "CONSENT above own SESSION above DONE (retired)" no no block \
	"CONSENT: push
SESSION: ${me}
DONE
ISSUECONSENTDONE objective.
"
# The agent writes the goal file, so an unheaded DONE cannot be an exit: it would
# rewrite the file without the header and walk past the ledger.
issue_parity_case "unheaded DONE (retired) still gates on the ledger" no no block \
	"DONE
ISSUEOWNERDONE objective.
"
me="parity-${sid}"

# LAST line on purpose -- see the sibling companions.
goal_refresh_parity_cases_loaded=1
