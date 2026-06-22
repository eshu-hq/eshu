#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"
list_only=false

usage() {
	cat <<USAGE
Usage: $(basename "$0") [--list]

Runs the local hosted governance proof gate. This gate composes focused API,
MCP, redaction, audit, Helm security posture, and NetworkPolicy egress checks
without requiring remote hosts, live providers, private values, or a cluster.

Use --list to print the proof commands without running them.
USAGE
}

die() {
	printf 'verify-hosted-governance-proof: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--list)
			list_only=true
			shift
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			die "unknown option: $1"
			;;
	esac
done

command -v go >/dev/null 2>&1 || die "go is required"
command -v rg >/dev/null 2>&1 || die "rg is required"
command -v bash >/dev/null 2>&1 || die "bash is required"

query_pattern='Test(AuthMiddleware_GovernanceStatusRequiresAuth|AuthMiddlewareWithScopedTokens(RejectsUnsupportedScopedRoute|AuditsUnsupportedScopedRoute|AllowsGovernanceStatusRoute|AllowsSemanticExtractionStatusRoute|AllowsComponentExtensionRoutes|AllowsCollectorStatusRoute|AllowsIngesterStatusRoutes|AllowsHostedReadinessRoute)|StatusHandlerGovernance|GovernanceStatusReadsPrivateAuditSinkAggregates)'
mcp_pattern='Test(HostedGovernanceRuntimeToolRoutesToStatus|DispatchToolGovernanceStatusAllowsScopedRoute|DispatchToolSemanticExtractionStatusAllowsScopedRoute|DispatchToolComponentExtensionsAllowsScopedRoutes|DispatchToolCollectorStatusAllowsScopedRoute|DispatchToolIngesterStatusAllowsScopedRoutes|DispatchToolHostedReadinessAllowsScopedRoute)'
local_no_policy_query_pattern='Test(StatusHandlerGovernanceLocalNoPolicyReturnsEnvelope|StatusHandlerSemanticExtractionNoProviderReturnsEnvelope|StatusIndexIncludesSemanticExtractionNoProvider)'
semantic_status_pattern='Test(SemanticExtractionDefaultsUnavailableWithoutAffectingHealth|RenderStatusIncludesSemanticExtractionNoProvider)'
semantic_queue_pattern='TestPlan(NoProviderModeCreatesNoProviderJobs|ZeroValueProviderFailsClosedToNoProvider)'

print_step() {
	local label="$1"
	shift
	printf '%s\n  %s\n' "${label}" "$*"
}

if [[ "${list_only}" == "true" ]]; then
	print_step "scoped-token API governance status and redaction canaries" \
		"go test ./internal/query -run '${query_pattern}' -count=1"
	print_step "local no-policy governance and no-provider semantic status" \
		"go test ./internal/query -run '${local_no_policy_query_pattern}' -count=1"
	print_step "semantic no-provider runtime status" \
		"go test ./internal/status -run '${semantic_status_pattern}' -count=1"
	print_step "semantic queue no-provider planning" \
		"go test ./internal/semanticqueue -run '${semantic_queue_pattern}' -count=1"
	print_step "scoped-token MCP governance parity" \
		"go test ./internal/mcp -run '${mcp_pattern}' -count=1"
	print_step "hosted governance retention-state proof self-test" \
		"scripts/test-verify-hosted-governance-retention-proof.sh"
	print_step "hosted auth audit and revocation proof self-test" \
		"scripts/test-verify-hosted-auth-audit-proof.sh"
	print_step "two-team scoped cross-scope denial proof verifier self-test" \
		"scripts/test-verify-two-team-governance-proof.sh"
	print_step "live K8s two-team scoped cross-scope denial proof verifier self-test" \
		"scripts/test-verify-k8s-two-team-governance-proof.sh"
	print_step "hosted security posture verifier self-test" \
		"scripts/test-verify-hosted-security-posture.sh"
	print_step "hosted security posture Helm render proof" \
		"scripts/verify-hosted-security-posture.sh"
	print_step "hosted NetworkPolicy egress verifier self-test" \
		"scripts/test-verify-hosted-network-policy-egress.sh"
	print_step "hosted NetworkPolicy egress render proof" \
		"scripts/verify-hosted-network-policy-egress.sh"
	exit 0
fi

run_step() {
	local label="$1"
	shift
	printf '==> %s\n' "${label}"
	"$@"
}

(
	cd "${repo_root}/go"
	run_step "scoped-token API governance status and redaction canaries" \
		go test ./internal/query -run "${query_pattern}" -count=1
	run_step "local no-policy governance and no-provider semantic status" \
		go test ./internal/query -run "${local_no_policy_query_pattern}" -count=1
	run_step "semantic no-provider runtime status" \
		go test ./internal/status -run "${semantic_status_pattern}" -count=1
	run_step "semantic queue no-provider planning" \
		go test ./internal/semanticqueue -run "${semantic_queue_pattern}" -count=1
	run_step "scoped-token MCP governance parity" \
		go test ./internal/mcp -run "${mcp_pattern}" -count=1
)

run_step "hosted security posture verifier self-test" \
	bash "${repo_root}/scripts/test-verify-hosted-security-posture.sh"
run_step "hosted governance retention-state proof self-test" \
	bash "${repo_root}/scripts/test-verify-hosted-governance-retention-proof.sh"
run_step "hosted auth audit and revocation proof self-test" \
	bash "${repo_root}/scripts/test-verify-hosted-auth-audit-proof.sh"
run_step "two-team scoped cross-scope denial proof verifier self-test" \
	bash "${repo_root}/scripts/test-verify-two-team-governance-proof.sh"
run_step "live K8s two-team scoped cross-scope denial proof verifier self-test" \
	bash "${repo_root}/scripts/test-verify-k8s-two-team-governance-proof.sh"
run_step "hosted security posture Helm render proof" \
	bash "${repo_root}/scripts/verify-hosted-security-posture.sh"
run_step "hosted NetworkPolicy egress verifier self-test" \
	bash "${repo_root}/scripts/test-verify-hosted-network-policy-egress.sh"
run_step "hosted NetworkPolicy egress render proof" \
	bash "${repo_root}/scripts/verify-hosted-network-policy-egress.sh"

printf 'hosted governance proof verification passed\n'
