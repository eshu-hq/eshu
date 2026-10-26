#!/usr/bin/env bash
# shellcheck shell=bash
# Fake running gate for scripts/test-agent-hooks.sh and
# scripts/test-cursor-hooks.sh. guard-live-gate.sh blocks on
# `pgrep -x ci-gates`, so the fake must be a process with exactly that name.
#
# macOS names a script process after its interpreter (a /bin/sh script shows as
# /bin/sh, a Python script as the Python binary), so no script can be seen by
# `pgrep -x ci-gates` there (#9098). A compiled binary keeps its own name on
# both systems, so the fake is a tiny Go program, agent-hooks-fake-gate.go,
# built into a temp dir as `ci-gates`. A `sleep` symlinked under that name does
# not work either: the multi-call uutils coreutils on Ubuntu 26.04 refuses to
# run under another name (#9067).
#
# Without a Go toolchain, Linux falls back to a shell script: the kernel names a
# script process after the script file there. macOS has no such fallback, so
# start_fake_gate returns 1 with a named HOST-GAP line and the caller skips the
# case instead of reporting a bare FAIL. ESHU_FAKE_GATE_GO names the go binary
# (default go) and ESHU_FAKE_GATE_OS the host name (default uname -s);
# scripts/test-agent-hooks-fake-gate.sh points them at a missing path and at
# Linux to exercise the no-Go and shell-fallback branches on any host.
#
# After a successful start, fake_gate_kind is "go" (the compiled fake) or
# "shell" (the Linux fallback), so a mirror can assert which one CI used.
#
# Both fakes run with stdout and stderr on /dev/null. A caller that captures
# start_fake_gate in $(...) otherwise waits for the shell fake's sleep child to
# release the pipe, about 30 s, and loses the fake's pid inside the subshell.

fake_gate_src="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/agent-hooks-fake-gate.go"

# start_fake_gate starts the fake and sets fakebin and fake_pid. The caller
# stops it with `kill "$fake_pid"` and removes "$fakebin". It returns 1 and
# prints a HOST-GAP line when this host cannot run the fake.
# shellcheck disable=SC2034  # fakebin, fake_pid and fake_gate_kind are outputs.
start_fake_gate() {
  local go_bin="${ESHU_FAKE_GATE_GO:-go}"
  local host_os="${ESHU_FAKE_GATE_OS:-$(uname -s)}"
  fakebin="$(mktemp -d)"
  fake_pid=""
  fake_gate_kind=""
  # GOFLAGS and GOWORK are cleared so a developer's settings cannot change a
  # file-mode build; GOTOOLCHAIN=local keeps the build from fetching anything.
  if command -v "$go_bin" >/dev/null 2>&1 \
    && env -u GOROOT -u GOFLAGS GOWORK=off GOTOOLCHAIN=local \
      "$go_bin" build -o "$fakebin/ci-gates" "$fake_gate_src" >/dev/null 2>&1; then
    "$fakebin/ci-gates" >/dev/null 2>&1 &
    fake_pid=$!
    fake_gate_kind=go
    return 0
  fi
  if [ "$host_os" = Linux ]; then
    printf '#!/bin/sh\nsleep 30 &\ntrap '"'"'kill $!; exit 0'"'"' TERM INT\nwait\n' >"$fakebin/ci-gates"
    chmod +x "$fakebin/ci-gates"
    "$fakebin/ci-gates" >/dev/null 2>&1 &
    fake_pid=$!
    fake_gate_kind=shell
    return 0
  fi
  printf 'HOST-GAP (%s): no Go toolchain to build the fake ci-gates process, and a script cannot be named ci-gates here; the case that needs it is not exercised on this host. Linux CI runs it.\n' "$host_os" >&2
  rm -rf "$fakebin"
  fakebin=""
  return 1
}
