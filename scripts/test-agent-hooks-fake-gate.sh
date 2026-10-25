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
# shellcheck disable=SC2154  # fakebin, fake_pid and fake_gate_kind come from the library.
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
  # Where Go is installed the compiled fake must be the one in use; a silent
  # fall back to the shell fake would pass on Linux and hide a broken build.
  if command -v go >/dev/null 2>&1; then
    if [ "$fake_gate_kind" = go ]; then
      ok "with Go installed the compiled fake is used (kind=$fake_gate_kind)"
    else
      no "Go is installed but the fake gate kind is [$fake_gate_kind], not go"
    fi
  fi
else
  no "start_fake_gate failed on a host with a Go toolchain or Linux"
fi

# --- 2. with no Go toolchain: Linux keeps the shell fake, macOS names the gap -
gap_file="$(mktemp)"
ESHU_FAKE_GATE_GO=/nonexistent-go start_fake_gate 2>"$gap_file"
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
      no "Linux without Go must fall back to the shell fake; rc=$rc gap=$(cat "$gap_file")"
    fi
    ;;
  *)
    if [ "$rc" -ne 0 ] && rg -Fq 'HOST-GAP' "$gap_file"; then
      ok "a host that cannot build the fake prints a named HOST-GAP line"
    else
      no "expected rc!=0 and a HOST-GAP line without Go; rc=$rc gap=$(cat "$gap_file")"
    fi
    [ -n "${fakebin:-}" ] && rm -rf "$fakebin"
    ;;
esac
rm -f "$gap_file"

# --- 3. the shell fallback must not hold its caller's stdout open -------------
# A caller that captures start_fake_gate with $(...) waits for every holder of
# the pipe. The shell fake's sleep child held it for 30 s and the fake's pid was
# lost inside the subshell, so it was left running. Forced onto the Linux branch
# on any host; the fake's pid and dir come back through the captured output.
begin=$SECONDS
captured="$(ESHU_FAKE_GATE_OS=Linux ESHU_FAKE_GATE_GO=/nonexistent-go start_fake_gate; printf '%s %s' "$fake_pid" "$fakebin")"
elapsed=$((SECONDS - begin))
read -r cap_pid cap_dir <<<"$captured"
if [ "$elapsed" -lt 5 ]; then
  ok "the shell fallback returns promptly inside a command substitution (${elapsed}s)"
else
  no "the shell fallback held the caller's stdout for ${elapsed}s inside a command substitution"
fi
if [ -n "$cap_pid" ]; then
  kill "$cap_pid" 2>/dev/null
  sleep 0.3
  if kill -0 "$cap_pid" 2>/dev/null; then
    no "shell fallback pid $cap_pid survived kill"
    kill -9 "$cap_pid" 2>/dev/null
  else
    ok "the shell fallback leaves no process after kill"
  fi
fi
[ -n "$cap_dir" ] && rm -rf "$cap_dir"

printf '\n%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
