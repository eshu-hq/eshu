#!/usr/bin/env bash
# Verify the MCP-identity E2E runner's report against the named baseline
# manifest (F-9, issue #5170). Run after scripts/run-auth-mcp-e2e.sh writes
# e2e-artifacts/auth-mcp-e2e-report.json: it asserts the exact ordered step-id
# list, per-step status, and runtime bound match
# testdata/golden/auth-mcp-e2e-baseline.json. A drift (added, removed,
# reordered, or newly-failing step, or a runtime-bound breach) fails here.
#
# Recapture the baseline with scripts/refresh-auth-mcp-e2e-baseline.sh.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# shellcheck source=scripts/lib/auth_mcp_e2e_manifest.sh
source "${repo_root}/scripts/lib/auth_mcp_e2e_manifest.sh"

baseline="${1:-${repo_root}/testdata/golden/auth-mcp-e2e-baseline.json}"
report="${2:-${repo_root}/e2e-artifacts/auth-mcp-e2e-report.json}"

validate_auth_mcp_e2e_manifest "${baseline}" "${report}"
