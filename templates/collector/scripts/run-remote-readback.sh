#!/usr/bin/env bash
# Remote readback driver: executes one template claim against a target core
# and asserts every emitted stable key reads back through the graph, API, and
# MCP surfaces. This script never reports success it did not measure: every
# surface is required, every key is asserted, and any missing configuration or
# failed assertion exits nonzero.
#
# Required environment (no defaults; an unset surface fails closed):
#   CORE_SUBMIT_URL            POST endpoint accepting one collector SDK result
#   CORE_GRAPH_READ_URL_TEMPLATE  GET URL with %s for the stable key (graph truth)
#   CORE_API_READ_URL_TEMPLATE    GET URL with %s for the stable key (API truth)
#   CORE_MCP_READ_COMMAND         shell command receiving the stable key as $1
#                                 (MCP truth; must exit 0 and echo the key)
# Caller-supplied URLs come from the target core's own http-api reference;
# this template invents no endpoint paths.
set -euo pipefail
cd "$(dirname "$0")/.."
: "${CORE_SUBMIT_URL:?CORE_SUBMIT_URL is not set: remote proof not performed}"
: "${CORE_GRAPH_READ_URL_TEMPLATE:?CORE_GRAPH_READ_URL_TEMPLATE is not set: remote proof not performed}"
: "${CORE_API_READ_URL_TEMPLATE:?CORE_API_READ_URL_TEMPLATE is not set: remote proof not performed}"
: "${CORE_MCP_READ_COMMAND:?CORE_MCP_READ_COMMAND is not set: remote proof not performed}"

result_file="$(mktemp /tmp/template-result.XXXXXX.json)"
trap 'rm -f "$result_file"' EXIT
go run ./cmd/collector --input ./testdata/complete.json > "$result_file"
echo "Submitting claim..."
curl -sS -f -X POST -H 'Content-Type: application/json' \
  --data @"$result_file" "$CORE_SUBMIT_URL" > /dev/null

mapfile -t KEYS < <(python3 -c "import json,sys;print('\n'.join(f['stable_key'] for f in json.load(open(sys.argv[1]))['facts']))" "$result_file")
if [ "${#KEYS[@]}" -eq 0 ]; then
  echo "readback FAILED: collector emitted no facts" >&2
  exit 1
fi

failures=0
for key in "${KEYS[@]}"; do
  graph_url="${CORE_GRAPH_READ_URL_TEMPLATE//\%s/$key}"
  api_url="${CORE_API_READ_URL_TEMPLATE//\%s/$key}"
  graph_body="$(curl -sS -f "$graph_url")" || { echo "readback FAILED: graph read for $key" >&2; failures=1; continue; }
  api_body="$(curl -sS -f "$api_url")" || { echo "readback FAILED: API read for $key" >&2; failures=1; continue; }
  mcp_out="$($CORE_MCP_READ_COMMAND "$key")" || { echo "readback FAILED: MCP read for $key" >&2; failures=1; continue; }
  for surface in "graph:$graph_body" "api:$api_body" "mcp:$mcp_out"; do
    name="${surface%%:*}"
    body="${surface#*:}"
    case "$body" in
      *"$key"*) ;;
      *) echo "readback FAILED: $name response for $key omits the stable key" >&2; failures=1 ;;
    esac
  done
done

if [ "$failures" -ne 0 ]; then
  echo "readback FAILED" >&2
  exit 1
fi
echo "readback passed for ${#KEYS[@]} stable keys through graph, API, and MCP"
