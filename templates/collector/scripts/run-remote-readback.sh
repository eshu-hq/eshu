#!/usr/bin/env bash
# Remote readback driver starter: executes one claim through the collector
# stdio contract, then reads the emitted stable keys back through the core
# graph, API, and MCP surfaces. Fill in CORE_API_BASE and CORE_MCP_ADDR for
# the target core before using this in a cutover issue.
set -euo pipefail
cd "$(dirname "$0")/.."
: "${CORE_API_BASE:?set CORE_API_BASE to the target core API base URL}"
: "${CORE_MCP_ADDR:?set CORE_MCP_ADDR to the target core MCP address}"
go run ./cmd/collector --input ./testdata/complete.json > /tmp/template-result.json
echo "collected $(python3 -c "import json;print(len(json.load(open('/tmp/template-result.json'))['facts']))") facts; read back stable keys via:"
echo "  graph/API: ${CORE_API_BASE}"
echo "  mcp: ${CORE_MCP_ADDR}"
