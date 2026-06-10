#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"
list_only=false
runtime=false

usage() {
	cat <<USAGE
Usage: $(basename "$0") [--runtime] [--list]

Runs the hosted governance remote Compose proof gate. By default this composes
the local hosted governance proof with remote Compose render proof. Pass
--runtime only after the remote Compose stack is already running from an
operator-local environment.

Use --list to print the proof commands without running them.
USAGE
}

die() {
	printf 'verify-hosted-governance-remote-compose-proof: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--list)
			list_only=true
			shift
			;;
		--runtime)
			runtime=true
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

command -v bash >/dev/null 2>&1 || die "bash is required"
command -v rg >/dev/null 2>&1 || die "rg is required"

scoped_negative_read_pattern='Test(ResolveEntityScopedSelectorDeniesOutOfScopeCanonicalID|CodeSearchScopedSelectorDeniesOutOfScopeCanonicalID)'

print_step() {
	local label="$1"
	shift
	printf '%s\n  %s\n' "${label}" "$*"
}

if [[ "${list_only}" == "true" ]]; then
	print_step "local hosted governance proof, API/MCP parity prerequisites, and denied/out-of-scope read posture" \
		"scripts/test-verify-hosted-governance-proof.sh && scripts/verify-hosted-governance-proof.sh"
	print_step "scoped negative-read canaries" \
		"go test ./internal/query -run '${scoped_negative_read_pattern}' -count=1"
	print_step "remote Compose render shape" \
		"scripts/test-remote-e2e-hosted-compose-render.sh"
	if [[ "${runtime}" == "true" ]]; then
		print_step "live remote Compose runtime-state proof" \
			"scripts/verify_remote_e2e_runtime_state.sh"
	else
		print_step "live remote Compose runtime proof skipped" \
			"rerun with --runtime after starting the remote Compose stack from a private operator environment"
	fi
	exit 0
fi

run_step() {
	local label="$1"
	shift
	printf '==> %s\n' "${label}"
	"$@"
}

run_step "local hosted governance proof, API/MCP parity prerequisites, and denied/out-of-scope read posture" \
	bash "${repo_root}/scripts/test-verify-hosted-governance-proof.sh"
run_step "local hosted governance proof execution" \
	bash "${repo_root}/scripts/verify-hosted-governance-proof.sh"
(
	cd "${repo_root}/go"
	run_step "scoped negative-read canaries" \
		go test ./internal/query -run "${scoped_negative_read_pattern}" -count=1
)
run_step "remote Compose render shape" \
	bash "${repo_root}/scripts/test-remote-e2e-hosted-compose-render.sh"

if [[ "${runtime}" == "true" ]]; then
	run_step "live remote Compose runtime-state proof" \
		bash "${repo_root}/scripts/verify_remote_e2e_runtime_state.sh"
else
	printf 'live remote Compose runtime proof skipped; rerun with --runtime after starting the remote Compose stack from a private operator environment\n'
fi

printf 'hosted governance remote Compose proof verification passed\n'
