#!/usr/bin/env bash
# G8 bounded re-verification stub (#9669, part of #6707).
#
# DESIGN ONLY: without ESHU_G8_LIVE=1 it prints the probe plan. Live: runs
# exactly three bounded probes with timeouts and records elapsed time.
# Calls no mutating endpoint; records no bearer.
#
# Usage:
#   ESHU_API_BASE=https://api.example ESHU_MCP_BASE=https://mcp.example \
#     ESHU_G8_LIVE=1 scripts/9669-g8-bounded-reverify.sh
set -euo pipefail

if [[ "${ESHU_G8_LIVE:-}" != "1" ]]; then
  cat <<'EOF'
plan: curl --max-time 10 "$ESHU_API_BASE/health"            # expect 200, v0 contract map
plan: curl --max-time 10 <one-authed-read>                  # expect 200 with bearer, 401 envelope without
plan: MCP tools/list over HTTP                             # expect 200 authed; 401 fail-closed unauth
plan: record per-probe status codes + total elapsed seconds
NOT-EXECUTED: no network contact without ESHU_G8_LIVE=1.
EOF
  exit 0
fi

: "${ESHU_API_BASE:?set ESHU_API_BASE}"
: "${ESHU_MCP_BASE:?set ESHU_MCP_BASE}"
start=$(date +%s)
curl -sS -o /dev/null -w 'GET /health: %{http_code}\n' --max-time 10 "${ESHU_API_BASE}/health"
curl -sS -o /dev/null -w 'POST tools/list unauth: %{http_code}\n' --max-time 15 \
  -X POST "${ESHU_MCP_BASE}/mcp/message" -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
printf 'elapsed: %ss (one authed read runs separately with a session-local bearer, never recorded)\n' "$(( $(date +%s) - start ))"
