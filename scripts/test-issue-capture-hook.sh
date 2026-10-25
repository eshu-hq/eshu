#!/usr/bin/env bash
# Behavioural mirror for the goal issue ledger: .claude/hooks/issue-capture.sh
# (PostToolUse on Bash), the shared reader/writer in
# .claude/hooks/lib/goal-issues.sh, and the two hooks that read the ledger --
# goal-continue.sh (the DONE refusal) and goal-refresh.sh (the per-turn
# restatement). See docs/internal/agent-hooks-issues.md.
#
# `gh` is a fake on PATH (scripts/lib/test-issue-capture-fake-gh.sh): the suite
# never touches the network and records every call, so "restates without
# network" is an assertion rather than a hope.
#
# The cases live in two files. This one holds the harness and the CAPTURE
# cases; scripts/test-issue-capture-hook-cases.sh (sourced below) holds the
# Stop-hook gate, the DEFER escape, the budget, and the refresher. Its load
# sentinel is asserted here so a companion that stops being sourced fails loudly.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CAPTURE="${repo_root}/.claude/hooks/issue-capture.sh"
CONTINUE="${repo_root}/.claude/hooks/goal-continue.sh"
REFRESH="${repo_root}/.claude/hooks/goal-refresh.sh"
LIB="${repo_root}/.claude/hooks/lib/goal-issues.sh"
FAKE_GH="${repo_root}/scripts/lib/test-issue-capture-fake-gh.sh"
FIXDIR="${repo_root}/scripts/testdata/issue-capture"

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
mkdir -p "${work}/tmp" "${work}/home/.claude" "${work}/bin" "${work}/fx"
# Hermeticity, the sibling suites' rule: the hooks resolve $HOME/.claude/
# active-goal and honor the CLAUDE_GOAL_* switches, so ambient session state
# would skew the cases. A per-run fake HOME and TMPDIR, and no ambient knobs.
export TMPDIR="${work}/tmp"
export HOME="${work}/home"
unset CLAUDE_GOAL_FILE CLAUDE_GOAL_OFF CLAUDE_GOAL_CONSENT CLAUDE_GOAL_MAX_NUDGES
unset CLAUDE_GOAL_ISSUE_CALL_TIMEOUT CLAUDE_GOAL_ISSUE_TOTAL_TIMEOUT
[[ "${HOME}" == "${work}/home" && -z "${CLAUDE_GOAL_FILE:-}" && -z "${CLAUDE_GOAL_OFF:-}" ]] ||
	{ printf 'NOT OK - suite hermeticity header broken\n' >&2; exit 1; }

cp "${FAKE_GH}" "${work}/bin/gh"
chmod +x "${work}/bin/gh"
export FIXTURES="${work}/fx"
export PATH="${work}/bin:${PATH}"

passed=0
failed=0
ok() { printf 'ok - %s\n' "$1"; passed=$((passed + 1)); }
no() { printf 'not ok - %s\n' "$1"; failed=$((failed + 1)); }

# Run-unique ids: the nudge counter is keyed on (session, prompt) in TMPDIR.
run_tag="ic$$"
OLD_ISO="2026-01-01T00:00:00Z"

fx_reset() { rm -rf "${FIXTURES}"; mkdir -p "${FIXTURES}"; : >"${FIXTURES}/calls.log"; }
fx_mode() { printf '%s' "$1" >"${FIXTURES}/mode"; }
fx_state() { printf '%s' "$2" >"${FIXTURES}/state-$1"; }
fx_created() { printf '%s' "$2" >"${FIXTURES}/created-$1"; }
gh_calls() { wc -l <"${FIXTURES}/calls.log" | tr -d ' '; }

# mk_repo <name>: a directory that looks like an Eshu checkout to the hooks
# (the project-skill marker eshu_root_path walks up to find).
mk_repo() {
	local d="${work}/$1"
	mkdir -p "${d}/.claude" "${d}/.agents/skills/eshu-code-review"
	printf '%s' "${d}"
}

# post_payload <sid> <cwd> <command> <stdout> [extra-json]: a Claude Bash
# PostToolUse payload in the shape recorded live (scripts/testdata/
# issue-capture/claude-posttool-bash-*.json). Built with python so no quoting
# in a command string can corrupt the JSON.
post_payload() {
	SID="$1" CWD="$2" CMD="$3" OUT="$4" EXTRA="${5:-}" python3 -c '
import json, os
d = {"session_id": os.environ["SID"], "cwd": os.environ["CWD"],
     "hook_event_name": "PostToolUse", "tool_name": "Bash",
     "tool_input": {"command": os.environ["CMD"]},
     "tool_response": {"stdout": os.environ["OUT"], "stderr": "",
                       "interrupted": False, "isImage": False}}
d.update(json.loads(os.environ["EXTRA"] or "{}"))
print(json.dumps(d))
'
}

cap_out="" cap_err="" cap_rc=0
run_capture() { # payload
	cap_out="$(printf '%s' "$1" | bash "${CAPTURE}" 2>"${work}/cap.err")"
	cap_rc=$?
	cap_err="$(cat "${work}/cap.err")"
}

ledger_of() { printf '%s.issues' "$1"; } # goal-file path -> its sidecar
has_line() { # file pattern
	[[ -f "$1" ]] && rg -q -- "$2" "$1"
}
ctx_of() { # hook stdout -> additionalContext text
	printf '%s' "$1" | python3 -c '
import json, sys
try:
    print(json.load(sys.stdin)["hookSpecificOutput"]["additionalContext"])
except Exception:
    pass
' 2>/dev/null
}

[[ -f "${CAPTURE}" ]] || { no "issue-capture.sh exists at ${CAPTURE}"; }
[[ -f "${LIB}" ]] || { no "lib/goal-issues.sh exists at ${LIB}"; }
bash -n "${CAPTURE}" 2>/dev/null && ok "issue-capture.sh parses" || no "issue-capture.sh parses"
bash -n "${LIB}" 2>/dev/null && ok "lib/goal-issues.sh parses" || no "lib/goal-issues.sh parses"

# ── 1. capture: what fires it ───────────────────────────────────────────────
lead="lead-${run_tag}"

fx_reset
R="$(mk_repo create-cmd)"
printf 'SESSION: %s\nFinish the thing.\n' "${lead}" >"${R}/.claude/active-goal.${lead}"
run_capture "$(post_payload "${lead}" "${R}" 'gh issue create -R eshu-hq/eshu --title t --body b' 'https://github.com/eshu-hq/eshu/issues/9101')"
L="$(ledger_of "${R}/.claude/active-goal.${lead}")"
[[ "${cap_rc}" -eq 0 ]] && ok "create via command: exits 0" || no "create via command: exits 0 (rc=${cap_rc})"
has_line "${L}" '^eshu-hq/eshu#9101 [0-9T:Z-]+ open$' &&
	ok "create via command: ledger line written" || no "create via command: ledger line written"
ctx="$(ctx_of "${cap_out}")"
rg -q 'eshu-hq/eshu#9101' <<<"${ctx}" && rg -q 'DEFER: #9101' <<<"${ctx}" &&
	ok "create via command: additionalContext names the issue and the DEFER exit" ||
	no "create via command: additionalContext names the issue and the DEFER exit"
rg -q 'eshu-hq/eshu#9101' <<<"${cap_err}" &&
	ok "create via command: echoed to stderr" || no "create via command: echoed to stderr"
rg -q -- 'issue view 9101 -R eshu-hq/eshu --json createdAt' "${FIXTURES}/calls.log" &&
	ok "create via command: confirmed with gh issue view" || no "create via command: confirmed with gh issue view"

# A script that files issues prints the bare URL; no command text to match.
fx_reset
R="$(mk_repo bare-url)"
printf 'SESSION: %s\nGoal.\n' "${lead}" >"${R}/.claude/active-goal.${lead}"
run_capture "$(post_payload "${lead}" "${R}" 'bash scripts/file-followups.sh' $'filing...\nhttps://github.com/eshu-hq/eshu/issues/9102\nhttps://github.com/eshu-hq/eshu/issues/9103\ndone')"
L="$(ledger_of "${R}/.claude/active-goal.${lead}")"
has_line "${L}" '^eshu-hq/eshu#9102 ' && has_line "${L}" '^eshu-hq/eshu#9103 ' &&
	ok "bare URL lines: every created issue captured" || no "bare URL lines: every created issue captured"

# gh api creation returns JSON; the URL is html_url, not a bare line.
fx_reset
R="$(mk_repo api-create)"
printf 'SESSION: %s\nGoal.\n' "${lead}" >"${R}/.claude/active-goal.${lead}"
run_capture "$(post_payload "${lead}" "${R}" 'gh api repos/eshu-hq/eshu/issues -f title=t -f body=b' '{"number": 9104, "html_url": "https://github.com/eshu-hq/eshu/issues/9104", "repository_url": "https://api.github.com/repos/eshu-hq/eshu"}')"
has_line "$(ledger_of "${R}/.claude/active-goal.${lead}")" '^eshu-hq/eshu#9104 ' &&
	ok "gh api POST /issues: html_url captured" || no "gh api POST /issues: html_url captured"

# `--input` makes gh send a POST (verified with GH_DEBUG=api); REST --input is the
# usual fallback when gh's GraphQL path fails, and the body comes back compact.
fx_reset
R="$(mk_repo api-input)"
printf 'SESSION: %s\nGoal.\n' "${lead}" >"${R}/.claude/active-goal.${lead}"
run_capture "$(post_payload "${lead}" "${R}" 'gh api repos/o/r/issues --input body.json' '{"number":9120,"html_url":"https://github.com/o/r/issues/9120","state":"open"}')"
has_line "$(ledger_of "${R}/.claude/active-goal.${lead}")" '^o/r#9120 ' &&
	ok "gh api POST via --input (compact JSON html_url): captured" || no "gh api POST via --input (compact JSON html_url): captured"

fx_reset
R="$(mk_repo api-comment)"
printf 'SESSION: %s\nGoal.\n' "${lead}" >"${R}/.claude/active-goal.${lead}"
run_capture "$(post_payload "${lead}" "${R}" 'gh api repos/eshu-hq/eshu/issues/5/comments -f body=x' '{"html_url": "https://github.com/eshu-hq/eshu/issues/5#issuecomment-1"}')"
[[ ! -f "$(ledger_of "${R}/.claude/active-goal.${lead}")" ]] &&
	ok "gh api comment on an existing issue: not captured" || no "gh api comment on an existing issue: not captured"

# ── 2. capture: what must NOT fire it ────────────────────────────────────────
# Read verbs print URLs of OLD issues. With gh unreachable the age check cannot
# save us, so the verb exclusion has to hold on its own.
fx_reset
fx_mode fail
R="$(mk_repo read-verbs)"
printf 'SESSION: %s\nGoal.\n' "${lead}" >"${R}/.claude/active-goal.${lead}"
run_capture "$(post_payload "${lead}" "${R}" 'gh issue list -R eshu-hq/eshu --json url -q ".[].url"' $'https://github.com/eshu-hq/eshu/issues/12\nhttps://github.com/eshu-hq/eshu/issues/13')"
run_capture "$(post_payload "${lead}" "${R}" 'gh issue view 12 -R eshu-hq/eshu' $'title:\tX\nurl:\thttps://github.com/eshu-hq/eshu/issues/12')"
[[ ! -f "$(ledger_of "${R}/.claude/active-goal.${lead}")" ]] &&
	ok "gh issue list / view output: nothing captured" || no "gh issue list / view output: nothing captured"

# A script that prints an OLD issue's URL on its own line: gh works, the issue
# is months old, so it is not "created during this goal".
fx_reset
fx_created 9105 "${OLD_ISO}"
R="$(mk_repo old-issue)"
printf 'SESSION: %s\nGoal.\n' "${lead}" >"${R}/.claude/active-goal.${lead}"
run_capture "$(post_payload "${lead}" "${R}" 'bash scripts/report.sh' 'https://github.com/eshu-hq/eshu/issues/9105')"
[[ ! -f "$(ledger_of "${R}/.claude/active-goal.${lead}")" ]] &&
	ok "an issue created more than 15 minutes ago is not captured" || no "an issue created more than 15 minutes ago is not captured"

# gh failure: capture anyway (cheap, reversible).
fx_reset
fx_mode fail
R="$(mk_repo gh-fail)"
printf 'SESSION: %s\nGoal.\n' "${lead}" >"${R}/.claude/active-goal.${lead}"
run_capture "$(post_payload "${lead}" "${R}" 'gh issue create -R eshu-hq/eshu --title t' 'https://github.com/eshu-hq/eshu/issues/9106')"
has_line "$(ledger_of "${R}/.claude/active-goal.${lead}")" '^eshu-hq/eshu#9106 ' &&
	ok "gh unreachable: the issue is captured anyway" || no "gh unreachable: the issue is captured anyway"

# Same URL twice: one ledger line.
fx_reset
R="$(mk_repo dedupe)"
printf 'SESSION: %s\nGoal.\n' "${lead}" >"${R}/.claude/active-goal.${lead}"
p="$(post_payload "${lead}" "${R}" 'gh issue create -R eshu-hq/eshu --title t' 'https://github.com/eshu-hq/eshu/issues/9107')"
run_capture "${p}"
run_capture "${p}"
[[ "$(rg -c '^eshu-hq/eshu#9107 ' "$(ledger_of "${R}/.claude/active-goal.${lead}")" 2>/dev/null)" == "1" ]] &&
	ok "the same issue captured twice yields one ledger line" || no "the same issue captured twice yields one ledger line"

# ── 3. envelopes recorded live ───────────────────────────────────────────────
# Muse delivers tool_response as a JSON-ENCODED STRING whose object carries the
# text under `output`, not `stdout` (scripts/testdata/issue-capture/
# muse-posttool-bash.json). A bare string response must work too.
fx_reset
R="$(mk_repo muse-env)"
printf 'SESSION: %s\nGoal.\n' "${lead}" >"${R}/.claude/active-goal.${lead}"
muse_p="$(FIX="${FIXDIR}" SID="${lead}" CWD="${R}" python3 -c '
import json, os
d = json.load(open(os.environ["FIX"] + "/muse-posttool-bash.json"))
d["session_id"] = os.environ["SID"]
d["cwd"] = os.environ["CWD"]
d["tool_input"]["command"] = "gh issue create -R eshu-hq/eshu --title t"
d["tool_response"] = json.dumps({"output": "https://github.com/eshu-hq/eshu/issues/9108\n", "exit_code": 0})
print(json.dumps(d))
')"
run_capture "${muse_p}"
has_line "$(ledger_of "${R}/.claude/active-goal.${lead}")" '^eshu-hq/eshu#9108 ' &&
	ok "Muse envelope (JSON-string tool_response, output key): captured" ||
	no "Muse envelope (JSON-string tool_response, output key): captured"

fx_reset
R="$(mk_repo muse-plain)"
printf 'SESSION: %s\nGoal.\n' "${lead}" >"${R}/.claude/active-goal.${lead}"
run_capture "$(post_payload "${lead}" "${R}" 'gh issue create -R eshu-hq/eshu --title t' '' '{"tool_response": "https://github.com/eshu-hq/eshu/issues/9109\n"}')"
has_line "$(ledger_of "${R}/.claude/active-goal.${lead}")" '^eshu-hq/eshu#9109 ' &&
	ok "plain-string tool_response: captured" || no "plain-string tool_response: captured"

# ── 4. whose ledger ──────────────────────────────────────────────────────────
# Recorded live: a Claude Code SUBAGENT's PostToolUse carries the LEAD's
# session_id (plus agent_id/agent_type), so it resolves the lead's goal
# directly. Muse observers and other sessions DO have their own ids, which is
# what the fallback is for.
fx_reset
R="$(mk_repo subagent)"
printf 'SESSION: %s\nGoal.\n' "${lead}" >"${R}/.claude/active-goal.${lead}"
run_capture "$(post_payload "${lead}" "${R}" 'gh issue create -R eshu-hq/eshu --title t' 'https://github.com/eshu-hq/eshu/issues/9110' '{"agent_id":"a94bb56e95918e2c7","agent_type":"general-purpose"}')"
has_line "$(ledger_of "${R}/.claude/active-goal.${lead}")" '^eshu-hq/eshu#9110 ' &&
	ok "Claude subagent (same session_id): lands in the lead's ledger" || no "Claude subagent (same session_id): lands in the lead's ledger"

fx_reset
R="$(mk_repo observer)"
printf 'SESSION: %s\nGoal.\n' "${lead}" >"${R}/.claude/active-goal.${lead}"
touch "$(ledger_of "${R}/.claude/active-goal.${lead}")" "${R}/.claude/active-goal.${lead}.tmp"
run_capture "$(post_payload "observer-${run_tag}" "${R}" 'gh issue create -R eshu-hq/eshu --title t' 'https://github.com/eshu-hq/eshu/issues/9111')"
has_line "$(ledger_of "${R}/.claude/active-goal.${lead}")" '^eshu-hq/eshu#9111 ' &&
	ok "distinct session_id, ONE lead goal: falls back to that lead (sidecars and .tmp are not candidates)" ||
	no "distinct session_id, ONE lead goal: falls back to that lead (sidecars and .tmp are not candidates)"

fx_reset
R="$(mk_repo two-leads)"
printf 'SESSION: lead-a\nGoal A.\n' >"${R}/.claude/active-goal.lead-a"
printf 'SESSION: lead-b\nGoal B.\n' >"${R}/.claude/active-goal.lead-b"
run_capture "$(post_payload "observer-${run_tag}" "${R}" 'gh issue create -R eshu-hq/eshu --title t' 'https://github.com/eshu-hq/eshu/issues/9112')"
has_line "${R}/.claude/active-goal.orphan.issues" '^eshu-hq/eshu#9112 ' &&
	[[ ! -f "${R}/.claude/active-goal.lead-a.issues" && ! -f "${R}/.claude/active-goal.lead-b.issues" ]] &&
	ok "two leads: orphan ledger, neither lead guessed" || no "two leads: orphan ledger, neither lead guessed"
rg -qi 'orphan' <<<"${cap_err}" && ok "two leads: orphan fallback announced on stderr" || no "two leads: orphan fallback announced on stderr"

# Zero live goals is "no goal", not an orphan: nothing would ever claim the
# line, and the next goal started in this checkout would inherit an unrelated
# session's issue.
fx_reset
R="$(mk_repo retired-only)"
printf 'DONE\nSESSION: lead-a\nGoal A.\n' >"${R}/.claude/active-goal.lead-a"
run_capture "$(post_payload "observer-${run_tag}" "${R}" 'gh issue create -R eshu-hq/eshu --title t' 'https://github.com/eshu-hq/eshu/issues/9113')"
[[ "${cap_rc}" -eq 0 && -z "${cap_out}" && ! -f "${R}/.claude/active-goal.orphan.issues" ]] &&
	ok "zero live goals (a retired goal is not a lead): silent, no orphan written" ||
	no "zero live goals (a retired goal is not a lead): silent, no orphan written"
# Silent on stdout, but not mute: one stderr line per issue so the owner can see
# the issue was seen and deliberately not ledgered.
fx_reset
R="$(mk_repo zero-goal-line)"
run_capture "$(post_payload "observer-${run_tag}" "${R}" 'bash scripts/file-followups.sh' $'https://github.com/eshu-hq/eshu/issues/9118\nhttps://github.com/eshu-hq/eshu/issues/9119')"
[[ "$(rg -c '^issue-capture: no live goal in ' <<<"${cap_err}")" == 2 ]] &&
	rg -qF "issue-capture: no live goal in ${R}; eshu-hq/eshu#9118 not ledgered" <<<"${cap_err}" &&
	rg -qF "issue-capture: no live goal in ${R}; eshu-hq/eshu#9119 not ledgered" <<<"${cap_err}" &&
	[[ -z "${cap_out}" && ! -f "${R}/.claude/active-goal.orphan.issues" ]] &&
	ok "zero live goals: exactly one stderr line per issue, still no ledger" ||
	no "zero live goals: exactly one stderr line per issue, still no ledger (err=${cap_err:0:200})"

# A shared goal headed for another session is that session's live goal: this
# session does not own it, but it is the ONE lead in the checkout.
fx_reset
R="$(mk_repo foreign-shared)"
printf 'SESSION: somebody-else\nTheir goal.\n' >"${R}/.claude/active-goal"
run_capture "$(post_payload "${lead}" "${R}" 'gh issue create -R eshu-hq/eshu --title t' 'https://github.com/eshu-hq/eshu/issues/9114')"
has_line "${R}/.claude/active-goal.issues" '^eshu-hq/eshu#9114 ' &&
	ok "a shared goal headed for another session is the single lead: its sidecar" ||
	no "a shared goal headed for another session is the single lead: its sidecar"

fx_reset
R="$(mk_repo shared-unheaded)"
printf 'Owner hand-written goal.\n' >"${R}/.claude/active-goal"
run_capture "$(post_payload "${lead}" "${R}" 'gh issue create -R eshu-hq/eshu --title t' 'https://github.com/eshu-hq/eshu/issues/9115')"
has_line "${R}/.claude/active-goal.issues" '^eshu-hq/eshu#9115 ' &&
	ok "an unheaded shared goal is every session's goal: its sidecar" || no "an unheaded shared goal is every session's goal: its sidecar"

# ── 5. silence and fail-open ─────────────────────────────────────────────────
fx_reset
R="$(mk_repo no-goal)"
run_capture "$(post_payload "${lead}" "${R}" 'gh issue create -R eshu-hq/eshu --title t' 'https://github.com/eshu-hq/eshu/issues/9116')"
[[ "${cap_rc}" -eq 0 && -z "${cap_out}" && -z "$(ls "${R}/.claude" 2>/dev/null)" ]] &&
	ok "Eshu checkout, no goal anywhere: silent, nothing written" || no "Eshu checkout, no goal anywhere: silent, nothing written"

fx_reset
NE="${work}/not-eshu"
mkdir -p "${NE}/.claude"
printf 'SESSION: %s\nGoal.\n' "${lead}" >"${NE}/.claude/active-goal.${lead}"
run_capture "$(post_payload "${lead}" "${NE}" 'gh issue create -R eshu-hq/eshu --title t' 'https://github.com/eshu-hq/eshu/issues/9117')"
[[ "${cap_rc}" -eq 0 && -z "${cap_out}" && ! -f "${NE}/.claude/active-goal.${lead}.issues" ]] &&
	ok "outside an Eshu checkout: silent, nothing written" || no "outside an Eshu checkout: silent, nothing written"

for bad in '' 'not json' '{"tool_input":' '[]' '{"session_id":"x"}' '{"session_id":"x","cwd":"/nonexistent","tool_input":{"command":3},"tool_response":7}'; do
	run_capture "${bad}"
	[[ "${cap_rc}" -eq 0 && -z "${cap_out}" ]] && ok "malformed payload exits 0 silently: ${bad:0:30}" ||
		no "malformed payload exits 0 silently: ${bad:0:30} (rc=${cap_rc} out=${cap_out:0:40})"
done

# ── 6. wiring: registered, or the behaviour is correct and unreachable ───────
# Assert the full (event, matcher, command) triple, not that a filename appears
# somewhere: a hook wired to the wrong matcher is dead code that reads as live.
# (test-agent-hooks.sh is grandfathered over the 500-line cap and may not grow,
# so this lives here rather than beside that suite's own wiring table.)
wired() { # json-file event matcher command-substring [min-timeout]
	python3 -c '
import json, sys
path, event, matcher, name, floor = sys.argv[1:6]
hooks = json.load(open(path)).get("hooks", {})
hit = [h for g in hooks.get(event, []) if g.get("matcher") == (matcher or None)
       for h in g.get("hooks", []) if name in h.get("command", "")]
ok = bool(hit) and all(float(h.get("timeout", 0)) >= float(floor or 0) for h in hit)
sys.exit(0 if ok else 1)
' "$1" "$2" "$3" "$4" "${5:-0}" 2>/dev/null
}
settings="${repo_root}/.claude/settings.json"
wired "${settings}" PostToolUse Bash issue-capture.sh && ok "settings.json wires issue-capture.sh to PostToolUse on Bash" ||
	no "settings.json wires issue-capture.sh to PostToolUse on Bash"
wired "${settings}" Stop "" goal-continue.sh 40 && ok "the Stop hook timeout covers the 30s gh cap" ||
	no "the Stop hook timeout covers the 30s gh cap (needs >= 40s in settings.json)"
wired "${repo_root}/.muse/hooks.json" PostToolUse bash .muse/hooks/issue-capture.sh &&
	ok ".muse/hooks.json routes PostToolUse bash to the wrapper" || no ".muse/hooks.json routes PostToolUse bash to the wrapper"
# The check must catch a wrong wiring, or it only ever runs against a correct
# file: the same entry under the wrong matcher has to fail.
python3 -c '
import json, sys
cfg = json.load(open(sys.argv[1]))
for g in cfg["hooks"]["PostToolUse"]:
    if any("issue-capture.sh" in h.get("command", "") for h in g.get("hooks", [])):
        g["matcher"] = "Edit"
json.dump(cfg, open(sys.argv[2], "w"))
' "${settings}" "${work}/bad-settings.json"
! wired "${work}/bad-settings.json" PostToolUse Bash issue-capture.sh && ok "the wiring check fails an entry under the wrong matcher" ||
	no "the wiring check fails an entry under the wrong matcher"
rg -Fq 'scripts/test-issue-capture-hook.sh' "${repo_root}/.github/workflows/verify-agent-hygiene.yml" &&
	ok "verify-agent-hygiene.yml runs this suite" || no "verify-agent-hygiene.yml runs this suite"
rg -Fq 'bash scripts/test-issue-capture-hook.sh' "${repo_root}/specs/ci-gates.d/gates-02.yaml" &&
	ok "the agent-canon registry entry chains this suite" || no "the agent-canon registry entry chains this suite"

# ── companion: Stop gate, DEFER, budget, refresher ──────────────────────────
# shellcheck source=scripts/test-issue-capture-hook-cases.sh
. "${repo_root}/scripts/test-issue-capture-hook-cases.sh"
[[ "${issue_capture_cases_loaded:-0}" == 1 ]] || no "the companion cases file ran to its end"

printf '\nissue-capture hook mirror: %s passed, %s failed\n' "${passed}" "${failed}"
[[ "${failed}" -eq 0 ]]
