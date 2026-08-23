#!/usr/bin/env bash
#
# test-agent-hooks.sh — behavioural mirror for .claude/hooks/.
#
# verify-agent-canon.sh proves every skill is *reachable* from the nudge table.
# It cannot prove the arms route to the right skill, and both mis-routes found
# when this table moved into the repo were ones a reachability check passes:
# security-scan.yml and verify-telemetry-coverage.sh each matched a broad arm
# sitting above the specific one.
#
# Two properties are asserted that a text-only check would miss:
#   - the EXIT CODE. skill-nudge blocks (2) rather than suggesting (0). A test
#     that only grepped the message would pass against the advisory version
#     this replaced, which is the whole behaviour under change.
#   - arm ORDER. bash `case` takes the first match, so a broad arm added above
#     a narrow one silently reintroduces the bug. The control cases below fail
#     if someone repairs a mis-route by widening a narrow arm instead of
#     ordering it.
set -uo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
hooks_dir="$repo_root/.claude/hooks"
passed=0
failed=0
sid=0

# Every marker this suite writes is namespaced by pid so a concurrent run, or a
# developer's real session, cannot collide with it. Cleared on the way out.
run_tag="t$$"
cleanup() {
  rm -f "/tmp/claude-skill-loaded-${run_tag}"* \
        "/tmp/claude-skill-override-${run_tag}"* \
        "/tmp/claude-skill-payload-${run_tag}"* 2>/dev/null
}
trap cleanup EXIT

payload() { printf '{"session_id":"%s","tool_input":{"file_path":"%s"}}' "$1" "$2"; }

# blocks <repo-relative path> <expected skill id>
# Paths resolve against the real checkout on purpose: the hook refuses to act
# outside an Eshu tree, so a synthetic /r/... path would make every case pass by
# exiting early rather than by routing correctly.
blocks() {
  sid=$((sid + 1))
  local path="$repo_root/$1" want="$2" out rc
  out=$(payload "${run_tag}${sid}" "$path" | bash "$hooks_dir/skill-nudge.sh" 2>&1)
  rc=$?
  if [ "$rc" -eq 2 ] && printf '%s' "$out" | rg -Fq -- "$want"; then
    printf 'ok - %s blocks, naming %s\n' "$1" "$want"
    passed=$((passed + 1))
  else
    printf 'FAIL - %s should block(2) naming %s; exit=%s out=%s\n' \
      "$1" "$want" "$rc" "${out:-<empty>}" >&2
    failed=$((failed + 1))
  fi
}

# quiet <repo-relative path> — no arm claims it, so it must pass silently.
quiet() {
  sid=$((sid + 1))
  local path="$repo_root/$1" out rc
  out=$(payload "${run_tag}${sid}" "$path" | bash "$hooks_dir/skill-nudge.sh" 2>&1)
  rc=$?
  if [ "$rc" -eq 0 ] && [ -z "$out" ]; then
    printf 'ok - %s passes silently\n' "$1"
    passed=$((passed + 1))
  else
    printf 'FAIL - %s should pass silently; exit=%s out=%s\n' "$1" "$rc" "$out" >&2
    failed=$((failed + 1))
  fi
}

# --- routing: each governed surface blocks, naming the right skill -----------
# Regressions: these two routed to generator-script-discipline before the
# narrow arms were ordered above the broad workflow/verifier arms.
blocks '.github/workflows/security-scan.yml' 'eshu-security-scan-gates'
blocks 'scripts/verify-telemetry-coverage.sh' 'telemetry-coverage-discipline'

# Controls: the broad arms must still catch what they legitimately own. These
# flip to FAIL if a future mis-route is "fixed" by widening a narrow arm.
blocks 'scripts/verify-openapi.sh' 'generator-script-discipline'
blocks '.github/workflows/test.yml' 'generator-script-discipline'

blocks 'go/internal/telemetry/instruments.go' 'telemetry-coverage-discipline'
blocks 'go/internal/queue/claim.go' 'concurrency-deadlock-rigor'
blocks 'go/go.mod' 'eshu-security-scan-gates'
blocks 'docs/internal/remote-validation/x.md' 'eshu-performance-rigor'
blocks 'go/internal/storage/postgres/q.sql' 'eshu-postgres-rigor'
blocks 'go/internal/reducer/run.go' 'eshu-correlation-truth'
blocks 'go/internal/query/handler.go' 'eshu-mcp-call-rigor'

# The *.go fallback. Nothing above claims go/cmd/ci-gates, so before it existed
# these produced no nudge at all.
blocks 'go/cmd/ci-gates/await.go' 'golang-engineering'
blocks 'go/cmd/api/main.go' 'golang-engineering'
blocks 'go/internal/parser/parse.go' 'golang-engineering'

# Placement guards. The *.go fallback must stay BELOW the specialist arms; each
# of these flips to golang-engineering if someone reorders the case.
blocks 'go/internal/collector/doc.go' 'eshu-folder-doc-keeper'
blocks 'go/internal/telemetry/contract_x.go' 'telemetry-coverage-discipline'
blocks 'go/internal/reducer/project.go' 'eshu-correlation-truth'

# --- surfaces nobody claims stay out of the way ------------------------------
quiet 'README-not-a-package.txt'
quiet 'deploy/values.yaml'

# --- the requirement is LOADING, not acknowledgement -------------------------
# The block lifts only when the skill is recorded as loaded. Without this case
# the suite would pass against a hook that blocked once and then let anything
# through, which is acknowledgement theatre rather than enforcement.
sid=$((sid + 1))
probe_sid="${run_tag}${sid}"
touch "/tmp/claude-skill-loaded-${probe_sid}-golang-engineering"
out=$(payload "$probe_sid" "$repo_root/go/cmd/api/main.go" \
  | bash "$hooks_dir/skill-nudge.sh" 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && [ -z "$out" ]; then
  printf 'ok - a loaded skill lifts the block\n'
  passed=$((passed + 1))
else
  printf 'FAIL - loaded skill should pass silently; exit=%s out=%s\n' "$rc" "$out" >&2
  failed=$((failed + 1))
fi

# Retrying WITHOUT loading must be refused again. This is the difference
# between requiring a load and requiring a shrug.
sid=$((sid + 1))
probe_sid="${run_tag}${sid}"
payload "$probe_sid" "$repo_root/go/cmd/api/main.go" \
  | bash "$hooks_dir/skill-nudge.sh" >/dev/null 2>&1
first=$?
payload "$probe_sid" "$repo_root/go/cmd/api/main.go" \
  | bash "$hooks_dir/skill-nudge.sh" >/dev/null 2>&1
second=$?
if [ "$first" -eq 2 ] && [ "$second" -eq 2 ]; then
  printf 'ok - retrying without loading is refused again\n'
  passed=$((passed + 1))
else
  printf 'FAIL - unloaded retry should stay blocked; got %s then %s\n' \
    "$first" "$second" >&2
  failed=$((failed + 1))
fi

# A multi-skill arm must require ALL of them. Loading one is not enough.
sid=$((sid + 1))
probe_sid="${run_tag}${sid}"
touch "/tmp/claude-skill-loaded-${probe_sid}-concurrency-deadlock-rigor"
out=$(payload "$probe_sid" "$repo_root/go/internal/queue/claim.go" \
  | bash "$hooks_dir/skill-nudge.sh" 2>&1)
rc=$?
if [ "$rc" -eq 2 ] && printf '%s' "$out" | rg -Fq 'eshu-postgres-rigor' \
   && ! printf '%s' "$out" | rg -Fq 'concurrency-deadlock-rigor'; then
  printf 'ok - a partially satisfied arm still blocks, naming only what is missing\n'
  passed=$((passed + 1))
else
  printf 'FAIL - partial load should block naming only the missing skill; exit=%s out=%s\n' \
    "$rc" "$out" >&2
  failed=$((failed + 1))
fi

# The escape hatch, for a skill that genuinely cannot be loaded.
sid=$((sid + 1))
probe_sid="${run_tag}${sid}"
touch "/tmp/claude-skill-override-${probe_sid}"
out=$(payload "$probe_sid" "$repo_root/go/cmd/api/main.go" \
  | bash "$hooks_dir/skill-nudge.sh" 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && [ -z "$out" ]; then
  printf 'ok - the override marker lets an unloadable surface through\n'
  passed=$((passed + 1))
else
  printf 'FAIL - override should pass silently; exit=%s out=%s\n' "$rc" "$out" >&2
  failed=$((failed + 1))
fi

# --- the repo guard ----------------------------------------------------------
# At user level this hook sits in front of every repository on the machine, so
# a Go file outside an Eshu checkout must be untouched. This case is also why
# the paths above resolve against the real checkout: synthetic ones would exit
# here and every assertion would pass for the wrong reason.
sid=$((sid + 1))
out=$(payload "${run_tag}${sid}" "/tmp/not-eshu/go/cmd/x/main.go" \
  | bash "$hooks_dir/skill-nudge.sh" 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && [ -z "$out" ]; then
  printf 'ok - a Go file outside an Eshu checkout is untouched\n'
  passed=$((passed + 1))
else
  printf 'FAIL - outside-repo path should pass silently; exit=%s out=%s\n' "$rc" "$out" >&2
  failed=$((failed + 1))
fi

# --- skill-loaded.sh records what was loaded ---------------------------------
sid=$((sid + 1))
probe_sid="${run_tag}${sid}"
printf '{"session_id":"%s","tool_input":{"skill":"superpowers:brainstorming"}}' "$probe_sid" \
  | bash "$hooks_dir/skill-loaded.sh" >/dev/null 2>&1
if [ -f "/tmp/claude-skill-loaded-${probe_sid}-brainstorming" ]; then
  printf 'ok - skill-loaded records a load and strips the plugin prefix\n'
  passed=$((passed + 1))
else
  printf 'FAIL - skill-loaded did not record superpowers:brainstorming\n' >&2
  failed=$((failed + 1))
fi

sid=$((sid + 1))
probe_sid="${run_tag}${sid}"
printf '{"session_id":"%s","tool_input":{}}' "$probe_sid" \
  | bash "$hooks_dir/skill-loaded.sh" >/dev/null 2>&1
rc=$?
if [ "$rc" -eq 0 ]; then
  printf 'ok - skill-loaded never blocks, even on an unexpected payload\n'
  passed=$((passed + 1))
else
  printf 'FAIL - skill-loaded must always exit 0; got %s\n' "$rc" >&2
  failed=$((failed + 1))
fi

# --- guard-live-gate ---------------------------------------------------------
sid=$((sid + 1))
out=$(printf '{"tool_input":{"command":"ESHU_POSTGRES_PORT=15532 make pre-pr"}}' \
  | bash "$hooks_dir/guard-live-gate.sh" 2>&1)
rc=$?
if [ "$rc" -eq 0 ]; then
  printf 'ok - explicit port override is allowed through\n'
  passed=$((passed + 1))
else
  printf 'FAIL - port-override run should pass; exit=%s out=%s\n' "$rc" "$out" >&2
  failed=$((failed + 1))
fi

sid=$((sid + 1))
out=$(printf '{"tool_input":{"command":"CLAUDE_HOOK_ALLOW=1 make pre-pr"}}' \
  | bash "$hooks_dir/guard-live-gate.sh" 2>&1)
rc=$?
if [ "$rc" -eq 0 ]; then
  printf 'ok - CLAUDE_HOOK_ALLOW=1 escape hatch works\n'
  passed=$((passed + 1))
else
  printf 'FAIL - override should pass; exit=%s out=%s\n' "$rc" "$out" >&2
  failed=$((failed + 1))
fi

sid=$((sid + 1))
out=$(printf '{"tool_input":{"command":"go test ./..."}}' \
  | bash "$hooks_dir/guard-live-gate.sh" 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && [ -z "$out" ]; then
  printf 'ok - unrelated command is untouched\n'
  passed=$((passed + 1))
else
  printf 'FAIL - unrelated command should pass silently; exit=%s out=%s\n' "$rc" "$out" >&2
  failed=$((failed + 1))
fi

sid=$((sid + 1))
out=$(printf '{"cwd":"/tmp/not-eshu","tool_input":{"command":"make pre-pr"}}' \
  | bash "$hooks_dir/guard-live-gate.sh" 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && [ -z "$out" ]; then
  printf 'ok - live-gate guard ignores make pre-pr outside an Eshu checkout\n'
  passed=$((passed + 1))
else
  printf 'FAIL - outside-repo make pre-pr should pass silently; exit=%s out=%s\n' "$rc" "$out" >&2
  failed=$((failed + 1))
fi

# --- on-compact --------------------------------------------------------------
sid=$((sid + 1))
out=$(printf '{}' | bash "$hooks_dir/on-compact.sh" 2>/dev/null)
if printf '%s' "$out" | python3 -c 'import json,sys; d=json.load(sys.stdin); sys.exit(0 if "eshu-session-lifecycle" in d["hookSpecificOutput"]["additionalContext"] else 1)' 2>/dev/null; then
  printf 'ok - on-compact emits valid JSON naming eshu-session-lifecycle\n'
  passed=$((passed + 1))
else
  printf 'FAIL - on-compact output bad: %s\n' "${out:-<empty>}" >&2
  failed=$((failed + 1))
fi

sid=$((sid + 1))
out=$(printf '{"cwd":"/tmp/not-eshu"}' | bash "$hooks_dir/on-compact.sh" 2>/dev/null)
if [ -z "$out" ]; then
  printf 'ok - on-compact says nothing outside an Eshu checkout\n'
  passed=$((passed + 1))
else
  printf 'FAIL - on-compact should be silent outside the repo; got: %s\n' "$out" >&2
  failed=$((failed + 1))
fi

printf '\nagent-hooks test mirror: %s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ] || exit 1
