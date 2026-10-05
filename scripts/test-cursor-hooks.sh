#!/usr/bin/env bash
# Behavioural mirror for .cursor/hooks.json -- the native Cursor port of the
# Claude hooks.
#
# Cursor runs every wired hook through one adapter, scripts/cursor-hook.py,
# which turns the Cursor payload into the Claude shape, runs the same file
# under .claude/hooks/, and turns the Claude answer back into Cursor JSON. So
# one logic copy serves both harnesses. These cases feed Cursor-shaped
# payloads (field names from https://cursor.com/docs/hooks) through the
# adapter and assert the verdict and that stdout is exactly one JSON object,
# because Cursor treats invalid JSON from a permission hook as a block.
#
# Fails while the adapter or hooks.json is missing (RED); the port turns it
# green. Probe hygiene from agent-hooks.md applies: run-unique ids, a private
# TMPDIR for the nudge counters, marker cleanup on entry and exit, and a
# scratch project dir so the doc-staleness hook never scans the real tree.
#
# CURSOR_HOOK_ADAPTER overrides the adapter path, so a seeded mutant can be
# run through this same suite to prove each case checks the verdict.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ADAPTER="${CURSOR_HOOK_ADAPTER:-${repo_root}/scripts/cursor-hook.py}"
CH="${repo_root}/.claude/hooks"
HOOKS_JSON="${repo_root}/.cursor/hooks.json"

work="$(mktemp -d)"
sid="cursorport$$"
sid12="$(printf '%s' "${sid}" | cut -c1-12)"
cleanup() {
  rm -f "/tmp/claude-skill-loaded-${sid12}-"* "/tmp/claude-skill-override-${sid12}"
  rm -rf "${work}"
}
trap cleanup EXIT
cleanup_markers() { rm -f "/tmp/claude-skill-loaded-${sid12}-"* "/tmp/claude-skill-override-${sid12}"; }
cleanup_markers
mkdir -p "${work}/proj/.claude" "${work}/proj/scripts" "${work}/tmp" "${work}/stub"
export TMPDIR="${work}/tmp"
unset CLAUDE_GOAL_FILE CLAUDE_GOAL_OFF CLAUDE_GOAL_MAX_NUDGES CLAUDE_PROJECT_DIR
export CURSOR_PROJECT_DIR="${repo_root}"

passed=0
failed=0
skipped=0
ok() { printf 'ok - %s\n' "$1"; passed=$((passed + 1)); }
no() { printf 'not ok - %s\n' "$1"; failed=$((failed + 1)); }
check() { if eval "$2"; then ok "$1"; else no "$1 (rc=${rc:-} out=${out:-} err=${err:-})"; fi; }

# ── Cursor envelopes (common keys from the Cursor hooks docs) ──────────────────

gen="gen1"
base() { # event [workspace_root]
  printf '"conversation_id":"%s","generation_id":"%s","model":"m","hook_event_name":"%s","cursor_version":"1.7.0","workspace_roots":["%s"],"user_email":null,"transcript_path":null' \
    "${sid}" "${gen}" "$1" "${2:-${repo_root}}"
}
shell_in() { # command [cwd]
  printf '{%s,"command":"%s","cwd":"%s","sandbox":false}' "$(base beforeShellExecution)" "$1" "${2:-${repo_root}}"
}
pretool_in() { # tool_name tool_input_json [cwd]
  printf '{%s,"tool_name":"%s","tool_input":%s,"tool_use_id":"u1","cwd":"%s"}' \
    "$(base preToolUse)" "$1" "$2" "${3:-${repo_root}}"
}
posttool_in() { # tool_name tool_input_json
  printf '{%s,"tool_name":"%s","tool_input":%s,"tool_output":"ok","tool_use_id":"u1","cwd":"%s"}' \
    "$(base postToolUse)" "$1" "$2" "${repo_root}"
}
edit_in() { # file_path
  printf '{%s,"file_path":"%s","edits":[{"old_string":"a","new_string":"b"}]}' "$(base afterFileEdit "${work}/proj")" "$1"
}
compact_in() { printf '{%s,"trigger":"auto","context_usage_percent":91}' "$(base preCompact)"; }
prompt_in() { # prompt (no quotes or backslashes)
  printf '{%s,"prompt":"%s","attachments":[]}' "$(base beforeSubmitPrompt "${work}/proj")" "$1"
}
stop_in() { # status loop_count
  printf '{%s,"status":"%s","loop_count":%s}' "$(base stop "${work}/proj")" "$1" "$2"
}

# run <event> <script> <payload>: sets out, err, rc.
run() {
  out="$(printf '%s' "$3" | python3 "${ADAPTER}" "$1" "$2" 2>"${work}/err")"
  rc=$?
  err="$(cat "${work}/err")"
}
# one_json: stdout is exactly one JSON object.
one_json() { printf '%s' "${out}" | python3 -c 'import json,sys; d=json.loads(sys.stdin.read()); sys.exit(0 if isinstance(d, dict) else 1)' 2>/dev/null; }
# jget <key>: a top-level string or JSON value from stdout, "" when absent.
jget() {
  printf '%s' "${out}" | python3 -c '
import json, sys
try:
    v = json.loads(sys.stdin.read()).get(sys.argv[1])
except Exception:
    v = None
print("" if v is None else (v if isinstance(v, str) else json.dumps(v)))' "$1" 2>/dev/null
}
# is_json <expected>: stdout parses to exactly this JSON value.
is_json() { printf '%s' "${out}" | python3 -c 'import json,sys; sys.exit(0 if json.loads(sys.stdin.read()) == json.loads(sys.argv[1]) else 1)' "$1" 2>/dev/null; }
jkeys() { printf '%s' "${out}" | python3 -c 'import json,sys; print(",".join(sorted(json.loads(sys.stdin.read()))))' 2>/dev/null; }

# ── A. wiring: .cursor/hooks.json ────────────────────────────────────────────

wiring="$(python3 - "${HOOKS_JSON}" <<'PY' 2>/dev/null
import json, sys
d = json.load(open(sys.argv[1]))
print("version=%s" % d.get("version"))
for event, entries in d["hooks"].items():
    for e in entries:
        print("%s|%s|%s|%s" % (event, e.get("matcher", ""), e.get("failClosed", False), e["command"]))
PY
)"
if [[ -n "${wiring}" ]]; then ok ".cursor/hooks.json is valid JSON"; else no ".cursor/hooks.json is valid JSON"; fi
printf '%s\n' "${wiring}" | rg -qx 'version=1' && ok "hooks.json declares version 1" || no "hooks.json declares version 1"

# event|matcher|failClosed|script -- the full triple each hook must carry.
for want in \
  'beforeShellExecution||True|.claude/hooks/guard-live-gate.sh' \
  'preToolUse|Write|True|.claude/hooks/skill-nudge.sh' \
  'postToolUse|Read|False|.claude/hooks/skill-loaded.sh' \
  'afterFileEdit||False|.claude/hooks/eshu-doc-staleness.sh' \
  'preCompact||False|.claude/hooks/on-compact.sh' \
  'beforeSubmitPrompt||False|.claude/hooks/goal-refresh.sh' \
  'stop||False|.claude/hooks/goal-continue.sh'
do
  IFS='|' read -r ev m fc script <<<"${want}"
  line="${ev}|${m}|${fc}|python3 scripts/cursor-hook.py ${ev} ${script}"
  if printf '%s\n' "${wiring}" | rg -qxF "${line}"; then ok "wired: ${ev} ${m:-*} failClosed=${fc} -> ${script##*/}"; else no "wired: ${line}"; fi
  [[ -f "${repo_root}/${script}" ]] || no "wired script exists: ${script}"
done
if printf '%s\n' "${wiring}" | rg -q '[$`;&]'; then
  no "hook commands are plain argv (no shell expansion, so they run with or without a shell)"
else
  ok "hook commands are plain argv (no shell expansion, so they run with or without a shell)"
fi
[[ -f "${ADAPTER}" ]] && ok "adapter exists" || no "adapter exists: ${ADAPTER}"
if [[ ! -f "${ADAPTER}" ]]; then
  printf '\nadapter missing -- envelope cases cannot run\n'
  printf '\ncursor hooks mirror: %s passed, %s failed\n' "${passed}" "${failed}"
  exit 1
fi

# ── B. guard-live-gate: same verdict as under Claude ──────────────────────────

run beforeShellExecution .claude/hooks/guard-live-gate.sh "$(shell_in 'ls -la')"
check "clean shell command is allowed" '[[ ${rc} -eq 0 && "$(jget permission)" == allow ]]'
check "clean shell allow is exactly one JSON object" 'one_json && [[ "$(jkeys)" == permission ]]'

run beforeShellExecution .claude/hooks/guard-live-gate.sh "$(shell_in 'CLAUDE_HOOK_ALLOW=1 make pre-pr')"
check "per-call CLAUDE_HOOK_ALLOW=1 override still waives the guard" '[[ ${rc} -eq 0 && "$(jget permission)" == allow ]]'

run beforeShellExecution .claude/hooks/guard-live-gate.sh "$(shell_in 'make pre-pr' "${work}")"
check "guard stays silent outside an Eshu checkout" '[[ "$(jget permission)" == allow ]]'

# Seed the busy state the way test-agent-hooks.sh does: `sleep` under the name
# ci-gates satisfies `pgrep -x ci-gates` without running anything.
fakebin="${work}/fakebin"
mkdir -p "${fakebin}"
ln -s "$(command -v sleep)" "${fakebin}/ci-gates"
"${fakebin}/ci-gates" 30 &
fake_pid=$!
sleep 0.3
c_out="$(printf '{"cwd":"%s","tool_name":"Bash","tool_input":{"command":"make pre-pr"}}' "${repo_root}" \
  | bash "${CH}/guard-live-gate.sh" 2>&1)"
c_rc=$?
run beforeShellExecution .claude/hooks/guard-live-gate.sh "$(shell_in 'make pre-pr')"
kill "${fake_pid}" 2>/dev/null
wait "${fake_pid}" 2>/dev/null
[[ ${c_rc} -eq 2 ]] && ok "control: the Claude hook itself refuses make pre-pr while ci-gates runs" \
  || no "control: Claude hook should exit 2 (rc=${c_rc} out=${c_out})"
check "Cursor payload is denied while ci-gates runs" '[[ ${rc} -eq 0 && "$(jget permission)" == deny ]] && one_json'
check "deny carries the Claude BLOCKED text to the user" '[[ "$(jget user_message)" == *"ci-gates run is already in flight"* ]]'
check "deny carries the same text to the agent" '[[ "$(jget agent_message)" == "${c_out}"* ]]'

# ── C. fail closed for guards, fail open for advisory hooks ───────────────────

run beforeShellExecution .claude/hooks/no-such-hook.sh "$(shell_in 'ls')"
check "guard: a missing hook script denies" '[[ ${rc} -eq 0 && "$(jget permission)" == deny ]] && one_json'
printf '#!/bin/bash\ncat >/dev/null\necho boom >&2\nexit 1\n' >"${work}/stub/crash.sh"
run preToolUse "${work}/stub/crash.sh" "$(pretool_in Write '{"file_path":"'"${repo_root}"'/README.md"}')"
check "guard: a hook exiting 1 denies, with its stderr" '[[ "$(jget permission)" == deny && "$(jget user_message)" == *boom* ]]'
run beforeShellExecution .claude/hooks/guard-live-gate.sh 'not json'
check "guard: an unreadable payload denies" '[[ ${rc} -eq 0 && "$(jget permission)" == deny ]] && one_json'
run beforeShellExecution .claude/hooks/guard-live-gate.sh ''
check "guard: an empty payload denies" '[[ ${rc} -eq 0 && "$(jget permission)" == deny ]] && one_json'
run preToolUse .claude/hooks/skill-nudge.sh $'  \n\t '
check "guard: a whitespace-only payload denies" '[[ ${rc} -eq 0 && "$(jget permission)" == deny ]] && one_json'
run afterFileEdit .claude/hooks/eshu-doc-staleness.sh ''
check "advisory: an empty payload fails open with {}" '[[ ${rc} -eq 0 && "${out}" == "{}" ]]'
run beforeSubmitPrompt .claude/hooks/goal-refresh.sh ''
check "an empty prompt payload still lets the prompt through" '[[ ${rc} -eq 0 ]] && is_json "{\"continue\": true}"'
printf '#!/bin/bash\ncat >/dev/null\nhead -c 30000 /dev/zero | tr "\\0" x >&2\nexit 2\n' >"${work}/stub/loud.sh"
run beforeShellExecution "${work}/stub/loud.sh" "$(shell_in 'ls')"
um="$(jget user_message)"
check "a deny built from child output is clipped at 20000 characters" '[[ "$(jget permission)" == deny && ${#um} -lt 20100 && "${um}" == *"truncated 10000 characters"* ]] && one_json'
run stop "${work}/stub/crash.sh" "$(stop_in completed 0)"
check "advisory: a crashing hook fails open with {} and a stderr note" '[[ ${rc} -eq 0 && "${out}" == "{}" && "${err}" == *boom* ]]'
run afterFileEdit .claude/hooks/no-such-hook.sh "$(edit_in "${repo_root}/README.md")"
check "advisory: a missing hook script fails open" '[[ ${rc} -eq 0 && "${out}" == "{}" ]]'
run no-such-event .claude/hooks/guard-live-gate.sh "$(shell_in 'ls')"
check "an unknown event name fails closed" '[[ "$(jget permission)" == deny ]]'

# ── D. skill-nudge on preToolUse Write ────────────────────────────────────────
#
# Cursor does not document the path key inside tool_input for Write, so the
# adapter reads the first present of these. Each one must reach the guard.

governed="${repo_root}/go/internal/cursorprobe$$/probe.go"
for key in file_path path target_file filePath file; do
  run preToolUse .claude/hooks/skill-nudge.sh "$(pretool_in Write '{"'"${key}"'":"'"${governed}"'","contents":"x"}')"
  check "Write path at tool_input.${key} reaches skill-nudge and is denied" '[[ ${rc} -eq 0 && "$(jget permission)" == deny ]] && one_json'
done
check "deny names the missing skill" '[[ "$(jget user_message)" == *golang-engineering* ]]'
check "deny tells a Cursor agent how to load a skill" '[[ "$(jget agent_message)" == *SKILL.md* ]]'
check "deny names the project's own skill path (not a guess at a checkout)" '[[ "$(jget agent_message)" == *"'"${repo_root}"'/.agents/skills/<id>/SKILL.md"* ]]'
run preToolUse .claude/hooks/skill-nudge.sh "$(pretool_in Write '{"path":"go/internal/cursorprobe/rel.go"}' "${repo_root}")"
check "a relative path resolves against cwd and is denied" '[[ "$(jget permission)" == deny ]]'
run preToolUse .claude/hooks/skill-nudge.sh \
  '{'"$(base preToolUse)"',"tool_name":"Write","tool_input":{"contents":"x"},"file_path":"'"${governed}"'","cwd":"'"${repo_root}"'"}'
check "top-level payload file_path is the last fallback and is denied" '[[ "$(jget permission)" == deny ]]'
run preToolUse .claude/hooks/skill-nudge.sh "$(pretool_in Write '{"contents":"x"}')"
check "no path field: allowed, not blocked on a guess" '[[ "$(jget permission)" == allow ]] && one_json'
check "no path field: the adapter says so on stderr" '[[ "${err}" == *"no file path"* ]]'
run preToolUse .claude/hooks/skill-nudge.sh "$(pretool_in Write '{"file_path":123,"content":"x"}')"
check "a path key with a non-string value denies" '[[ ${rc} -eq 0 && "$(jget permission)" == deny ]] && one_json'
run preToolUse .claude/hooks/skill-nudge.sh "$(pretool_in Write '{"path":["a.go"],"content":"x"}')"
check "a fallback path key with a non-string value denies too" '[[ "$(jget permission)" == deny ]]'
run preToolUse .claude/hooks/skill-nudge.sh "$(pretool_in Write '{"file_path":"'"${work}"'/notes.txt"}')"
check "edits outside an Eshu checkout pass" '[[ "$(jget permission)" == allow ]]'

# ── E. skill-loaded bridge: postToolUse Read of a SKILL.md ────────────────────
#
# Cursor has no Skill tool. Its agent loads a skill by reading SKILL.md, so the
# adapter records that read the way skill-loaded.sh records a Skill call.

cleanup_markers
run postToolUse .claude/hooks/skill-loaded.sh "$(posttool_in Read '{"path":"'"${repo_root}"'/README.md"}')"
check "a plain Read records no skill" '[[ ${rc} -eq 0 && "${out}" == "{}" ]] && ! ls /tmp/claude-skill-loaded-'"${sid12}"'-* >/dev/null 2>&1'
run preToolUse .claude/hooks/skill-nudge.sh "$(pretool_in Write '{"file_path":"'"${governed}"'"}')"
check "before the skill read, the governed Write is denied" '[[ "$(jget permission)" == deny ]]'
run postToolUse .claude/hooks/skill-loaded.sh \
  "$(posttool_in Read '{"target_file":"'"${repo_root}"'/.agents/skills/golang-engineering/SKILL.md"}')"
check "reading .agents/skills/<id>/SKILL.md records the skill" '[[ ${rc} -eq 0 && -f /tmp/claude-skill-loaded-'"${sid12}"'-golang-engineering ]]'
run preToolUse .claude/hooks/skill-nudge.sh "$(pretool_in Write '{"file_path":"'"${governed}"'"}')"
check "after the skill read, the same Write is allowed" '[[ "$(jget permission)" == allow ]] && one_json'
cleanup_markers
run postToolUse .claude/hooks/skill-loaded.sh "$(posttool_in Read '{"path":".claude/skills/eshu-postgres-rigor/SKILL.md"}')"
check "a relative .claude/skills SKILL.md read is recorded too" '[[ -f /tmp/claude-skill-loaded-'"${sid12}"'-eshu-postgres-rigor ]]'
cleanup_markers

# Only the project's own skills count. Each read below has the right shape but
# must record nothing.
mkdir -p "${work}/other/.codex/skills/golang-engineering" "${work}/outside/skill"
printf 'x\n' >"${work}/other/.codex/skills/golang-engineering/SKILL.md"
printf 'x\n' >"${work}/outside/skill/SKILL.md"
no_marker() { # label path [project]
  cleanup_markers
  CURSOR_PROJECT_DIR="${3:-${repo_root}}" run postToolUse .claude/hooks/skill-loaded.sh "$(posttool_in Read '{"file_path":"'"$2"'"}')"
  check "skill bridge ignores $1" '[[ ${rc} -eq 0 && "${out}" == "{}" ]] && ! ls /tmp/claude-skill-loaded-'"${sid12}"'-* >/dev/null 2>&1'
}
no_marker "a /etc/.agents/skills path" "/etc/.agents/skills/golang-engineering/SKILL.md"
no_marker "a home-dir skills path" "${HOME}/.claude/skills/golang-engineering/SKILL.md"
no_marker "another repo's skills path" "${work}/other/.codex/skills/golang-engineering/SKILL.md"
no_marker "a ../ traversal out of the skills dir" "${repo_root}/.agents/skills/golang-engineering/../../../docs/.agents/skills/golang-engineering/SKILL.md"
no_marker "an unknown skill id" "${repo_root}/.agents/skills/no-such-skill/SKILL.md"
mkdir -p "${work}/fakeproj/.agents/skills/golang-engineering" "${work}/fakeproj/.agents/skills/eshu-postgres-rigor"
printf 'x\n' >"${work}/fakeproj/.agents/skills/golang-engineering/SKILL.md"
ln -s "${work}/outside/skill/SKILL.md" "${work}/fakeproj/.agents/skills/eshu-postgres-rigor/SKILL.md"
no_marker "a SKILL.md symlinked out of the project" "${work}/fakeproj/.agents/skills/eshu-postgres-rigor/SKILL.md" "${work}/fakeproj"
cleanup_markers
CURSOR_PROJECT_DIR="${work}/fakeproj" run postToolUse .claude/hooks/skill-loaded.sh \
  "$(posttool_in Read '{"file_path":"'"${work}"'/fakeproj/.agents/skills/golang-engineering/SKILL.md"}')"
check "control: a real skill in that same project is recorded" '[[ -f /tmp/claude-skill-loaded-'"${sid12}"'-golang-engineering ]]'
cleanup_markers

# ── F. afterFileEdit: doc-staleness runs against the project dir ──────────────
#
# CURSOR_PROJECT_DIR points at a scratch project whose check-docs-stale.sh is a
# stub, so this proves the adapter hands the project dir to the hook as
# CLAUDE_PROJECT_DIR without scanning the real tree.

printf '#!/bin/bash\nprintf "%%s %%s\\n" "$ESHU_DOC_KEEPER_TOOL" "$*" >"%s/stale-ran"\n' "${work}" >"${work}/proj/scripts/check-docs-stale.sh"
chmod +x "${work}/proj/scripts/check-docs-stale.sh"
CURSOR_PROJECT_DIR="${work}/proj" run afterFileEdit .claude/hooks/eshu-doc-staleness.sh "$(edit_in "${repo_root}/go/x.go")"
check "afterFileEdit runs the staleness scan with --all" '[[ ${rc} -eq 0 && "$(cat "${work}/stale-ran" 2>/dev/null)" == "claude-code --all" ]]'
check "afterFileEdit prints {}" '[[ "${out}" == "{}" ]]'

# ── G. preCompact: markers cleared, compaction note shown ─────────────────────

touch "/tmp/claude-skill-loaded-${sid12}-golang-engineering"
run preCompact .claude/hooks/on-compact.sh "$(compact_in)"
check "preCompact clears this conversation's skill markers" '[[ ! -f /tmp/claude-skill-loaded-'"${sid12}"'-golang-engineering ]]'
check "preCompact shows the re-grounding note" '[[ ${rc} -eq 0 && "$(jget user_message)" == *eshu-session-lifecycle* ]] && one_json'
check "the compaction note tells Cursor to read the SKILL.md" '[[ "$(jget user_message)" == *"'"${repo_root}"'/.agents/skills/<id>/SKILL.md"* ]]'

# ── H. goal round trip: beforeSubmitPrompt writes, stop enforces ──────────────

goal="${work}/proj/.claude/active-goal.${sid}"
run beforeSubmitPrompt .claude/hooks/goal-refresh.sh "$(prompt_in 'GOAL: Finish the cursor port.')"
check "GOAL: prompt writes the per-conversation goal file" '[[ -f "${goal}" ]] && rg -q "Finish the cursor port" "${goal}"'
check "setting a goal answers exactly {\"continue\": true}" '[[ ${rc} -eq 0 ]] && is_json "{\"continue\": true}"'
rm -f "${goal}"
run beforeSubmitPrompt .claude/hooks/goal-refresh.sh "$(prompt_in 'GOAL: Finish the cursor port.')"
run beforeSubmitPrompt .claude/hooks/goal-refresh.sh "$(prompt_in 'keep going')"
check "with an active goal the prompt still answers exactly {\"continue\": true}" '[[ ${rc} -eq 0 ]] && is_json "{\"continue\": true}"'
check "the goal file survives an ordinary prompt" '[[ -f "${goal}" ]] && rg -q "Finish the cursor port" "${goal}"'

gen="turn-a"
run stop .claude/hooks/goal-continue.sh "$(stop_in completed 0)"
check "stop with an open goal hands it back as a follow-up" '[[ ${rc} -eq 0 && "$(jget followup_message)" == *"Finish the cursor port"* ]] && one_json'
check "the budget counter is keyed on generation_id" 'ls "${work}/tmp"/claude-goal-nudge-*-turn-a >/dev/null 2>&1'
run stop .claude/hooks/goal-continue.sh "$(stop_in aborted 0)"
check "a stop the user aborted is never continued" '[[ "${out}" == "{}" ]]'
run stop .claude/hooks/goal-continue.sh "$(stop_in '' 0)"
check "a stop with an empty status is not continued" '[[ "${out}" == "{}" ]]'
run stop .claude/hooks/goal-continue.sh '{'"$(base stop "${work}/proj")"',"loop_count":0}'
check "a stop with no status is not continued" '[[ "${out}" == "{}" ]]'

c_out="$(printf '{"session_id":"%s","prompt_id":"par","stop_hook_active":false,"cwd":"%s","transcript_path":null,"hook_event_name":"Stop"}' \
  "${sid}" "${work}/proj" | bash "${CH}/goal-continue.sh" 2>/dev/null)"
[[ "${c_out}" == *'"decision": "block"'* ]] && ok "parity: the Claude hook blocks on the same goal" || no "parity: Claude hook should block (out=${c_out})"

export CLAUDE_GOAL_MAX_NUDGES=1
gen="turn-b"
run stop .claude/hooks/goal-continue.sh "$(stop_in completed 0)"
check "budget: first stop of a generation continues" '[[ -n "$(jget followup_message)" ]]'
run stop .claude/hooks/goal-continue.sh "$(stop_in completed 1)"
check "budget: a spent generation is released" '[[ ${rc} -eq 0 && "${out}" == "{}" ]]'
unset CLAUDE_GOAL_MAX_NUDGES

printf 'SESSION: %s\nDONE\n' "${sid}" >"${goal}"
gen="turn-c"
run stop .claude/hooks/goal-continue.sh "$(stop_in completed 0)"
check "a DONE goal lets the stop through" '[[ ${rc} -eq 0 && "${out}" == "{}" ]]'

printf '\ncursor hooks mirror: %s passed, %s failed, %s skipped\n' "${passed}" "${failed}" "${skipped}"
[[ "${failed}" -eq 0 ]]
