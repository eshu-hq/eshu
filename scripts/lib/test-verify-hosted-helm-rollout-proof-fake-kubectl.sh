#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  "-n eshu rollout status deployment/eshu-api --timeout=120s" | \
  "-n eshu rollout status deployment/eshu-mcp-server --timeout=120s" | \
  "-n eshu rollout status statefulset/eshu --timeout=120s" | \
  "-n eshu rollout status deployment/eshu-resolution-engine --timeout=120s")
    printf 'rolled out\n'
    ;;
  "-n eshu get job/eshu-schema-bootstrap -o json")
    cat <<'JSON'
{"status":{"succeeded":1,"failed":0,"conditions":[{"type":"Complete","status":"True"}]}}
JSON
    ;;
  *)
    printf 'unexpected kubectl args: %s\n' "$*" >&2
    exit 1
    ;;
esac
