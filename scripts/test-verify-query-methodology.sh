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
[[ ! -e "$work/static-artifacts/identity.json" ]] || { printf 'static proof claimed a committed paired identity\n' >&2; exit 1; }
[[ -s "$work/static-artifacts/coverage.json" ]] || { printf 'static proof omitted inventory coverage\n' >&2; exit 1; }
guard_pattern='^(TestMethodologyProductionTreeRejectsBaseOnlyMigration|TestMethodologyCombinedCheckoutUsesIntegratedBase)$'
rg -Fq -- "-list $guard_pattern" "$work/static.log" || { printf 'static proof omitted both identity guard roots\n' >&2; exit 1; }
rg -Fq -- "-run $guard_pattern" "$work/static.log" || { printf 'static proof did not execute both identity guard roots\n' >&2; exit 1; }
for omitted in TestMethodologyProductionTreeRejectsBaseOnlyMigration TestMethodologyCombinedCheckoutUsesIntegratedBase; do
 if SHIM_OMIT_TREE_GUARD="$omitted" run_shim "missing-$omitted" --static; then
  printf 'static proof accepted missing guarded root %s\n' "$omitted" >&2
  exit 1
 fi
done
GIT_DIR="$work/missing-git" GIT_WORK_TREE=. GIT_PREFIX=go/internal/query/ run_shim inherited-git-env --static
cmp "$work/static-artifacts/coverage.json" "$work/inherited-git-env-artifacts/coverage.json" || { printf 'hook Git environment changed staged coverage\n' >&2; exit 1; }
# Reproduce the first staged commit on a branch whose HEAD equals origin/main.
scratch="$work/first-commit"
mkdir -p "$scratch/scripts/lib" "$scratch/go" "$scratch/bin"
cp "$repo_root/scripts/verify-query-methodology.sh" "$scratch/scripts/"
cp "$repo_root/scripts/lib/go-test-run-guard.sh" "$scratch/scripts/lib/"
cp "$repo_root/scripts/lib/query-methodology-comparison-identity.py" "$scratch/scripts/lib/"
cp "$work/bin/go" "$work/bin/docker" "$scratch/bin/"
cat > "$scratch/bin/python3" <<'PYTHON_SHIM'
#!/usr/bin/env bash
if [[ "$1" == */verify-query-methodology-live-contract.py ]]; then exit 0; fi
exec /usr/bin/python3 "$@"
PYTHON_SHIM
chmod +x "$scratch/bin/python3"
printf '.proof-artifacts/\n' > "$scratch/.gitignore"
printf 'package proof\n' > "$scratch/go/source.go"
git -C "$scratch" init --quiet --initial-branch=main
git -C "$scratch" config user.name 'Eshu Test'
git -C "$scratch" config user.email 'eshu-test@example.invalid'
git -C "$scratch" add .
git -C "$scratch" commit --quiet -m 'scratch base'
git -C "$scratch" update-ref refs/remotes/origin/main HEAD
git -C "$scratch" switch --quiet -c first-commit
printf '// staged first change\n' >> "$scratch/go/source.go"
git -C "$scratch" add go/source.go
scratch_run() {
 env -u GITHUB_EVENT_NAME -u GITHUB_EVENT_PATH -u GITHUB_SHA -u GITHUB_REF \
  SHIM_LOG="$work/first-commit.log" ESHU_QUERY_METHODOLOGY_ARTIFACT_DIR="$scratch/.proof-artifacts/query-methodology" PATH="$scratch/bin:$PATH" \
  bash "$scratch/scripts/verify-query-methodology.sh" "$1" > "$work/first-commit.out" 2>&1
}
scratch_run --static || { cat "$work/first-commit.out" >&2; printf 'first staged commit failed static proof\n' >&2; exit 1; }
[[ ! -e "$scratch/.proof-artifacts/query-methodology/identity.json" ]] || { printf 'first staged commit claimed paired identity\n' >&2; exit 1; }
printf '// unstaged change\n' >> "$scratch/go/source.go"
if scratch_run --static; then printf 'unstaged bytes passed staged proof\n' >&2; exit 1; fi
git -C "$scratch" restore go/source.go
printf 'package unknown\n' > "$scratch/go/untracked.go"
if scratch_run --static; then printf 'untracked source passed staged proof\n' >&2; exit 1; fi
rm "$scratch/go/untracked.go"
git -C "$scratch" commit --quiet -m 'scratch candidate'
git -C "$scratch" update-ref refs/remotes/origin/main HEAD
# scratch_run strips the ambient GitHub event, so this runs in event-less local
# mode where base resolves to merge-base(S,S) and equals the candidate.
if scratch_run --live; then
 printf 'equal base and candidate passed live proof\n' >&2
 exit 1
fi
rg -Fq 'base equals candidate' "$work/first-commit.out" || { cat "$work/first-commit.out" >&2; printf 'live equal-base rejection lacked cause\n' >&2; exit 1; }
if rg -q '^docker ' "$work/first-commit.log"; then printf 'equal-base live proof reached Docker\n' >&2; exit 1; fi
if rg -q '^docker ' "$work/static.log"; then printf 'static stage touched Docker\n' >&2; exit 1; fi
if SHIM_OMIT=coverage run_shim missing-coverage --static; then printf 'missing coverage passed\n' >&2; exit 1; fi
run_shim stale-coverage --static
if SHIM_OMIT=coverage run_shim stale-coverage --static; then printf 'old coverage concealed omitted producer\n' >&2; exit 1; fi
if ! run_shim live --live; then
 cat "$work/live.out" >&2
 printf 'live methodology shim failed\n' >&2
 exit 1
fi
[[ -s "$work/live-artifacts/identity.json" ]] || { printf 'live proof omitted frozen comparison identity\n' >&2; exit 1; }
for image_id in postgres-bookworm neo4j-community; do
 expected_image="$(jq -er --arg id "$image_id" '.images[] | select(.id == $id) | .destination + "@" + .digest' "$repo_root/specs/ci-images.v1.json")"
 rg -Fq -- "$expected_image" "$work/live.log" || {
  printf 'live methodology runner did not use approved %s image %s\n' "$image_id" "$expected_image" >&2
  exit 1
 }
done
if rg -q 'docker run .* (postgres|neo4j)@sha256:' "$work/live.log"; then
 printf 'live methodology runner pulled an upstream Docker Hub image\n' >&2
 exit 1
fi
for test in TestQueryMethodologyPostgresLive TestImportDependencyMethodologyLive TestImportDependencyMethodologyMCPTerminalCapLive TestQueryplanProfileFlagsUnboundedVarLength TestPilotArtifactFiles; do
 rg -q -- "-run \^$test" "$work/live.log" || { printf 'live stage omitted %s\n' "$test" >&2; exit 1; }
done
for event in zero skip fail; do
 if SHIM_EVENT="$event" run_shim "event-$event" --live; then
  printf 'methodology %s event falsely passed\n' "$event" >&2
  exit 1
 fi
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
for fixture in \
 go/internal/mcp/methodology_live_test.go \
 go/internal/query/methodology_graph_artifact_live_test.go \
 go/internal/query/methodology_graph_cases_live_test.go \
 go/internal/query/methodology_graph_cycle_cap_live_test.go \
 go/internal/query/methodology_graph_fixture_live_test.go \
 go/internal/query/methodology_graph_live_test.go \
 go/internal/query/methodology_graph_measure_live_test.go \
 go/internal/query/methodology_graph_oracle_live_test.go \
 go/internal/query/methodology_graph_profile_live_test.go \
 go/internal/query/methodology_postgres_live_test.go \
 specs/live-tests.d/live-tests-01.yaml \
 scripts/lib/live_tests_registry.py \
 scripts/lib/verify-query-methodology-live-contract.py \
 scripts/test-verify-query-methodology-live-contract.py \
 scripts/lib/query-methodology-comparison-identity.py \
 scripts/test-query-methodology-comparison-identity.py \
 .github/workflows/test.yml \
 .pre-commit-config.yaml; do
 printf '%s\n' "$fixture" > "$work/paths"
 "$work/ci-gates" select --registry "$repo_root/specs/ci-gates.v1.yaml" --tier pre-pr --paths-from "$work/paths" --json > "$work/selection.json"
 for gate in query-methodology-static query-plan-regression; do
  jq -e --arg gate "$gate" 'any(.selected[]; .id == $gate)' "$work/selection.json" >/dev/null || { printf '%s failed to select %s\n' "$fixture" "$gate" >&2; exit 1; }
 done
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
