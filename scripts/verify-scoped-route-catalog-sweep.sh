#!/usr/bin/env bash
# Scoped-route full-catalog sweep gate (epic #5161 row #5167): every
# MCP-tool-reachable route answers a covering personal token on a live stack,
# and the only refusals are the documented ledger routes answering the
# route-policy 403 with disclosure.
#
# The sweep itself is scripts/run-auth-mcp-e2e.sh --module catalog-sweep (issue
# #5167): it derives the tool -> route -> class policy from the Go
# route-policy predicates (`go test ./internal/mcp -run TestCatalogSweepPolicy`,
# the same enumeration TestEveryMCPReachableRouteIsScopedOrAnnotated gates
# on -- never a hand-maintained list), boots the standard
# docker-compose.e2e.yaml profile (Postgres + Neo4j + services) on a throwaway
# project, mints a personal token carrying a covering role through the console
# UI, calls every tools/list tool, and writes
# e2e-artifacts/auth-mcp-e2e-catalog-sweep.json. This script runs that module
# and then independently re-judges the machine-readable report: it prints
# exercised/refused/errors counts and exits nonzero on any undocumented 403,
# so a runner-side verdict can never green a bad row silently.
#
# Shares the auth-mcp suite's stack identity (project, 29xxx ports, dev-server
# port 5195 are all owned by scripts/run-auth-mcp-e2e.sh): do not run
# concurrently with that suite. All ESHU_E2E_* overrides pass through.
# ESHU_QUERY_PROFILE overrides the mcp-server query profile for the sweep run
# (default local_full_stack; see above). It is read from the environment by
# docker-compose.e2e.yaml interpolation.
#
# Usage:
#   scripts/verify-scoped-route-catalog-sweep.sh            # live sweep + judge
#   ESHU_SWEEP_JUDGE_ONLY=1 \
#   ESHU_SWEEP_REPORT_JSON=<report> ESHU_SWEEP_POLICY_JSON=<policy> \
#     scripts/verify-scoped-route-catalog-sweep.sh          # judge only
# (judge-only is hermetic: no Docker, no stack, no network. The self-test
# scripts/test-verify-scoped-route-catalog-sweep.sh uses it for RED/GREEN.)
#
# Exit 0 = every exercised route passed and every 403 was a documented,
# disclosed ledger refusal. Nonzero names the offending rows.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

die() {
	printf 'verify-scoped-route-catalog-sweep: %s\n' "$*" >&2
	exit 1
}

# shellcheck source=lib/live-gate-lock.sh
source "${repo_root}/scripts/lib/live-gate-lock.sh"

report="${ESHU_SWEEP_REPORT_JSON:-${repo_root}/e2e-artifacts/auth-mcp-e2e-catalog-sweep.json}"
policy="${ESHU_SWEEP_POLICY_JSON:-${repo_root}/e2e-artifacts/auth-mcp-e2e-catalog-sweep-policy.json}"

runner_rc=0
if [[ "${ESHU_SWEEP_JUDGE_ONLY:-0}" != "1" ]]; then
	# The sweep boots fixed-port containers and is Docker/CPU-heavy: hold the
	# cross-worktree live-gate mutex so a second gate cannot starve this one
	# into a spurious timeout failure. Judge-only mode touches no stack and
	# takes no lock.
	acquire_live_gate_lock
	trap release_live_gate_lock EXIT
	# The sweep must run the full tool surface on both services: an empty
	# profile defaults the servers to production, which hides the
	# capability-gated divergence tools from tools/list (#7768/#7933) and
	# answers 501 on direct-API replays, failing the sweep's coverage check
	# and all-scope controls. local_full_stack advertises and serves them
	# (the capability matrix supports them there) while leaving every other
	# suite on the production default, since only this run exports the
	# variable (docker-compose.e2e.yaml interpolates it, empty by default).
	export ESHU_QUERY_PROFILE="${ESHU_QUERY_PROFILE:-local_full_stack}"
	echo "verify-scoped-route-catalog-sweep: running the live catalog sweep (module catalog-sweep, ESHU_QUERY_PROFILE=${ESHU_QUERY_PROFILE})"
	bash "${repo_root}/scripts/run-auth-mcp-e2e.sh" --module catalog-sweep || runner_rc=$?
	echo "verify-scoped-route-catalog-sweep: sweep module exit ${runner_rc}"
else
	echo "verify-scoped-route-catalog-sweep: judge-only mode, skipping the live stack"
fi

python3 "${repo_root}/scripts/judge-scoped-route-catalog-sweep.py" "$report" "$policy"
judge_rc=$?

if [[ $runner_rc -ne 0 ]]; then
	echo "verify-scoped-route-catalog-sweep: FAIL the sweep module exited ${runner_rc}" >&2
	exit "$runner_rc"
fi
exit "$judge_rc"
