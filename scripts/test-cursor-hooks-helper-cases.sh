#!/usr/bin/env bash
# Sourced by scripts/test-cursor-hooks.sh -- not run on its own.
#
# The helper-agent, worktree and payload-log cases for the Cursor skill bridge
# (scripts/cursor_hook_family.py). They live here because the mirror reached
# the repo's 500-line cap. Sourced, not executed, so the helpers (run, check,
# ok/no, jget) and the single pass/fail tally stay shared. This file is a
# trigger path for the agent-canon gate, so editing it alone selects the gate.
# The sentinel on the last line lets the parent notice if it stops loading.

# ── I. helper agents share the parent's skill markers ─────────────────────────
#
# Claude gives a subagent its parent's session_id. Under Cursor, subagentStart
# links the helper to its parent and every event follows the links to the
# root. Which id a helper's own tool hooks carry is not known, so each case
# carries the helper id in a different field, with the other ids unlinked.

ids() { # conversation_id [session_id] [subagent_id]
  local s='"conversation_id":"'"$1"'"'
  [[ -n "${2:-}" ]] && s+=',"session_id":"'"$2"'"'
  [[ -n "${3:-}" ]] && s+=',"subagent_id":"'"$3"'"'
  printf '%s' "${s}"
}
fam_in() { # event ids_fragment rest
  printf '{%s,"generation_id":"g","hook_event_name":"%s","workspace_roots":["%s"],"transcript_path":null,"cwd":"%s",%s}' \
    "$2" "$1" "${repo_root}" "${repo_root}" "$3"
}
start_in() { # conversation_id subagent_id parent_conversation_id
  printf '{"conversation_id":"%s","subagent_id":"%s","parent_conversation_id":"%s","subagent_type":"generalPurpose","task":"LOGSECRET task","tool_call_id":"tc","is_parallel_worker":false,"hook_event_name":"subagentStart","cwd":"%s"}' \
    "$1" "$2" "$3" "${repo_root}"
}
run_start() { out="$(printf '%s' "$1" | python3 "${ADAPTER}" subagentStart 2>"${work}/err")"; rc=$?; err="$(cat "${work}/err")"; }
skill_md="${repo_root}/.agents/skills/golang-engineering/SKILL.md"
read_as() { run postToolUse .claude/hooks/skill-loaded.sh "$(fam_in postToolUse "$1" '"tool_name":"Read","tool_input":{"file_path":"'"${skill_md}"'"},"tool_output":"ok"')"; }
write_as() { run preToolUse .claude/hooks/skill-nudge.sh "$(fam_in preToolUse "$1" '"tool_name":"Write","tool_input":{"file_path":"'"${governed}"'"}')"; }
allowed() { [[ ${rc} -eq 0 && "$(jget permission)" == allow ]]; }
denied() { [[ ${rc} -eq 0 && "$(jget permission)" == deny ]]; }
pair() { cleanup_markers; read_as "$1"; write_as "$2"; check "$3" "$4"; } # read_ids write_ids label verdict

cleanup_markers
cleanup_links
run_start "$(start_in "${sid}" "${H}" "${sid}")"
check "subagentStart answers exactly {\"permission\": \"allow\"}" '[[ ${rc} -eq 0 ]] && is_json "{\"permission\": \"allow\"}"'
for mode in conversation_id session_id subagent_id; do
  case "${mode}" in
    conversation_id) hid="$(ids "${H}")" ;;
    session_id) hid="$(ids "${F}" "${H}")" ;;
    subagent_id) hid="$(ids "${F}" "${F}" "${H}")" ;;
  esac
  pair "${hid}" "$(ids "${sid}")" "helper id as ${mode}: the helper's SKILL.md read unlocks the parent's edit" allowed
  pair "$(ids "${sid}")" "${hid}" "helper id as ${mode}: the parent's SKILL.md read unlocks the helper's edit" allowed
done
run_start "$(start_in "${C}" "" "${sid}")"
pair "$(ids "${C}")" "$(ids "${sid}")" "a start whose conversation_id differs from the parent links that id too" allowed
pair "$(ids "${S}" "${sid}")" "$(ids "${sid}")" "a helper with a fresh conversation_id but the parent's session_id shares markers" allowed
# A start whose session_id differs from parent_conversation_id: that session id
# must be linked too, so a helper event carrying it (with a fresh
# conversation_id) joins the family. Cursor keeps session ids per conversation,
# so the link cannot reach an unrelated chat.
PS="pss-r$$" F2="fr2-r$$"
printf '%s' "$(start_in "${sid}" "${H}" "${sid}" | sed 's/"subagentStart",/"subagentStart","session_id":"'"${PS}"'",/')" >"${work}/start-ps.json"
run_start "$(cat "${work}/start-ps.json")"
pair "$(ids "${F2}" "${PS}")" "$(ids "${sid}")" "a start's own session_id (distinct from the parent conversation) is linked: the helper's read unlocks the parent's edit" allowed
pair "$(ids "${sid}")" "$(ids "${F2}" "${PS}")" "a start's own session_id is linked: the parent's read unlocks the helper's edit" allowed
pair "$(ids "${U}")" "$(ids "${sid}")" "an unlinked conversation's read does not unlock the parent's edit" denied
pair "$(ids "${sid}")" "$(ids "${U}")" "the parent's read does not unlock an unlinked conversation's edit" denied

# Only the skill events share the family key. Claude fires no Stop,
# UserPromptSubmit or SessionStart for a subagent, so a helper must never see,
# retire or compact away its parent's goal or markers.
goal_in() { # event ids_fragment rest (cwd is the scratch project)
  printf '{%s,"generation_id":"%s","hook_event_name":"%s","workspace_roots":["%s"],"transcript_path":null,"cwd":"%s",%s}' \
    "$2" "${gen}" "$1" "${work}/proj" "${work}/proj" "$3"
}
gen="gh-helper"
pgoal="${work}/proj/.claude/active-goal.${sid}"
rm -f "${work}/proj/.claude/active-goal."*
run beforeSubmitPrompt .claude/hooks/goal-refresh.sh "$(goal_in beforeSubmitPrompt "$(ids "${sid}")" '"prompt":"GOAL: parent objective"')"
check "fixture: the parent's goal is set" '[[ -f "${pgoal}" ]] && rg -q "parent objective" "${pgoal}"'
run stop .claude/hooks/goal-continue.sh "$(goal_in stop "$(ids "${H}")" '"status":"completed","loop_count":0')"
check "a helper's completed stop does not pick up the parent's open goal" '[[ ${rc} -eq 0 && "${out}" == "{}" ]]'
run beforeSubmitPrompt .claude/hooks/goal-refresh.sh "$(goal_in beforeSubmitPrompt "$(ids "${H}")" '"prompt":"/goal done"')"
check "a helper's /goal done leaves the parent's goal active" '[[ "$(head -1 "${pgoal}")" != DONE* ]] && rg -q "parent objective" "${pgoal}"'
gen="gh-parent"
run stop .claude/hooks/goal-continue.sh "$(goal_in stop "$(ids "${sid}")" '"status":"completed","loop_count":0')"
check "control: the parent's own stop still hands its goal back" '[[ "$(jget followup_message)" == *"parent objective"* ]]'
rm -f "${work}/proj/.claude/active-goal."*
touch "/tmp/claude-skill-loaded-${sid12}-golang-engineering"
run preCompact .claude/hooks/on-compact.sh "$(fam_in preCompact "$(ids "${H}")" '"trigger":"auto","context_usage_percent":91')"
check "a helper's compaction leaves the parent's skill markers" '[[ ${rc} -eq 0 && -f "/tmp/claude-skill-loaded-${sid12}-golang-engineering" ]]'

# Cycles and long chains end safely: the id is used unlinked (the nudge keeps
# blocking) rather than looping or picking an arbitrary member.
run_start "$(start_in "${A}" "${A}" "${B}")"
run_start "$(start_in "${B}" "${B}" "${A}")"
check "a link that would close a cycle is refused, and the start still allows" \
  '[[ -f /tmp/eshu-cursor-link-${A} && ! -e /tmp/eshu-cursor-link-${B} ]] && is_json "{\"permission\": \"allow\"}"'
printf '%s' "${A}" >"/tmp/eshu-cursor-link-${B}"
cleanup_markers
read_as "$(ids "${A}")"
check "a planted link cycle ends safely: the read records under its own id" \
  '[[ ${rc} -eq 0 && "${out}" == "{}" && -f "/tmp/claude-skill-loaded-${A:0:12}-golang-engineering" ]]'
# The depth bound: a chain of exactly 16 links resolves; 17 is too deep.
for i in $(seq 0 15); do printf 'e%s-r%s' "$((i + 1))" "$$" >"/tmp/eshu-cursor-link-e${i}-r$$"; done
for i in $(seq 0 16); do printf 'f%s-r%s' "$((i + 1))" "$$" >"/tmp/eshu-cursor-link-f${i}-r$$"; done
cleanup_markers
read_as "$(ids "e0-r$$")"
check "a chain of exactly 16 links resolves to its root" '[[ -f "/tmp/claude-skill-loaded-e16-r$$-golang-engineering" ]]'
cleanup_markers
read_as "$(ids "f0-r$$")"
check "a chain of 17 links is too deep: the read records under its own id" \
  '[[ ${rc} -eq 0 && -f "/tmp/claude-skill-loaded-f0-r$$-golang-engineering" && ! -f "/tmp/claude-skill-loaded-f17-r$$-golang-engineering" ]]'
run_start "$(start_in "${GDP}" "${GDP}" "f0-r$$")"
check "a start under a too-deep parent chain says so, not that it would loop" \
  '[[ "${err}" == *"too deep"* && "${err}" != *loop* && ! -e "/tmp/eshu-cursor-link-${GDP}" ]] && is_json "{\"permission\": \"allow\"}"'

# A bad link or root file means unlinked: never a crash, a hang or a follow.
printf '\377\376' >"/tmp/eshu-cursor-link-${GU8}"
pair "$(ids "${GU8}")" "$(ids "${GU8}")" "a non-UTF-8 link file is ignored: the chat's own read unlocks its edit" allowed
mkfifo "/tmp/eshu-cursor-link-${GFI}"
cleanup_markers
out="$(printf '%s' "$(fam_in postToolUse "$(ids "${GFI}")" '"tool_name":"Read","tool_input":{"file_path":"'"${skill_md}"'"}')" \
  | perl -e 'alarm 10; exec @ARGV' python3 "${ADAPTER}" postToolUse .claude/hooks/skill-loaded.sh 2>/dev/null)"
rc=$?
check "a FIFO link file is ignored without hanging" '[[ ${rc} -eq 0 && -f "/tmp/claude-skill-loaded-${GFI:0:12}-golang-engineering" ]]'
printf '%s' "${sid}" >"${work}/sl-target"
ln -s "${work}/sl-target" "/tmp/eshu-cursor-link-${GSL}"
pair "$(ids "${GSL}")" "$(ids "${sid}")" "a symlinked link file is not followed" denied
ln -s "${work}/sl-target" "/tmp/eshu-cursor-root-${GRT}"
pair "$(ids "${GRS}" "${GRT}")" "$(ids "${GRT}")" "a symlinked root marker is not trusted" denied

# The family logic is a sibling module. Without it the adapter must still
# print one JSON object: closed for a guard, open for an advisory event.
mkdir -p "${work}/lonely/scripts"
cp "${ADAPTER}" "${work}/lonely/scripts/cursor-hook.py"
out="$(shell_in ls | python3 "${work}/lonely/scripts/cursor-hook.py" beforeShellExecution .claude/hooks/guard-live-gate.sh 2>/dev/null)"
check "without its family module the adapter fails closed for a guard" '[[ "$(jget permission)" == deny && "$(jget user_message)" == *cursor_hook_family* ]]'
out="$(edit_in "${repo_root}/README.md" | CURSOR_PROJECT_DIR="${work}/proj" python3 "${work}/lonely/scripts/cursor-hook.py" afterFileEdit .claude/hooks/eshu-doc-staleness.sh 2>/dev/null)"
check "without its family module an advisory event fails open" '[[ "${out}" == "{}" ]]'
# A family module that is present but broken (a syntax error) must be handled
# the same way as a missing one, not crash with a traceback and no answer.
mkdir -p "${work}/broken/scripts"
cp "${ADAPTER}" "${work}/broken/scripts/cursor-hook.py"
cp "$(dirname "${ADAPTER}")/cursor_hook_family.py" "${work}/broken/scripts/cursor_hook_family.py"
printf '\ndef broken(:\n' >>"${work}/broken/scripts/cursor_hook_family.py"
out="$(shell_in ls | python3 "${work}/broken/scripts/cursor-hook.py" beforeShellExecution .claude/hooks/guard-live-gate.sh 2>/dev/null)"
check "a broken family module fails closed for a guard" '[[ "$(jget permission)" == deny && "$(jget user_message)" == *cursor_hook_family* ]]'
out="$(edit_in "${repo_root}/README.md" | CURSOR_PROJECT_DIR="${work}/proj" python3 "${work}/broken/scripts/cursor-hook.py" afterFileEdit .claude/hooks/eshu-doc-staleness.sh 2>/dev/null)"
check "a broken family module fails open for an advisory event" '[[ "${out}" == "{}" ]]'

# A malformed subagentStart never blocks and never writes outside the state dir.
long="$(head -c 300 /dev/zero | tr '\0' a)"
for bad in 'not json' '' '[]' \
  '{"subagent_id":"../esc-r'$$'","parent_conversation_id":"'"${sid}"'"}' \
  '{"subagent_id":"a/esc-r'$$'","parent_conversation_id":"'"${sid}"'"}' \
  '{"subagent_id":"esc-r'$$'..","parent_conversation_id":"'"${sid}"'"}' \
  '{"subagent_id":"esc-r'$$'","parent_conversation_id":"../../etc"}' \
  '{"subagent_id":"esc-r'$$'","parent_conversation_id":"'"${long}"'"}' \
  '{"subagent_id":123,"parent_conversation_id":["x"],"conversation_id":{}}'; do
  run_start "${bad}"
  check "malformed subagentStart is allowed: ${bad:0:40}" '[[ ${rc} -eq 0 ]] && is_json "{\"permission\": \"allow\"}"'
done
check "no malformed start wrote a link" '! ls /tmp/*esc-r$$* >/dev/null 2>&1 && ! ls "${work}"/*esc-r$$* >/dev/null 2>&1'
out="$(printf '{}' | python3 "${ADAPTER}" subagentStart extra-arg 2>/dev/null)"
check "subagentStart with an unexpected argument still allows" 'is_json "{\"permission\": \"allow\"}"'
cleanup_markers
cleanup_links

# ── J. a SKILL.md read in a git worktree of the project counts ────────────────

g() { git -c core.hooksPath=/dev/null -c user.name=t -c user.email=t@example.invalid -c commit.gpgsign=false -c init.defaultBranch=main "$@" >/dev/null 2>&1; }
wt="${work}/wt"
for d in main other; do
  mkdir -p "${wt}/${d}/.agents/skills/golang-engineering"
  printf 'x\n' >"${wt}/${d}/.agents/skills/golang-engineering/SKILL.md"
  g -C "${wt}/${d}" init -q && g -C "${wt}/${d}" add -A && g -C "${wt}/${d}" commit -qm init
done
g -C "${wt}/main" worktree add -q "${wt}/second" -b second
mkdir -p "${wt}/second/.agents/skills/wt-only" "${wt}/second/.claude/skills/claude-only"
printf 'x\n' >"${wt}/second/.agents/skills/wt-only/SKILL.md"
printf 'x\n' >"${wt}/second/.claude/skills/claude-only/SKILL.md"
[[ -f "${wt}/second/.git" ]] && ok "fixture: a real second git worktree exists" || no "fixture: git worktree add failed"
wt_read() { # label yes|no path
  local id="${3%/SKILL.md}"
  id="${id##*/}"
  cleanup_markers
  CURSOR_PROJECT_DIR="${wt}/main" run postToolUse .claude/hooks/skill-loaded.sh "$(posttool_in Read '{"file_path":"'"$3"'"}')"
  if [[ "$2" == yes ]]; then
    check "worktree rule: $1 counts" '[[ ${rc} -eq 0 && -f "/tmp/claude-skill-loaded-${sid12}-${id}" ]]'
  else
    check "worktree rule: $1 does not count" '[[ ${rc} -eq 0 && "${out}" == "{}" ]] && ! ls /tmp/claude-skill-loaded-'"${sid12}"'-* >/dev/null 2>&1'
  fi
}
wt_read "a read in the project's own checkout (control)" yes "${wt}/main/.agents/skills/golang-engineering/SKILL.md"
wt_read "a read in a second worktree of the project" yes "${wt}/second/.agents/skills/golang-engineering/SKILL.md"
wt_read "a skill only that worktree has" yes "${wt}/second/.agents/skills/wt-only/SKILL.md"
wt_read "a worktree id missing from its .agents/skills" no "${wt}/second/.claude/skills/claude-only/SKILL.md"
wt_read "an unrelated repo with the same skill id" no "${wt}/other/.agents/skills/golang-engineering/SKILL.md"
mkdir -p "${work}/badgit" "${work}/nogit"
printf '#!/bin/sh\nexit 1\n' >"${work}/badgit/git"
chmod +x "${work}/badgit/git"
for t in bash python3 cat tr cut touch rm ls; do ln -s "$(command -v "${t}")" "${work}/nogit/${t}"; done
PATH="${work}/badgit:${PATH}" wt_read "with git failing, a second worktree" no "${wt}/second/.agents/skills/golang-engineering/SKILL.md"
PATH="${work}/badgit:${PATH}" wt_read "with git failing, the project checkout" yes "${wt}/main/.agents/skills/golang-engineering/SKILL.md"
PATH="${work}/nogit" wt_read "with no git at all, a second worktree" no "${wt}/second/.agents/skills/golang-engineering/SKILL.md"
PATH="${work}/nogit" wt_read "with no git at all, the project checkout" yes "${wt}/main/.agents/skills/golang-engineering/SKILL.md"
cleanup_markers

# ── K. payload log: opt-in, ids and key names only ────────────────────────────

logf="${work}/hook.log"
run_start "$(start_in "${sid}" "${H}" "${sid}")"
read_as "$(ids "${F}")"
check "with ESHU_CURSOR_HOOK_LOG unset nothing is logged" '[[ ! -e "${logf}" ]]'
export ESHU_CURSOR_HOOK_LOG="${logf}"
run beforeShellExecution .claude/hooks/guard-live-gate.sh "$(shell_in 'echo LOGSECRET')"
run_start "$(start_in "${sid}" "${H}" "${sid}")"
read_as "$(ids "${F}" "${F}" "${H}")"
unset ESHU_CURSOR_HOOK_LOG
log_ok="$(python3 - "${logf}" "${sid}" <<'PY' 2>&1
import json, sys
want = {"event", "keys", "conversation_id", "session_id", "subagent_id", "parent_conversation_id", "key"}
rows = [json.loads(l) for l in open(sys.argv[1])]
assert [r["event"] for r in rows] == ["beforeShellExecution", "subagentStart", "postToolUse"], rows
assert all(set(r) == want for r in rows), rows
assert "command" in rows[0]["keys"] and rows[0]["keys"] == sorted(rows[0]["keys"]), rows[0]
assert rows[2]["subagent_id"] and rows[2]["key"] == sys.argv[2], rows[2]
print("ok")
PY
)"
check "the log has one line per event with only the allowed fields" '[[ "${log_ok}" == ok ]]'
check "the log carries no command, task or other text" '! rg -q LOGSECRET "${logf}"'
cleanup_markers
cleanup_links

# LAST line on purpose: the parent checks it to prove this file ran.
cursor_helper_cases_loaded=1
