#!/usr/bin/env bash
# Verifies the standalone vulnerability scanner boundary is documented as a
# service contract, not as a second vulnerability engine.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
security_doc="${repo_root}/docs/public/reference/security-intelligence.md"
boundary_doc="${repo_root}/docs/public/reference/standalone-vulnerability-scanner-boundary.md"
source_doc="${repo_root}/docs/public/reference/security-intelligence-source-coverage.md"
release_doc="${repo_root}/docs/public/reference/security-intelligence-release-gate.md"
plan_doc="${repo_root}/docs/internal/security-intelligence-implementation-plan.md"

require_fixed() {
    local file="$1"
    local needle="$2"
    if ! rg --fixed-strings --quiet -- "${needle}" "${file}"; then
        printf 'missing required scanner-boundary text in %s: %s\n' "${file#${repo_root}/}" "${needle}" >&2
        exit 1
    fi
}

require_regex() {
    local file="$1"
    local pattern="$2"
    if ! rg --quiet -- "${pattern}" "${file}"; then
        printf 'missing required scanner-boundary pattern in %s: %s\n' "${file#${repo_root}/}" "${pattern}" >&2
        exit 1
    fi
}

require_fixed "${security_doc}" "Standalone Vulnerability Scanner Boundary"
require_fixed "${boundary_doc}" "# Standalone Vulnerability Scanner Boundary"
require_fixed "${boundary_doc}" "CLI process"
require_fixed "${boundary_doc}" "Local Eshu services"
require_fixed "${boundary_doc}" "Hosted collectors"
require_fixed "${boundary_doc}" "Scanner workers"
require_fixed "${boundary_doc}" "Reducer"
require_fixed "${boundary_doc}" "Read surfaces"
require_fixed "${boundary_doc}" "Heavy analyzers must stay out of the default reducer lane"
require_fixed "${boundary_doc}" "Standalone local mode MUST NOT fork a second vulnerability engine."
require_fixed "${boundary_doc}" "reducer_supply_chain_impact_finding"
require_fixed "${boundary_doc}" "advisory source cache"
require_fixed "${boundary_doc}" "package metadata cache"
require_regex "${boundary_doc}" "API.*MCP.*CLI JSON.*terminal.*SARIF.*VEX"

require_fixed "${source_doc}" "one truth model across local CLI, hosted API, MCP, and future service use"
require_fixed "${source_doc}" "stable scanner artifacts"

require_fixed "${release_doc}" "Standalone local proof"
require_fixed "${release_doc}" "Hosted E2E proof"
require_fixed "${release_doc}" "does not fork a second vulnerability engine"

require_fixed "${plan_doc}" "Chunk 5A: Standalone Scanner Service Boundary"
require_fixed "${plan_doc}" "one truth model across local CLI, hosted API, MCP, and future service use"

printf 'security-intelligence standalone boundary docs verified\n'
