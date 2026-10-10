#!/usr/bin/env bash
if [[ "$DOCKER_MODE" == fail-after-sample ]]; then
 for _ in {1..100}; do
  [[ ! -e "$RUNNER_TEMP/stats-failed" ]] || break
  /bin/sleep 0.01
 done
 [[ -e "$RUNNER_TEMP/stats-failed" ]] || exit 24
else
 /bin/sleep 0.15
fi
[[ "${GATE_MODE:-pass}" != fail ]] || exit 23
