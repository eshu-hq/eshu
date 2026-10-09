#!/usr/bin/env bash
# Exercise production runner routing and registry selection with bounded shims.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"
cat > "$work/bin/go" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$SHIM_LOG"
if [[ "$*" == *'-list '* ]]; then
 printf '%s\n' TestMethodologyRequiredProductionVariants TestHandlerQueryplanManifestBindsProductionBuilders TestLegacyQueryplanManifestBindsProductionQueries
 exit 0
fi
case "$*" in
 *'-run ^TestQueryMethodologyPostgresLive$'*)
  [[ -n "$ESHU_POSTGRES_TEST_DSN" && -n "$ESHU_QUERY_METHODOLOGY_POSTGRES_IMAGE" ]] || exit 11
  [[ "${SHIM_FAIL:-}" != postgres ]] || exit 12
  if [[ "${SHIM_OMIT:-}" != postgres ]]; then printf '{"engine":"postgresql"}\n' > "$ESHU_QUERY_METHODOLOGY_POSTGRES_ARTIFACT"; fi ;;
 *'-run ^TestImportDependencyMethodologyLive$'*)
  [[ "$ESHU_QUERY_METHODOLOGY_LIVE" == 1 && "$ESHU_QUERYPLAN_PROFILE_ISOLATED" == 1 && "$ESHU_QUERY_METHODOLOGY_CALIBRATED" == 1 ]] || exit 13
  printf '{"engine":"neo4j"}\n' > "$ESHU_QUERY_METHODOLOGY_GRAPH_ARTIFACT" ;;
 *'-run ^TestImportDependencyMethodologyMCPTerminalCapLive$'*)
  [[ -n "$ESHU_NEO4J_URI" ]] || exit 14 ;;
esac
SH
cat > "$work/bin/docker" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf 'docker %s\n' "$*" >> "$SHIM_LOG"
case "$1" in logs) printf 'Started.\n' ;; port) printf '127.0.0.1:12345\n' ;; esac
SH
chmod +x "$work/bin/go" "$work/bin/docker"
run_shim() {
 local name="$1" mode="$2"
 SHIM_LOG="$work/$name.log" ESHU_QUERY_METHODOLOGY_ARTIFACT_DIR="$work/$name-artifacts" ESHU_SKIP_LIVE_GATE_LOCK=1 PATH="$work/bin:$PATH" bash "$repo_root/scripts/verify-query-methodology.sh" "$mode" > "$work/$name.out" 2>&1
}
run_shim static --static
if rg -q '^docker ' "$work/static.log"; then printf 'static stage touched Docker\n' >&2; exit 1; fi
run_shim live --live
for test in TestQueryMethodologyPostgresLive TestImportDependencyMethodologyLive TestImportDependencyMethodologyMCPTerminalCapLive TestQueryplanProfileFlagsUnboundedVarLength TestPilotArtifactFiles; do
 rg -q -- "-run \^$test" "$work/live.log" || { printf 'live stage omitted %s\n' "$test" >&2; exit 1; }
done
if SHIM_OMIT=postgres run_shim missing --live; then printf 'missing artifact passed\n' >&2; exit 1; fi
run_shim stale --live
if SHIM_OMIT=postgres run_shim stale --live; then printf 'old artifact concealed omitted producer\n' >&2; exit 1; fi
if SHIM_FAIL=postgres run_shim failing --live; then printf 'failed producer passed\n' >&2; exit 1; fi
# The real selector must cover query-only, schema-only and combined tree paths.
(cd "$repo_root/go" && go build -o "$work/ci-gates" ./cmd/ci-gates)
for family in query graph-schema postgres-schema combined; do
 case "$family" in
  query) printf '%s\n' go/internal/query/cloud_resource_list_store.go > "$work/paths" ;;
  graph-schema) printf '%s\n' go/internal/graph/schema_neo4j.go > "$work/paths" ;;
  postgres-schema) printf '%s\n' go/internal/storage/postgres/migrations/070_cloud_resource_owner_page_index.sql > "$work/paths" ;;
  combined) printf '%s\n' go/internal/query/cloud_resource_list_store.go go/internal/graph/schema_neo4j.go go/internal/storage/postgres/migrations/070_cloud_resource_owner_page_index.sql > "$work/paths" ;;
 esac
 "$work/ci-gates" select --registry "$repo_root/specs/ci-gates.v1.yaml" --tier pre-pr --paths-from "$work/paths" --json > "$work/selection.json"
 for gate in query-methodology-static query-plan-regression; do
  jq -e --arg gate "$gate" 'any(.selected[]; .id == $gate)' "$work/selection.json" >/dev/null || { printf '%s failed to select %s\n' "$family" "$gate" >&2; exit 1; }
 done
 printf 'selection %s: static and live proof selected\n' "$family"
done
printf 'test-verify-query-methodology: pass\n'
