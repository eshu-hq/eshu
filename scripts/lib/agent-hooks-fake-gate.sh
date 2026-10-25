#!/usr/bin/env bash
# Fake running gate for scripts/test-agent-hooks.sh. The kernel names a script
# process after the script file, so an executable called ci-gates satisfies
# `pgrep -x ci-gates` without running a gate. A `sleep` symlinked under that
# name does not work: the multi-call uutils coreutils on Ubuntu 26.04 refuses
# to run under another name (#9067).

# start_fake_gate starts the fake and sets fakebin and fake_pid. The caller
# stops it with `kill "$fake_pid"` and removes "$fakebin".
start_fake_gate() {
  fakebin="$(mktemp -d)"
  printf '#!/bin/sh\nsleep 30 &\ntrap '"'"'kill $!; exit 0'"'"' TERM INT\nwait\n' >"$fakebin/ci-gates"
  chmod +x "$fakebin/ci-gates"
  "$fakebin/ci-gates" &
  fake_pid=$!
}
