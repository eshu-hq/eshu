#!/usr/bin/env bash
set -euo pipefail
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
