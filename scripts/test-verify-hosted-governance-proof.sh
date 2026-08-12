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

# #6055: the --list output now names the go_test_run_guard invocation (which
# asserts a minimum matched-test count before running `go test -run`) rather
# than a bare `go test -run` command — assert the new shape, not merely a
# relaxed substring of the old one, so this self-test still proves the
# printed command is the one actually executed below.
rg --fixed-strings --quiet "go_test_run_guard 12" "${list_log}"
rg --fixed-strings --quiet -- "-- ./internal/query -count=1" "${list_log}"
rg --fixed-strings --quiet "go_test_run_guard 7" "${list_log}"
rg --fixed-strings --quiet -- "-- ./internal/mcp -count=1" "${list_log}"
rg --fixed-strings --quiet "scripts/test-verify-hosted-security-posture.sh" "${list_log}"
rg --fixed-strings --quiet "scripts/test-verify-hosted-governance-retention-proof.sh" "${list_log}"
rg --fixed-strings --quiet "scripts/test-verify-hosted-auth-audit-proof.sh" "${list_log}"
rg --fixed-strings --quiet "scripts/verify-hosted-security-posture.sh" "${list_log}"
rg --fixed-strings --quiet "scripts/test-verify-hosted-network-policy-egress.sh" "${list_log}"
rg --fixed-strings --quiet "scripts/verify-hosted-network-policy-egress.sh" "${list_log}"
rg --fixed-strings --quiet "scoped-token API governance status and redaction canaries" "${list_log}"
rg --fixed-strings --quiet "scoped-token MCP governance parity" "${list_log}"
rg --fixed-strings --quiet "local no-policy governance and no-provider semantic status" "${list_log}"
rg --fixed-strings --quiet "semantic no-provider runtime status" "${list_log}"
rg --fixed-strings --quiet "semantic queue no-provider planning" "${list_log}"
rg --fixed-strings --quiet "hosted governance retention-state proof self-test" "${list_log}"
rg --fixed-strings --quiet "scripts/test-verify-two-team-governance-proof.sh" "${list_log}"
rg --fixed-strings --quiet "go_test_run_guard 2" "${list_log}"
rg --fixed-strings --quiet -- "-- ./internal/status -count=1" "${list_log}"
rg --fixed-strings --quiet -- "-- ./internal/semanticqueue -count=1" "${list_log}"

printf 'hosted governance proof verifier tests passed\n'
