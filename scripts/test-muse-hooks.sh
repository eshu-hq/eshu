#!/usr/bin/env bash
# Behavioural mirror for .muse/hooks/* -- the Muse Code port of the Claude hooks.
#
# Each Muse wrapper translates the Muse envelope to the Claude shape its
# sibling already handles, then delegates to that same file, so one logic
# copy serves both harnesses. These tests feed RECORDED Muse payloads
# (captured live from `muse exec`; see docs/internal/agent-hooks-muse.md)
# through the wrappers and assert the translation plus the verdict.
#
# Fails while the wrappers are missing (RED); the port must turn it green.
# Probe hygiene from agent-hooks.md applies: run-unique session ids, a private
# TMPDIR for the nudge counters, and marker cleanup up front, so a stale
# counter or marker can never read as a behavioural finding.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MH="${repo_root}/.muse/hooks"
CH="${repo_root}/.claude/hooks"

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
mkdir -p "${work}/.claude" "${work}/tmp"
export TMPDIR="${work}/tmp"

passed=0
failed=0
ok() { printf 'ok - %s\n' "$1"; passed=$((passed + 1)); }
no() { printf 'not ok - %s\n' "$1"; failed=$((failed + 1)); }

sid="museport$$"
sid12="$(printf '%s' "${sid}" | cut -c1-12)"
rm -f "/tmp/claude-skill-loaded-${sid12}-"* "/tmp/claude-skill-override-${sid12}"

# ── recorded Muse envelopes (keys as `muse exec` delivered them) ────────────

muse_stop() { # turn_id [stop_hook_active]
  printf '{"hook_event_name":"Stop","stop_hook_active":%s,"last_assistant_message":"status","session_id":"%s","turn_id":"%s","cwd":"%s","transcript_path":null,"model":"m","permission_mode":"default","model_provider":"meta"}' \
    "${2:-false}" "${sid}" "$1" "${work}"
}

muse_prompt() { # prompt_text
  printf '{"hook_event_name":"UserPromptSubmit","prompt":%s,"session_id":"%s","turn_id":"t-%s","cwd":"%s","transcript_path":null}' \
    "$(printf '%s' "$1" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))')" \
    "${sid}" "${sid}" "${work}"
}

muse_pretool() { # tool_name tool_input_json
  printf '{"hook_event_name":"PreToolUse","tool_name":"%s","tool_input":%s,"tool_use_id":"u1","session_id":"%s","turn_id":"t1","cwd":"%s","transcript_path":null}' \
    "$1" "$2" "${sid}" "${work}"
}

muse_posttool() { # tool_name tool_input_json
  printf '{"hook_event_name":"PostToolUse","tool_name":"%s","tool_input":%s,"tool_use_id":"u1","tool_response":"ok","session_id":"%s","turn_id":"t1","cwd":"%s","transcript_path":null}' \
    "$1" "$2" "${sid}" "${work}"
}

muse_sessionstart() { # source [cwd]
  printf '{"hook_event_name":"SessionStart","source":"%s","session_id":"%s","cwd":"%s","transcript_path":null}' \
    "$1" "${sid}" "${2:-${work}}"
}

muse_precompact() { # [cwd]
  printf '{"hook_event_name":"PreCompact","session_id":"%s","cwd":"%s","transcript_path":null}' \
    "${sid}" "${1:-${work}}"
}

verdict() { # output -> block|allow
  if printf '%s' "$1" | rg -q '"decision"[[:space:]]*:[[:space:]]*"block"'; then printf 'block'; else printf 'allow'; fi
}

# ── A. wiring: hooks.json + wrapper files ────────────────────────────────────

[[ -f "${repo_root}/.muse/hooks.json" ]] || no "project hooks file .muse/hooks.json exists"
if command -v python3 >/dev/null 2>&1 && [[ -f "${repo_root}/.muse/hooks.json" ]]; then
  if python3 -c 'import json; json.load(open("'"${repo_root}"'/.muse/hooks.json"))' 2>/dev/null; then
    ok ".muse/hooks.json is valid JSON"
  else
    no ".muse/hooks.json is valid JSON"
  fi
else
  no ".muse/hooks.json is valid JSON (missing file or python3)"
fi

for w in goal-continue goal-refresh skill-nudge skill-loaded guard-live-gate on-compact eshu-doc-staleness; do
  if [[ -f "${MH}/${w}.sh" ]]; then
    ok "wrapper exists: .muse/hooks/${w}.sh"
  else
    no "wrapper exists: .muse/hooks/${w}.sh"
    continue
  fi
  [[ -x "${MH}/${w}.sh" ]] || no "wrapper is executable: ${w}.sh"
  bash -n "${MH}/${w}.sh" 2>/dev/null || no "wrapper parses: ${w}.sh"
done
[[ -f "${MH}/lib/muse-root.sh" ]] || no "shared lib .muse/hooks/lib/muse-root.sh exists"

if [[ -f "${repo_root}/.muse/hooks.json" ]]; then
  for w in goal-continue goal-refresh skill-nudge skill-loaded guard-live-gate on-compact eshu-doc-staleness; do
    if rg -q "\.muse/hooks/${w}\.sh" "${repo_root}/.muse/hooks.json"; then
      ok "hooks.json routes to ${w}.sh"
    else
      no "hooks.json routes to ${w}.sh"
    fi
  done
  for ev in SessionStart UserPromptSubmit PreToolUse PostToolUse Stop PreCompact; do
    if rg -q "\"${ev}\"" "${repo_root}/.muse/hooks.json"; then
      ok "hooks.json wires event ${ev}"
    else
      no "hooks.json wires event ${ev}"
    fi
  done
  for tool in write_file edit_file bash read_skill; do
    if rg -q "\"${tool}\"" "${repo_root}/.muse/hooks.json"; then
      ok "hooks.json matches Muse tool ${tool}"
    else
      no "hooks.json matches Muse tool ${tool}"
    fi
  done
fi

# From here every case needs the wrappers; stop the bleeding if they are absent.
if [[ ! -f "${MH}/goal-continue.sh" ]]; then
  printf '\nwrappers missing -- envelope cases cannot run\n'
  printf '\nmuse hooks mirror: %s passed, %s failed\n' "${passed}" "${failed}"
  exit 1
fi

run_stop() { printf '%s' "$1" | CLAUDE_GOAL_FILE="${goal}" bash "${MH}/goal-continue.sh" 2>/dev/null; }

# ── B. Stop: Muse envelope enforced like the Claude one ──────────────────────

goal="${work}/goal-stop"
rm -f "${goal}"
[[ "$(verdict "$(run_stop "$(muse_stop t1)")")" == "allow" ]] && ok "Stop with no goal file allows" || no "Stop with no goal file allows"

printf 'SESSION: %s\nFinish the migration.\n' "${sid}" >"${goal}"
out="$(run_stop "$(muse_stop t2)")"
[[ "$(verdict "${out}")" == "block" ]] && ok "Stop with active goal blocks on Muse envelope" || no "Stop with active goal blocks on Muse envelope"
printf '%s' "${out}" | rg -q "Finish the migration" && ok "block reason quotes the goal text" || no "block reason quotes the goal text"

printf 'DONE\nFinished.\n' >"${goal}"
[[ "$(verdict "$(run_stop "$(muse_stop t3)")")" == "allow" ]] && ok "Stop with DONE goal allows" || no "Stop with DONE goal allows"

# turn_id is the budget key (Claude's prompt_id is absent from the envelope).
printf 'SESSION: %s\nUnfinished work.\n' "${sid}" >"${goal}"
export CLAUDE_GOAL_MAX_NUDGES=2
[[ "$(verdict "$(run_stop "$(muse_stop budget1)")")" == "block" ]] && ok "budget: first stop of the turn blocks" || no "budget: first stop of the turn blocks"
[[ "$(verdict "$(run_stop "$(muse_stop budget1)")")" == "block" ]] && ok "budget: second stop of the turn blocks" || no "budget: second stop of the turn blocks"
[[ "$(verdict "$(run_stop "$(muse_stop budget1)")")" == "allow" ]] && ok "budget: exhausted turn allows" || no "budget: exhausted turn allows"
[[ "$(verdict "$(run_stop "$(muse_stop budget2)")")" == "block" ]] && ok "budget: a new turn_id starts a fresh budget" || no "budget: a new turn_id starts a fresh budget"
if ls "${work}/tmp"/claude-goal-nudge-*-budget1 >/dev/null 2>&1; then
  ok "budget counter is keyed on turn_id"
else
  no "budget counter is keyed on turn_id"
fi
unset CLAUDE_GOAL_MAX_NUDGES

# ── C. UserPromptSubmit: producer + refresher ─────────────────────────────────

unset CLAUDE_GOAL_FILE
rm -f "${work}"/.claude/active-goal.*
out="$(printf '%s' "$(muse_prompt '/goal Migrate the hooks')" | bash "${MH}/goal-refresh.sh" 2>/dev/null)"
produced="${work}/.claude/active-goal.${sid}"
if [[ -f "${produced}" ]] && rg -q "Migrate the hooks" "${produced}"; then
  ok "/goal prompt writes the per-session goal file"
else
  no "/goal prompt writes the per-session goal file"
fi

export CLAUDE_GOAL_FILE="${work}/goal-refresh"
printf 'SESSION: %s\nHold the line.\n' "${sid}" >"${CLAUDE_GOAL_FILE}"
out="$(printf '%s' "$(muse_prompt 'continue')" | bash "${MH}/goal-refresh.sh" 2>/dev/null)"
printf '%s' "${out}" | rg -q "Hold the line" && ok "active goal is restated on every prompt" || no "active goal is restated on every prompt"
printf 'DONE\n' >"${CLAUDE_GOAL_FILE}"
out="$(printf '%s' "$(muse_prompt 'continue')" | bash "${MH}/goal-refresh.sh" 2>/dev/null)"
[[ -z "${out}" ]] && ok "retired goal stays silent" || no "retired goal stays silent"
unset CLAUDE_GOAL_FILE

# ── D. PreToolUse edit wrapper: path key translated ──────────────────────────
#
# Quoting rule: build inline JSON with single quotes plus '"${var}"'
# interpolation. A \"-escaped {"a","b"} inside a piped "$()" is brace-split
# by bash 3.2 into two invocations, silently corrupting the payload.

export CLAUDE_GOAL_FILE="${work}/goal-unused"
governed="${repo_root}/go/hook-probe-$$.go"
if [[ "$(printf '%s' "$(muse_pretool write_file '{"path":"'"${governed}"'","content":"x"}')" | bash "${MH}/skill-nudge.sh" >/dev/null 2>&1; printf '%s' "$?")" == "2" ]]; then
  ok "write_file on a governed surface is refused without the skill"
else
  no "write_file on a governed surface is refused without the skill"
fi
muse_pretool_cwd() { # tool_name tool_input_json cwd
  printf '{"hook_event_name":"PreToolUse","tool_name":"%s","tool_input":%s,"tool_use_id":"u1","session_id":"%s","turn_id":"t1","cwd":"%s","transcript_path":null}' \
    "$1" "$2" "${sid}" "$3"
}
if [[ "$(printf '%s' "$(muse_pretool_cwd write_file '{"path":"go/hook-rel-$$.go","content":"x"}' "${repo_root}")" | bash "${MH}/skill-nudge.sh" >/dev/null 2>&1; printf '%s' "$?")" == "2" ]]; then
  ok "relative write_file path resolves against payload cwd and is refused"
else
  no "relative write_file path resolves against payload cwd and is refused"
fi
touch "/tmp/claude-skill-loaded-${sid12}-golang-engineering"
if printf '%s' "$(muse_pretool edit_file '{"path":"'"${governed}"'","find":"a","replace":"b"}')" | bash "${MH}/skill-nudge.sh" >/dev/null 2>&1; then
  ok "edit_file passes once the skill is loaded"
else
  no "edit_file passes once the skill is loaded"
fi
rm -f "/tmp/claude-skill-loaded-${sid12}-golang-engineering"
if printf '%s' "$(muse_pretool write_file '{"path":"'"${work}"'/notes.txt","content":"x"}')" | bash "${MH}/skill-nudge.sh" >/dev/null 2>&1; then
  ok "edits outside an Eshu checkout pass untouched"
else
  no "edits outside an Eshu checkout pass untouched"
fi

# ── E. PostToolUse skill wrapper: read_skill name recorded ───────────────────

printf '%s' "$(muse_posttool read_skill '{"name":"bundled:taste"}')" | bash "${MH}/skill-loaded.sh" >/dev/null 2>&1
[[ -f "/tmp/claude-skill-loaded-${sid12}-taste" ]] && ok "read_skill load is recorded with the bare id" || no "read_skill load is recorded with the bare id"
printf 'garbage' | bash "${MH}/skill-loaded.sh" >/dev/null 2>&1
[[ "$?" -eq 0 ]] && ok "skill wrapper never blocks, even on garbage" || no "skill wrapper never blocks, even on garbage"

# ── F. guard-live-gate wrapper: same command key ─────────────────────────────

guard_in() { printf '{"hook_event_name":"PreToolUse","tool_name":"bash","tool_input":{"command":"%s","description":"t"},"tool_use_id":"u1","session_id":"%s","turn_id":"t1","cwd":"%s","transcript_path":null}' "$1" "${sid}" "$2"; }
if printf '%s' "$(guard_in 'echo hi' "${repo_root}")" | bash "${MH}/guard-live-gate.sh" >/dev/null 2>&1; then
  ok "benign Bash passes the guard"
else
  no "benign Bash passes the guard"
fi
if printf '%s' "$(guard_in 'echo hi' "${work}")" | bash "${MH}/guard-live-gate.sh" >/dev/null 2>&1; then
  ok "guard stays silent outside an Eshu checkout"
else
  no "guard stays silent outside an Eshu checkout"
fi
if printf '%s' "$(guard_in 'CLAUDE_HOOK_ALLOW=1 make pre-pr' "${repo_root}")" | bash "${MH}/guard-live-gate.sh" >/dev/null 2>&1; then
  ok "per-call override still waives the guard"
else
  no "per-call override still waives the guard"
fi

# ── G. on-compact wrapper: fresh starts stay silent ──────────────────────────

touch "/tmp/claude-skill-loaded-${sid12}-golang-engineering"
out="$(printf '%s' "$(muse_sessionstart startup)" | bash "${MH}/on-compact.sh" 2>/dev/null)"
[[ -z "${out}" ]] && ok "fresh SessionStart emits no compaction context" || no "fresh SessionStart emits no compaction context"
[[ -f "/tmp/claude-skill-loaded-${sid12}-golang-engineering" ]] && ok "fresh SessionStart keeps skill markers" || no "fresh SessionStart keeps skill markers"
out="$(printf '%s' "$(muse_sessionstart resume "${repo_root}")" | bash "${MH}/on-compact.sh" 2>/dev/null)"
printf '%s' "${out}" | rg -q "eshu-session-lifecycle" && ok "resume re-grounds on the lifecycle skill" || no "resume re-grounds on the lifecycle skill"
touch "/tmp/claude-skill-loaded-${sid12}-golang-engineering"
printf '%s' "$(muse_precompact "${repo_root}")" | bash "${MH}/on-compact.sh" >/dev/null 2>&1
[[ ! -f "/tmp/claude-skill-loaded-${sid12}-golang-engineering" ]] && ok "PreCompact clears this session's skill markers" || no "PreCompact clears this session's skill markers"

# ── H. doc-staleness wrapper: exit 0, no tracked mutation ────────────────────

before="$(cd "${repo_root}" && git status --short | head -n 20)"
printf '%s' "$(muse_posttool write_file '{"path":"'"${governed}"'","content":"x"}')" | bash "${MH}/eshu-doc-staleness.sh" >/dev/null 2>&1
[[ "$?" -eq 0 ]] && ok "staleness wrapper exits 0" || no "staleness wrapper exits 0"
after="$(cd "${repo_root}" && git status --short | head -n 20)"
[[ "${before}" == "${after}" ]] && ok "staleness run mutates no tracked file" || no "staleness run mutates no tracked file"

# ── I. parity: same layout, both envelopes, same verdict ─────────────────────

claude_stop() { # prompt_id
  printf '{"session_id":"%s","prompt_id":"%s","stop_hook_active":false,"cwd":"%s","transcript_path":null,"hook_event_name":"Stop"}' \
    "${sid}" "$1" "${work}"
}
for layout in active done foreign blocked; do
  case "${layout}" in
    active) printf 'SESSION: %s\nDo the thing.\n' "${sid}" >"${work}/goal-parity" ;;
    done) printf 'DONE\nDid it.\n' >"${work}/goal-parity" ;;
    foreign) printf 'SESSION: somebody-else\nNot yours.\n' >"${work}/goal-parity" ;;
    blocked) printf 'SESSION: %s\nBLOCKED: waiting on CI run 12345\n' "${sid}" >"${work}/goal-parity" ;;
  esac
  c_out="$(printf '%s' "$(claude_stop "par-${layout}")" | CLAUDE_GOAL_FILE="${work}/goal-parity" bash "${CH}/goal-continue.sh" 2>/dev/null)"
  m_out="$(printf '%s' "$(muse_stop "par-${layout}")" | CLAUDE_GOAL_FILE="${work}/goal-parity" bash "${MH}/goal-continue.sh" 2>/dev/null)"
  c="$(verdict "${c_out}")"
  m="$(verdict "${m_out}")"
  if [[ "${c}" == "${m}" ]]; then ok "parity ${layout}: both ${c}"; else no "parity ${layout}: claude=${c} muse=${m}"; fi
done

rm -f "/tmp/claude-skill-loaded-${sid12}-"* "/tmp/claude-skill-override-${sid12}"

printf '\nmuse hooks mirror: %s passed, %s failed\n' "${passed}" "${failed}"
[[ "${failed}" -eq 0 ]]
