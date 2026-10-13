#!/usr/bin/env bash
set -euo pipefail
# OOM/external-SIGKILL simulation for the #8062 137 case: die of SIGKILL
# before doing anything, so the verifier reads 137 off `wait` and must take
# the loud-timeout branch. The trailing exit documents the expected status
# if the signal ever failed to land; `wait` reports 137 either way.
if [[ -n "${ESHU_FAKE_UV_SIGKILL_SELF:-}" ]]; then
  echo "[fake-uv] dying of SIGKILL (OOM simulation)" >&2
  kill -KILL "$$"
  exit 137
fi
# Stalled-build simulation for the #7983 timeout cases: sleep before doing
# anything so the verifier's bound has something to fire on.
if [[ -n "${ESHU_FAKE_UV_SLEEP:-}" ]]; then
  echo "[fake-uv] sleeping ${ESHU_FAKE_UV_SLEEP}s (stalled-build simulation)" >&2
  sleep "${ESHU_FAKE_UV_SLEEP}" &
  sleeper=$!
  if [[ -n "${ESHU_FAKE_UV_SLEEP_PIDFILE:-}" ]]; then
    echo "${sleeper}" > "${ESHU_FAKE_UV_SLEEP_PIDFILE}"
  fi
  wait "${sleeper}"
fi
# Capture the full command for assertion
echo "[fake-uv] $*" >&2
# Verify the docs command contains the expected mkdocs build flags
# (the config-file path is absolute from the verifier's repo_root)
case "$*" in
  *"mkdocs build --strict --clean --config-file"*)
    if [[ -n "${ESHU_FAKE_UV_FAIL:-}" ]]; then
      echo "[fake-uv] simulating mkdocs build failure" >&2
      exit 1
    fi
    echo "[fake-uv] mkdocs build SUCCESS" >&2
    exit 0
    ;;
  *)
    echo "[fake-uv] unexpected command: $*" >&2
    exit 1
    ;;
esac
