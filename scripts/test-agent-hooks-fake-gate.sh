#!/usr/bin/env bash
#
# test-agent-hooks-fake-gate.sh - behavioural mirror for the fake running gate
# in scripts/lib/agent-hooks-fake-gate.sh (#9098).
#
# test-agent-hooks.sh and test-cursor-hooks.sh need a process that
# `pgrep -x ci-gates` can see, because .claude/hooks/guard-live-gate.sh blocks
# on exactly that probe. Their blocking cases cannot prove the fake works on
# their own: while any real ci-gates runs on the host (a developer box in the
# middle of a gate run), the guard blocks on the REAL gate and the cases pass
# whether or not the fake is visible. This mirror asserts the fake's own pid,
# so a foreign gate cannot hide a broken fake.
#
# macOS names a script process after its interpreter, so a shell or Python
# script called ci-gates never matches `pgrep -x`. Only a compiled binary keeps
# the name. Every case below runs on macOS and on Linux.
set -uo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
passed=0
failed=0

ok() { printf 'ok - %s\n' "$1"; passed=$((passed + 1)); }
no() { printf 'FAIL - %s\n' "$1" >&2; failed=$((failed + 1)); }

# shellcheck source=lib/agent-hooks-fake-gate.sh
. "$repo_root/scripts/lib/agent-hooks-fake-gate.sh"

# pgrep_has PID succeeds when `pgrep -x ci-gates` lists exactly that pid.
pgrep_has() {
  pgrep -x ci-gates 2>/dev/null | rg -qx -- "$1"
}

# stop_fake stops the fake started by start_fake_gate and removes its dir.
# shellcheck disable=SC2154  # fake_pid and fakebin are set by start_fake_gate.
stop_fake() {
  kill "$fake_pid" 2>/dev/null
  wait "$fake_pid" 2>/dev/null
  [ -n "${fakebin:-}" ] && rm -rf "$fakebin"
}

# --- 1. the fake is a process `pgrep -x ci-gates` lists by its own pid -------
if start_fake_gate; then
  sleep 0.3
  if pgrep_has "$fake_pid"; then
    ok "the fake gate is listed by pgrep -x ci-gates under its own pid"
  else
    no "fake gate pid $fake_pid is not listed by pgrep -x ci-gates (comm=$(ps -p "$fake_pid" -o comm= 2>/dev/null))"
  fi
  kill "$fake_pid" 2>/dev/null
  wait "$fake_pid" 2>/dev/null
  if pgrep_has "$fake_pid"; then
    no "fake gate pid $fake_pid survived kill"
  else
    ok "killing the fake gate removes it from pgrep -x ci-gates"
  fi
  rm -rf "$fakebin"
else
  no "start_fake_gate failed on a host with a Go toolchain or Linux"
fi

# --- 2. with no Go toolchain: Linux keeps the shell fake, macOS names the gap -
out="$(ESHU_FAKE_GATE_GO=/nonexistent-go start_fake_gate 2>&1)"
ESHU_FAKE_GATE_GO=/nonexistent-go start_fake_gate >/dev/null 2>&1
rc=$?
case "$(uname -s)" in
  Linux)
    if [ "$rc" -eq 0 ]; then
      sleep 0.3
      if pgrep_has "$fake_pid"; then
        ok "Linux without Go still runs the shell fake, visible to pgrep -x ci-gates"
      else
        no "Linux shell fake pid $fake_pid is not listed by pgrep -x ci-gates"
      fi
      stop_fake
    else
      no "Linux without Go must fall back to the shell fake; rc=$rc out=$out"
    fi
    ;;
  *)
    if [ "$rc" -ne 0 ] && rg -Fq 'HOST-GAP' <<<"$out"; then
      ok "a host that cannot build the fake prints a named HOST-GAP line"
    else
      no "expected rc!=0 and a HOST-GAP line without Go; rc=$rc out=$out"
    fi
    [ -n "${fakebin:-}" ] && rm -rf "$fakebin"
    ;;
esac

printf '\n%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
