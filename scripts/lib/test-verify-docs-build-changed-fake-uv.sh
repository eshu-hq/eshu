#!/usr/bin/env bash
set -euo pipefail
# Stalled-build simulation for the #7983 timeout cases: sleep before doing
# anything so the verifier's bound has something to fire on.
if [[ -n "${ESHU_FAKE_UV_SLEEP:-}" ]]; then
  echo "[fake-uv] sleeping ${ESHU_FAKE_UV_SLEEP}s (stalled-build simulation)" >&2
  sleep "${ESHU_FAKE_UV_SLEEP}"
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
