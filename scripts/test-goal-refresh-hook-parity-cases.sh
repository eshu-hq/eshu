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
if printf '%s' "${tomb_r}" | rg -q 'RETIRED'; then
	no "a retired per-session goal is never restated"
else
	ok "a retired per-session goal is never restated"
fi
if printf '%s' "${tomb_r}" | rg -q 'SHAREDACTIVE' && printf '%s' "${tomb_s}" | rg -q 'SHAREDACTIVE'; then
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
if printf '%s' "${ovr_inj}" | rg -q 'DECOY'; then
	no "an explicit override never falls through to another goal file"
else
	ok "an explicit override never falls through to another goal file"
fi

# LAST line on purpose -- see the sibling companions.
goal_refresh_parity_cases_loaded=1
