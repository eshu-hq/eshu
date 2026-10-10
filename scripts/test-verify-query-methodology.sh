#!/usr/bin/env bash
# Exercise production runner routing and registry selection with bounded shims.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"
cp "$repo_root/scripts/lib/test-verify-query-methodology-go.sh" "$work/bin/go"
cp "$repo_root/scripts/lib/test-verify-query-methodology-docker.sh" "$work/bin/docker"
chmod +x "$work/bin/go" "$work/bin/docker"
run_shim() {
 local name="$1" mode="$2"
 SHIM_LOG="$work/$name.log" ESHU_QUERY_METHODOLOGY_ARTIFACT_DIR="$work/$name-artifacts" ESHU_SKIP_LIVE_GATE_LOCK=1 PATH="$work/bin:$PATH" bash "$repo_root/scripts/verify-query-methodology.sh" "$mode" > "$work/$name.out" 2>&1
}
run_shim static --static
if rg -q '^docker ' "$work/static.log"; then printf 'static stage touched Docker\n' >&2; exit 1; fi
if SHIM_OMIT=coverage run_shim missing-coverage --static; then printf 'missing coverage passed\n' >&2; exit 1; fi
run_shim stale-coverage --static
if SHIM_OMIT=coverage run_shim stale-coverage --static; then printf 'old coverage concealed omitted producer\n' >&2; exit 1; fi
run_shim live --live
for test in TestQueryMethodologyPostgresLive TestImportDependencyMethodologyLive TestImportDependencyMethodologyMCPTerminalCapLive TestQueryplanProfileFlagsUnboundedVarLength TestPilotArtifactFiles; do
 rg -q -- "-run \^$test" "$work/live.log" || { printf 'live stage omitted %s\n' "$test" >&2; exit 1; }
done
if SHIM_OMIT=postgres run_shim missing --live; then printf 'missing artifact passed\n' >&2; exit 1; fi
run_shim stale --live
if SHIM_OMIT=postgres run_shim stale --live; then printf 'old artifact concealed omitted producer\n' >&2; exit 1; fi
if SHIM_FAIL=postgres run_shim failing --live; then printf 'failed producer passed\n' >&2; exit 1; fi
if SHIM_OMIT=graph-cases run_shim missing-graph-cases --live; then printf 'missing graph cases passed\n' >&2; exit 1; fi
run_shim stale-graph-cases --live
if SHIM_OMIT=graph-cases run_shim stale-graph-cases --live; then printf 'old graph cases concealed omitted producer\n' >&2; exit 1; fi
# The real selector must cover query-only, schema-only and combined tree paths.
(cd "$repo_root/go" && go build -o "$work/ci-gates" ./cmd/ci-gates)
for family in query graph-schema postgres-schema live-helper combined; do
 case "$family" in
  query) printf '%s\n' go/internal/query/cloud_resource_list_store.go > "$work/paths" ;;
  graph-schema) printf '%s\n' go/internal/graph/schema_neo4j.go > "$work/paths" ;;
  postgres-schema) printf '%s\n' go/internal/storage/postgres/migrations/070_cloud_resource_owner_page_index.sql > "$work/paths" ;;
  live-helper) printf '%s\n' scripts/lib/live-gate-lock.sh > "$work/paths" ;;
  combined) printf '%s\n' go/internal/query/cloud_resource_list_store.go go/internal/graph/schema_neo4j.go go/internal/storage/postgres/migrations/070_cloud_resource_owner_page_index.sql > "$work/paths" ;;
 esac
 "$work/ci-gates" select --registry "$repo_root/specs/ci-gates.v1.yaml" --tier pre-pr --paths-from "$work/paths" --json > "$work/selection.json"
 for gate in query-methodology-static query-plan-regression; do
  jq -e --arg gate "$gate" 'any(.selected[]; .id == $gate)' "$work/selection.json" >/dev/null || { printf '%s failed to select %s\n' "$family" "$gate" >&2; exit 1; }
 done
 printf 'selection %s: static and live proof selected\n' "$family"
done
for helper in scripts/extend-read-api-work-budgets.sh scripts/test-extend-read-api-work-budgets.sh; do
 printf '%s\n' "$helper" > "$work/paths"
 "$work/ci-gates" select --registry "$repo_root/specs/ci-gates.v1.yaml" --tier pre-pr --paths-from "$work/paths" --json > "$work/selection.json"
 for gate in read-api-work-budget-mirror; do
  jq -e --arg gate "$gate" 'any(.selected[]; .id == $gate)' "$work/selection.json" >/dev/null || { printf '%s failed to select %s\n' "$helper" "$gate" >&2; exit 1; }
 done
 printf 'selection %s: static mirror selected\n' "$helper"
done
for fixture in scripts/lib/test-verify-query-methodology-go.sh scripts/lib/test-verify-query-methodology-docker.sh; do
 printf '%s\n' "$fixture" > "$work/paths"
 "$work/ci-gates" select --registry "$repo_root/specs/ci-gates.v1.yaml" --tier pre-pr --paths-from "$work/paths" --json > "$work/selection.json"
 for gate in query-methodology-static query-plan-regression; do
  jq -e --arg gate "$gate" 'any(.selected[]; .id == $gate)' "$work/selection.json" >/dev/null || { printf '%s failed to select %s\n' "$fixture" "$gate" >&2; exit 1; }
 done
 printf 'selection %s: static and live proof selected\n' "$fixture"
done
for fixture in scripts/lib/test-methodology-scale-resource-observer-gate.sh scripts/lib/test-methodology-scale-resource-observer-docker.sh scripts/lib/test-methodology-scale-resource-observer-sleep.sh; do
 printf '%s\n' "$fixture" > "$work/paths"
 "$work/ci-gates" select --registry "$repo_root/specs/ci-gates.v1.yaml" --tier pre-pr --paths-from "$work/paths" --json > "$work/selection.json"
 jq -e 'any(.selected[]; .id == "read-api-work-budget-mirror")' "$work/selection.json" >/dev/null || { printf '%s failed to select read-api-work-budget-mirror\n' "$fixture" >&2; exit 1; }
 printf 'selection %s: resource mirror selected\n' "$fixture"
done
printf 'test-verify-query-methodology: pass\n'
