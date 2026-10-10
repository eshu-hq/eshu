#!/usr/bin/env bash
# Production-bound #7881 pilot: deterministic contracts or disposable paired proof.
# Standard and coverage boundary: docs/internal/query-engineering.md.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
mode="${1:---live}"
case "$mode" in --static|--live) ;; *) printf 'usage: %s [--static|--live]\n' "$0" >&2; exit 2 ;; esac
artifact_dir="${ESHU_QUERY_METHODOLOGY_ARTIFACT_DIR:-$repo_root/.proof-artifacts/query-methodology}"
mkdir -p "$artifact_dir"
export ESHU_QUERY_METHODOLOGY_COVERAGE="$artifact_dir/coverage.json"
rm -f "$ESHU_QUERY_METHODOLOGY_COVERAGE"
cd "$repo_root/go"
# shellcheck source=scripts/lib/go-test-run-guard.sh
. "$repo_root/scripts/lib/go-test-run-guard.sh"
python3 "$repo_root/scripts/lib/verify-query-methodology-live-contract.py" --root "$repo_root"
go test ./internal/queryplan -count=1
[[ -s "$ESHU_QUERY_METHODOLOGY_COVERAGE" ]] || { printf 'missing required produced coverage artifact\n' >&2; exit 1; }
go_test_run_guard 3 '^(TestMethodologyRequiredProductionVariants|TestHandlerQueryplanManifestBindsProductionBuilders|TestLegacyQueryplanManifestBindsProductionQueries)$' -- ./internal/query -count=1
if [[ "$mode" == --static ]]; then
 printf 'verify-query-methodology: deterministic production contracts pass\n'
 exit 0
fi
# A successful old file must never conceal a skipped or omitted producer.
rm -f "$artifact_dir/postgresql.json" "$artifact_dir/neo4j.json" "$artifact_dir/neo4j-cases.json"
# Keep measured samples serialized across the shared host.
# shellcheck source=scripts/lib/live-gate-lock.sh
. "$repo_root/scripts/lib/live-gate-lock.sh"
acquire_live_gate_lock
postgres="eshu-methodology-postgres-$$-$RANDOM"
graph="eshu-methodology-neo4j-$$-$RANDOM"
password="methodology-$$-$RANDOM"
cleanup() {
 local status=$?
 docker rm -f "$postgres" "$graph" >/dev/null 2>&1 || true
 release_live_gate_lock
 exit "$status"
}
trap cleanup EXIT INT TERM
postgres_image='postgres@sha256:5a5a84b19854a9ffaa54082c166ff4ec27473a361e496e5ea167f298f2da9722'
graph_image='neo4j@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f'
docker run --rm -d --name "$postgres" -p 127.0.0.1::5432 -e POSTGRES_DB=methodology -e "POSTGRES_PASSWORD=$password" "$postgres_image" >/dev/null
docker run --rm -d --name "$graph" -p 127.0.0.1::7687 -e "NEO4J_AUTH=neo4j/$password" "$graph_image" >/dev/null
ready=false
for _ in {1..60}; do
 if docker exec "$postgres" pg_isready -U postgres -d methodology >/dev/null 2>&1 && docker logs "$graph" 2>&1 | rg -q 'Started\.'; then
  ready=true
  break
 fi
 sleep 2
done
if [[ "$ready" != true ]]; then
 printf 'verify-query-methodology: disposable engines did not become ready\n' >&2
 exit 1
fi
postgres_port="$(docker port "$postgres" 5432/tcp | awk -F: 'NR==1 {print $NF}')"
graph_port="$(docker port "$graph" 7687/tcp | awk -F: 'NR==1 {print $NF}')"
[[ "$postgres_port" =~ ^[0-9]+$ && "$graph_port" =~ ^[0-9]+$ ]] || { printf 'invalid disposable port discovery\n' >&2; exit 1; }
export ESHU_QUERY_METHODOLOGY_POSTGRES_ARTIFACT="$artifact_dir/postgresql.json"
export ESHU_QUERY_METHODOLOGY_GRAPH_ARTIFACT="$artifact_dir/neo4j.json"
export ESHU_QUERY_METHODOLOGY_POSTGRES_IMAGE="$postgres_image"
export ESHU_QUERY_METHODOLOGY_GRAPH_IMAGE="$graph_image"
export ESHU_POSTGRES_TEST_DSN="postgres://postgres:$password@127.0.0.1:$postgres_port/methodology?sslmode=disable"
methodology_live_test_run() {
 local root_name="$1" tag="$2" package="$3" timeout="$4"
 local events="$artifact_dir/${root_name}.events.jsonl"
 if ! go test -json -tags "$tag" -run "^${root_name}$" "$package" -count=1 -timeout="$timeout" > "$events"; then
  cat "$events" >&2
  return 1
 fi
 python3 "$repo_root/scripts/lib/verify-query-methodology-live-contract.py" --events "$events" --root-name "$root_name"
}
methodology_live_test_run TestQueryMethodologyPostgresLive integration ./internal/query 5m
export ESHU_QUERY_METHODOLOGY_LIVE=1 ESHU_QUERYPLAN_PROFILE_ISOLATED=1
export ESHU_QUERYPLAN_PROFILE_LIVE=1
export ESHU_QUERY_METHODOLOGY_CALIBRATED=1
export ESHU_QUERY_METHODOLOGY_REPORT="$artifact_dir/neo4j-cases.json"
export ESHU_NEO4J_URI="bolt://127.0.0.1:$graph_port" ESHU_NEO4J_USERNAME=neo4j ESHU_NEO4J_PASSWORD="$password" ESHU_NEO4J_DATABASE=neo4j
methodology_live_test_run TestImportDependencyMethodologyLive queryplan_profile_live ./internal/query 10m
methodology_live_test_run TestImportDependencyMethodologyMCPTerminalCapLive queryplan_profile_live ./internal/mcp 5m
go_test_run_guard 1 '^TestQueryplanProfileFlagsUnboundedVarLength$' -- -tags queryplan_profile_live ./internal/query -count=1 -v -timeout=5m
[[ -s "$ESHU_QUERY_METHODOLOGY_POSTGRES_ARTIFACT" && -s "$ESHU_QUERY_METHODOLOGY_GRAPH_ARTIFACT" && -s "$ESHU_QUERY_METHODOLOGY_REPORT" ]] || { printf 'missing required produced artifact\n' >&2; exit 1; }
go_test_run_guard 1 '^TestPilotArtifactFiles$' -- ./internal/queryplan -count=1 -v
printf 'verify-query-methodology: paired production evidence passed; artifacts=%s\n' "$artifact_dir"
