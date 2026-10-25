#!/usr/bin/env bash
# shellcheck shell=bash
# Companion cases for scripts/test-agent-hooks.sh: the guard-live-gate BLOCKING
# path against a fake running ci-gates process (#9098). Sourced by the mirror,
# which owns passed, failed, skipped, sid, repo_root and hooks_dir; split out
# because the mirror is a grandfathered file that may not grow.
# shellcheck disable=SC2154  # the mirror defines the counters and paths.

# --- the guard's BLOCKING path, which is its whole reason to exist -----------
# Both detection branches are exercised hermetically. Neither needs a real
# gate: a typo in the lsof flags or pgrep -x becoming pgrep -f would silently
# disable detection, and every pass-through case above would still be green.

# pgrep branch: a process whose name is literally ci-gates (see the helper for
# why it is a compiled binary). A host that cannot build it prints a HOST-GAP
# line and the case counts as skipped, not failed.
. "$repo_root/scripts/lib/agent-hooks-fake-gate.sh"
sid=$((sid + 1))
if start_fake_gate; then
  sleep 0.3
  out=$(printf '{"cwd":"%s","tool_input":{"command":"make pre-pr"}}' "$repo_root" \
    | bash "$hooks_dir/guard-live-gate.sh" 2>&1)
  rc=$?
  kill "$fake_pid" 2>/dev/null
  wait "$fake_pid" 2>/dev/null
  rm -rf "$fakebin"
  if [ "$rc" -eq 2 ] && rg -Fq 'ci-gates run is already in flight' <<<"$out"; then
    printf 'ok - a running ci-gates process blocks the gate\n'
    passed=$((passed + 1))
  else
    printf 'FAIL - running ci-gates should block(2); exit=%s out=%s\n' "$rc" "$out" >&2
    failed=$((failed + 1))
  fi
else
  printf 'skip - a running ci-gates process blocks the gate: HOST-GAP, no fake ci-gates here\n'
  skipped=$((skipped + 1))
fi

# A running gate blocks even with a full port override: the second run still
# contends for CPU, and an earlier revision let any override skip this check.
sid=$((sid + 1))
if start_fake_gate; then
  sleep 0.3
  out=$(printf '{"cwd":"%s","tool_input":{"command":"ESHU_POSTGRES_PORT=15532 NEO4J_BOLT_PORT=7788 make pre-pr"}}' "$repo_root" \
    | bash "$hooks_dir/guard-live-gate.sh" 2>&1)
  rc=$?
  kill "$fake_pid" 2>/dev/null
  wait "$fake_pid" 2>/dev/null
  rm -rf "$fakebin"
  if [ "$rc" -eq 2 ]; then
    printf 'ok - a port override does not waive the running-process check\n'
    passed=$((passed + 1))
  else
    printf 'FAIL - override must not bypass the process check; exit=%s out=%s\n' "$rc" "$out" >&2
    failed=$((failed + 1))
  fi
else
  printf 'skip - a port override does not waive the running-process check: HOST-GAP, no fake ci-gates here\n'
  skipped=$((skipped + 1))
fi
