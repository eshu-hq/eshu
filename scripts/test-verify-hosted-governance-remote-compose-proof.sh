#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-hosted-governance-remote-compose-proof.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

if [[ ! -f "${verifier}" ]]; then
	printf 'missing verifier: %s\n' "${verifier}" >&2
	exit 1
fi

bash -n "${verifier}"

list_log="${tmp_dir}/list.log"
bash "${verifier}" --list >"${list_log}"

rg --fixed-strings --quiet "scripts/test-verify-hosted-governance-proof.sh" "${list_log}"
rg --fixed-strings --quiet "scripts/verify-hosted-governance-proof.sh" "${list_log}"
rg --fixed-strings --quiet "go test ./internal/query -run 'Test(ResolveEntityScopedSelectorDeniesOutOfScopeCanonicalID|CodeSearchScopedSelectorDeniesOutOfScopeCanonicalID)' -count=1" "${list_log}"
rg --fixed-strings --quiet "scripts/test-remote-e2e-hosted-compose-render.sh" "${list_log}"
rg --fixed-strings --quiet "remote Compose render shape" "${list_log}"
rg --fixed-strings --quiet "API/MCP parity prerequisites" "${list_log}"
rg --fixed-strings --quiet "denied/out-of-scope read posture" "${list_log}"
rg --fixed-strings --quiet "live remote Compose runtime proof skipped" "${list_log}"
rg --fixed-strings --quiet "scripts/test-verify-two-team-governance-proof.sh" "${list_log}"

runtime_list_log="${tmp_dir}/runtime-list.log"
bash "${verifier}" --list --runtime >"${runtime_list_log}"
rg --fixed-strings --quiet "scripts/verify_remote_e2e_runtime_state.sh" "${runtime_list_log}"
rg --fixed-strings --quiet "live remote Compose runtime-state proof" "${runtime_list_log}"
rg --fixed-strings --quiet "scripts/run-two-team-governance-proof.sh" "${runtime_list_log}"
rg --fixed-strings --quiet "live two-team scoped cross-scope denial proof" "${runtime_list_log}"

if bash "${verifier}" --unknown >"${tmp_dir}/unknown.out" 2>"${tmp_dir}/unknown.err"; then
	printf 'expected unknown option to fail\n' >&2
	exit 1
fi
rg --fixed-strings --quiet "unknown option: --unknown" "${tmp_dir}/unknown.err"

printf 'hosted governance remote Compose proof verifier tests passed\n'
