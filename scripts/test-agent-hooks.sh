#!/usr/bin/env bash
#
# test-agent-hooks.sh — behavioural mirror for .claude/hooks/.
#
# verify-agent-canon.sh proves every skill is *reachable* from the nudge table.
# It cannot prove the arms route to the right skill, and the two failures this
# suite pins were both mis-routes that a reachability check passes:
# security-scan.yml and verify-telemetry-coverage.sh each matched a broad arm
# placed above the specific one and nudged generator-script-discipline.
#
# Ordering is the fragile part. bash `case` takes the first match, so any arm
# added above the narrow ones silently reintroduces the bug. The control case
# below (verify-openapi.sh) fails if someone fixes a mis-route by widening a
# narrow arm instead of ordering it.
set -uo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
hooks_dir="$repo_root/.claude/hooks"
passed=0
failed=0
sid=0

# skill-nudge fires once per (session, skill) and remembers that in a /tmp
# stamp. Reusing fixed session ids makes every run after the first silently
# emit nothing, which passes on a fresh CI runner and fails on a developer
# machine — a false green in the direction that hides. Tag each run with this
# process's pid and clear the stamps on the way out.
run_tag="t$$"
cleanup() { rm -f "/tmp/claude-nudge-${run_tag}"*; }
trap cleanup EXIT

# nudges <file_path> <expected substring>
nudges() {
  sid=$((sid + 1))
  local path="$1" want="$2" out
  out=$(printf '{"session_id":"%s%s","tool_input":{"file_path":"%s"}}' \
      "$run_tag" "$sid" "$path" \
    | bash "$hooks_dir/skill-nudge.sh" 2>/dev/null)
  if printf '%s' "$out" | rg -Fq -- "$want"; then
    printf 'ok - %s nudges %s\n' "$path" "$want"
    passed=$((passed + 1))
  else
    printf 'FAIL - %s should nudge %s, got: %s\n' "$path" "$want" "${out:-<empty>}" >&2
    failed=$((failed + 1))
  fi
}

# silent <file_path> — a path no arm claims must produce no output at all.
silent() {
  sid=$((sid + 1))
  local path="$1" out
  out=$(printf '{"session_id":"%s%s","tool_input":{"file_path":"%s"}}' \
      "$run_tag" "$sid" "$path" \
    | bash "$hooks_dir/skill-nudge.sh" 2>/dev/null)
  if [ -z "$out" ]; then
    printf 'ok - %s nudges nothing\n' "$path"
    passed=$((passed + 1))
  else
    printf 'FAIL - %s should nudge nothing, got: %s\n' "$path" "$out" >&2
    failed=$((failed + 1))
  fi
}

# Regressions: these two routed to generator-script-discipline before the arms
# were ordered ahead of the broad .github/workflows and scripts/verify- arms.
nudges '/r/.github/workflows/security-scan.yml' 'eshu-security-scan-gates'
nudges '/r/scripts/verify-telemetry-coverage.sh' 'telemetry-coverage-discipline'

# Control: the broad arms must still catch what they legitimately own. If a
# mis-route is ever "fixed" by widening a narrow arm, this flips to FAIL.
nudges '/r/scripts/verify-openapi.sh' 'generator-script-discipline'
nudges '/r/.github/workflows/test.yml' 'generator-script-discipline'

# Remaining routed surfaces.
nudges '/r/go/internal/telemetry/instruments.go' 'telemetry-coverage-discipline'
nudges '/r/go/internal/queue/claim.go' 'concurrency-deadlock-rigor'
nudges '/r/go/go.mod' 'eshu-security-scan-gates'
nudges '/r/docs/internal/remote-validation/x.md' 'eshu-performance-rigor'
nudges '/r/go/internal/storage/postgres/q.sql' 'eshu-postgres-rigor'
nudges '/r/go/internal/reducer/run.go' 'eshu-correlation-truth'
nudges '/r/go/internal/query/handler.go' 'eshu-mcp-call-rigor'
nudges '/r/go/internal/collector/doc.go' 'eshu-folder-doc-keeper'

# The *.go fallback. Nothing above claims go/cmd/ci-gates, so before the
# fallback existed these produced no nudge at all.
nudges '/r/go/cmd/ci-gates/await.go' 'golang-engineering'
nudges '/r/go/cmd/api/main.go' 'golang-engineering'
nudges '/r/go/internal/parser/parse.go' 'golang-engineering'

# Placement guard. The fallback must stay BELOW the specialist arms: a *.go arm
# hoisted above them swallows doc.go and every Go surface arm. Each of these
# would flip to golang-engineering if someone reorders the case.
nudges '/r/go/internal/collector/doc.go' 'eshu-folder-doc-keeper'
nudges '/r/go/internal/telemetry/contract_x.go' 'telemetry-coverage-discipline'
nudges '/r/go/internal/reducer/project.go' 'eshu-correlation-truth'

# Non-Go unclaimed paths stay quiet: a hook that fires on everything gets ignored.
silent '/r/README-not-a-package.txt'
silent '/r/deploy/values.yaml'

# The live-gate guard: blocks a default-port run, allows an explicit override.
sid=$((sid + 1))
out=$(printf '{"tool_input":{"command":"ESHU_POSTGRES_PORT=15532 make pre-pr"}}' \
  | bash "$hooks_dir/guard-live-gate.sh" 2>&1)
rc=$?
if [ "$rc" -eq 0 ]; then
  printf 'ok - explicit port override is allowed through\n'
  passed=$((passed + 1))
else
  printf 'FAIL - port-override run should pass, exit=%s out=%s\n' "$rc" "$out" >&2
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
  printf 'FAIL - override should pass, exit=%s out=%s\n' "$rc" "$out" >&2
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
  printf 'FAIL - unrelated command should pass silently, exit=%s out=%s\n' "$rc" "$out" >&2
  failed=$((failed + 1))
fi

# on-compact must emit valid JSON naming the session skill.
sid=$((sid + 1))
out=$(printf '{}' | bash "$hooks_dir/on-compact.sh" 2>/dev/null)
if printf '%s' "$out" | python3 -c 'import json,sys; d=json.load(sys.stdin); sys.exit(0 if "eshu-session-lifecycle" in d["hookSpecificOutput"]["additionalContext"] else 1)' 2>/dev/null; then
  printf 'ok - on-compact emits valid JSON naming eshu-session-lifecycle\n'
  passed=$((passed + 1))
else
  printf 'FAIL - on-compact output bad: %s\n' "${out:-<empty>}" >&2
  failed=$((failed + 1))
fi

printf '\nagent-hooks test mirror: %s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ] || exit 1
