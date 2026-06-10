#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-hosted-governance-proof.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

if [[ ! -f "${verifier}" ]]; then
	printf 'missing verifier: %s\n' "${verifier}" >&2
	exit 1
fi

bash -n "${verifier}"

list_log="${tmp_dir}/list.log"
bash "${verifier}" --list >"${list_log}"

rg --fixed-strings --quiet "go test ./internal/query" "${list_log}"
rg --fixed-strings --quiet "go test ./internal/mcp" "${list_log}"
rg --fixed-strings --quiet "scripts/test-verify-hosted-security-posture.sh" "${list_log}"
rg --fixed-strings --quiet "scripts/verify-hosted-security-posture.sh" "${list_log}"
rg --fixed-strings --quiet "scripts/test-verify-hosted-network-policy-egress.sh" "${list_log}"
rg --fixed-strings --quiet "scripts/verify-hosted-network-policy-egress.sh" "${list_log}"
rg --fixed-strings --quiet "scoped-token API governance status and redaction canaries" "${list_log}"
rg --fixed-strings --quiet "scoped-token MCP governance parity" "${list_log}"
rg --fixed-strings --quiet "local no-policy governance and no-provider semantic status" "${list_log}"
rg --fixed-strings --quiet "semantic no-provider runtime status" "${list_log}"
rg --fixed-strings --quiet "semantic queue no-provider planning" "${list_log}"
rg --fixed-strings --quiet "go test ./internal/status" "${list_log}"
rg --fixed-strings --quiet "go test ./internal/semanticqueue" "${list_log}"

printf 'hosted governance proof verifier tests passed\n'
