#!/usr/bin/env bash
#
# verify-mcp-client-auth-doc.sh - lockstep guard: fail when
# docs/public/operate/mcp-client-auth.md drops one of the literal strings its
# client matrix promises (issue #5169, F-8).
#
# This is one half of the lockstep guard: go/cmd/eshu's
# TestDocLockstepLiterals pins the same four literals against the ACTUAL
# `eshu mcp setup` rendered output (hardcoded, not built from the
# mcpTokenEnvVar/apiKeyEnvVar constants, so a constant-value rename without a
# doc update fails that test too). This script checks the doc's own copy of
# the same literals. Neither side alone catches a rename in only one place;
# together they do -- cheap and honest per the design's own framing, not a
# structural cross-reference.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
doc_path="${repo_root}/docs/public/operate/mcp-client-auth.md"

if [ ! -f "${doc_path}" ]; then
  echo "verify-mcp-client-auth-doc: missing ${doc_path}" >&2
  exit 1
fi

missing=0
checked=0

check() {
  local literal="$1"
  checked=$((checked + 1))
  if ! rg -q --fixed-strings -- "${literal}" "${doc_path}"; then
    echo "verify-mcp-client-auth-doc: doc missing pinned literal: ${literal}" >&2
    missing=$((missing + 1))
  fi
}

check '${ESHU_MCP_TOKEN}'
check '/mcp/message'
check 'bearer_token_env_var = "ESHU_MCP_TOKEN"'
check 'WARNING: the shared ESHU_API_KEY is an admin/dev credential: full AllScopes'

if [ "${missing}" -ne 0 ]; then
  echo "verify-mcp-client-auth-doc: FAILED (${missing}/${checked} pinned literal(s) missing)" >&2
  exit 1
fi

echo "verify-mcp-client-auth-doc: OK: ${checked} literal(s) checked"
