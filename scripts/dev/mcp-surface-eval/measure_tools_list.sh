#!/usr/bin/env bash
# Measures tools/list per surface with the command from epic #8752.
#
# Usage: ESHU_MCP_BASE_URL=https://host ESHU_MCP_KEY=... ./measure_tools_list.sh
# Prints one line per surface: surface, tool count, and the byte size of the
# compact tools array (jq -c '.result.tools' | wc -c).
set -euo pipefail
: "${ESHU_MCP_BASE_URL:?set ESHU_MCP_BASE_URL}"
: "${ESHU_MCP_KEY:?set ESHU_MCP_KEY}"
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
for surface in full router core_router; do
  # The key goes through a curl config on stdin so it never appears in argv.
  printf 'header = "Authorization: Bearer %s"\n' "$ESHU_MCP_KEY" |
    curl -sS -K - -X POST \
      "${ESHU_MCP_BASE_URL%/}/mcp/message?surface=${surface}" \
      -H 'Content-Type: application/json' \
      -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' -o "$tmp"
  count="$(jq '.result.tools|length' "$tmp")"
  bytes="$(jq -c '.result.tools' "$tmp" | wc -c | tr -d ' ')"
  printf '%s\t%s tools\t%s bytes\n' "$surface" "$count" "$bytes"
done
