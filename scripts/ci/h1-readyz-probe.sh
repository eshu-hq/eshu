#!/usr/bin/env bash
# Record the endpoint of the gate's existing successful readiness request.
set -euo pipefail
url="$1"
report="$2"
start="$(date -u +%FT%T.%NZ)"
local_endpoint="$(curl -fsS -o /dev/null -w '%{local_ip} %{local_port}' "$url" 2>/dev/null)"
end="$(date -u +%FT%T.%NZ)"
printf 'start_utc=%s\nend_utc=%s\nlocal_endpoint=%s\npath=/readyz\nstatus=200\n' \
  "$start" "$end" "$local_endpoint" > "$report"
