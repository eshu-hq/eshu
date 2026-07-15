#!/usr/bin/env bash
# Console live E2E gate (issue #3326).
#
# Runs the PRIVATE/LIVE Eshu console against an ALREADY-RUNNING local Compose
# stack and proves every major route renders real data or an explicit
# empty/unavailable state with no demo fallback, no unhandled console errors,
# and no unexpected failed network requests.
#
# Prerequisite: the local stack is up and healthy with retained data plus a
# fresh, isolated identity surface for the default browser-session gate. Bring
# it up with the gitignored env file first (see apps/console/README.md), e.g.:
#   docker compose -p eshu-3326-e2e --env-file e2e-artifacts/.env.console-e2e \
#     -f docker-compose.yaml up --build -d
#
# This script does NOT manage Docker; it only drives the browser gate so the
# stack lifecycle stays explicit and operator-controlled.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

api_key="${ESHU_E2E_API_KEY:-${ESHU_API_KEY:-}}"
auth_mode="${ESHU_E2E_AUTH_MODE:-browser_session}"
http_port="${ESHU_HTTP_PORT:-8080}"
api_base="${ESHU_E2E_API_BASE:-http://127.0.0.1:${http_port}}"

if [[ "$auth_mode" == "bearer" && -z "$api_key" ]]; then
  echo "run-console-live-e2e: bearer mode requires ESHU_API_KEY or ESHU_E2E_API_KEY." >&2
  exit 1
fi

echo "run-console-live-e2e: typechecking the gate"
npx tsc -p apps/console/e2e/tsconfig.json

echo "run-console-live-e2e: probing live readiness at $api_base"
for endpoint in /healthz /readyz; do
  code="$(curl -sS -m 5 -o /dev/null -w '%{http_code}' "${api_base}${endpoint}" || true)"
  if [[ "$code" != "200" ]]; then
    echo "run-console-live-e2e: ${api_base}${endpoint} returned ${code}; stack not ready" >&2
    exit 1
  fi
done

echo "run-console-live-e2e: running the browser gate"
# Load the TypeScript runner through Vite (scripts/console-live-e2e-runtime.mjs)
# so the gate runs on the repo's supported Node range without native TS stripping
# (Node >= 23.6 only) or an extra dependency.
ESHU_E2E_AUTH_MODE="$auth_mode" ESHU_E2E_API_KEY="$api_key" ESHU_E2E_API_BASE="$api_base" \
  node scripts/console-live-e2e-runtime.mjs
