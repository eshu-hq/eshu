#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

CONFIG_FILE="$TMP_DIR/mcp.json"

cat >"$CONFIG_FILE" <<'EOF'
{
  "mcpServers": {
    "eshu-e2e": {
      "type": "http",
      "url": "http://10.0.0.5:8081/mcp/message",
      "headers": {
        "Authorization": "Bearer remote-token"
      }
    }
  }
}
EOF

ESHU_MCP_CONFIG_FILE="$CONFIG_FILE" \
ESHU_LOCAL_MCP_URL="http://127.0.0.1:18081/mcp/message" \
ESHU_LOCAL_MCP_TOKEN="local-token-1" \
ESHU_SKIP_PROBES="true" \
"$REPO_ROOT/scripts/sync_local_compose_mcp.sh"

jq -e '
  .mcpServers["eshu-e2e"].url == "http://10.0.0.5:8081/mcp/message" and
  .mcpServers["eshu-e2e"].headers.Authorization == "Bearer remote-token" and
  .mcpServers["eshu-local-compose"].type == "http" and
  .mcpServers["eshu-local-compose"].url == "http://127.0.0.1:18081/mcp/message" and
  .mcpServers["eshu-local-compose"].headers.Authorization == "Bearer local-token-1"
' "$CONFIG_FILE" >/dev/null

ESHU_MCP_CONFIG_FILE="$CONFIG_FILE" \
ESHU_LOCAL_MCP_URL="http://127.0.0.1:28081/mcp/message" \
ESHU_LOCAL_MCP_TOKEN="local-token-2" \
ESHU_SKIP_PROBES="true" \
"$REPO_ROOT/scripts/sync_local_compose_mcp.sh"

jq -e '
  (.mcpServers | keys | sort) == ["eshu-e2e", "eshu-local-compose"] and
  .mcpServers["eshu-e2e"].url == "http://10.0.0.5:8081/mcp/message" and
  .mcpServers["eshu-e2e"].headers.Authorization == "Bearer remote-token" and
  .mcpServers["eshu-local-compose"].url == "http://127.0.0.1:28081/mcp/message" and
  .mcpServers["eshu-local-compose"].headers.Authorization == "Bearer local-token-2"
' "$CONFIG_FILE" >/dev/null

echo "sync_local_compose_mcp test passed"
