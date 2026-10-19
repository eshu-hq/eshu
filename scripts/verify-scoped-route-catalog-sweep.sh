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

python3 - "$report" "$policy" <<'EOF'
import json
import sys

report_path, policy_path = sys.argv[1], sys.argv[2]
failures = []


def load(path, what):
    try:
        with open(path) as fh:
            return json.load(fh)
    except (OSError, ValueError) as err:
        failures.append(f"{what} unreadable at {path}: {err}")
        return None


policy = load(policy_path, "policy")
report = load(report_path, "report")

refusal_actuals = {
    "route_denied_403",
    "route_denied_403 (disclosed)",
    "route_denied_403 (undisclosed)",
    "http_403",
}
ledger_classes = {"shared_key_only", "pending_row_filtering"}


def is_refusal(actual):
    return actual in refusal_actuals or actual.startswith("http_403")


if policy is not None and report is not None:
    policy_rows = {(r["tool"], r["label"]): r for r in policy.get("rows", [])}
    results = report.get("results", [])
    seen = set()
    exercised = len(results)
    routes = set()
    refused = {"shared_key_only": 0, "pending_row_filtering": 0}
    strict = tolerant = errors = 0
    for res in results:
        key = (res.get("tool"), res.get("label"))
        seen.add(key)
        row = policy_rows.get(key)
        if row is None:
            failures.append(f'{res.get("tool")}[{res.get("label")}] has no policy row')
            errors += 1
            continue
        routes.add(res.get("route", ""))
        actual = res.get("actual", "")
        passed = res.get("pass") is True
        cls = row.get("class", "")
        if cls in ledger_classes:
            if passed and "(disclosed)" in actual:
                refused[cls] += 1
            else:
                failures.append(
                    f'{row["tool"]}[{row["label"]}] {row["method"]} {row["path"]} '
                    f"({cls}) refused without the documented disclosed 403: "
                    f"actual={actual!r} pass={passed}")
                errors += 1
        elif is_refusal(actual):
            failures.append(
                f'{row["tool"]}[{row["label"]}] {row["method"]} {row["path"]} '
                f"is allowlisted but answered {actual!r}: undocumented 403")
            errors += 1
        elif not passed:
            failures.append(
                f'{row["tool"]}[{row["label"]}] {row["method"]} {row["path"]}: '
                f'expected {res.get("expected")!r}, got {actual!r} -- {res.get("detail", "")}')
            errors += 1
        elif row.get("accept", ["ok"]) == ["ok"]:
            strict += 1
        else:
            tolerant += 1
    for key, row in sorted(policy_rows.items()):
        if key not in seen:
            failures.append(
                f'{row["tool"]}[{row["label"]}] {row["method"]} {row["path"]} '
                "has a policy row but no sweep result")
            errors += 1
    refused_total = refused["shared_key_only"] + refused["pending_row_filtering"]
    summary = (f"exercised={exercised} routes={len(routes)} "
               f"refused={refused_total} strict={strict} "
               f"tolerant={tolerant} errors={errors}")
    if failures:
        print(f"verify-scoped-route-catalog-sweep: FAIL {summary}")
        for line in failures:
            print(f"  {line}")
        print(f"  report: {report_path}")
        print("  policy: Go-derived (TestCatalogSweepPolicy), not a hand list")
        sys.exit(1)
    print(f"verify-scoped-route-catalog-sweep: PASS {summary}")
    print(f"  refused disclosed: shared_key_only={refused['shared_key_only']} "
          f"pending_row_filtering={refused['pending_row_filtering']}")
    print(f"  report: {report_path}")
else:
    print("verify-scoped-route-catalog-sweep: FAIL unreadable artifact")
    for line in failures:
        print(f"  {line}")
    sys.exit(1)
EOF
judge_rc=$?

if [[ $runner_rc -ne 0 ]]; then
	echo "verify-scoped-route-catalog-sweep: FAIL the sweep module exited ${runner_rc}" >&2
	exit "$runner_rc"
fi
exit "$judge_rc"
