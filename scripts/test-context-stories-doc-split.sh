#!/usr/bin/env bash
# Verify the three-page context/story reference contract and its failure modes.
# This guards route ownership, legacy anchors, navigation, shared-contract
# placement, and the tighter per-page maintainability limit introduced by #5947.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fail() {
  printf 'context-stories-doc-split: %s\n' "$*" >&2
  return 1
}

count_fixed() {
  local text="$1"
  shift
  local output code
  set +e
  output="$(rg -o --no-filename --fixed-strings "$text" "$@" 2>/dev/null)"
  code=$?
  set -e
  if [ "$code" -gt 1 ]; then
    fail "rg failed while counting: ${text}"
    return 1
  fi
  printf '%s\n' "$output" | awk 'NF { count++ } END { print count + 0 }'
}

require_count() {
  local expected="$1"
  local text="$2"
  shift 2
  local actual
  actual="$(count_fixed "$text" "$@")" || return 1
  [ "$actual" -eq "$expected" ] \
    || fail "expected ${expected} occurrence(s) of ${text}; found ${actual}"
}

verify_layout() {
  local root="$1"
  local docs_dir="${root}/docs/public/reference/http-api"
  local hub="${docs_dir}/context-and-stories.md"
  local stories="${docs_dir}/story-routes.md"
  local deployment="${docs_dir}/deployment-trace-and-influence.md"
  local mkdocs="${root}/docs/mkdocs.yml"
  local route_table="${root}/docs/public/reference/http-api.md"
  local file lines heading term route

  for file in "$hub" "$stories" "$deployment" "$mkdocs" "$route_table"; do
    [ -f "$file" ] || fail "missing ${file#"${root}/"}" || return 1
  done

  for file in "$hub" "$stories" "$deployment"; do
    lines="$(wc -l <"$file" | tr -d ' ')"
    [ "$lines" -le 450 ] \
      || fail "${file#"${root}/"} has ${lines} lines; limit is 450" || return 1
  done

  legacy_headings=(
    '## Route Map'
    '## Entity Resolution'
    '## Context'
    '### Deployment Trace Relationship Endpoints'
    '### Tech Fingerprint Rollup'
    '## Incident Context'
    '## Work-Item Evidence'
    '## Catalog'
    '## Service Intelligence Report'
    '## Stories'
    '## Service Investigation'
    '## Documentation Generation Flow'
  )
  for heading in "${legacy_headings[@]}"; do
    require_count 1 "$heading" "$hub" || return 1
  done

  require_count 1 'Context And Stories: reference/http-api/context-and-stories.md' "$mkdocs" || return 1
  require_count 1 'Story Routes: reference/http-api/story-routes.md' "$mkdocs" || return 1
  require_count 1 'Deployment Trace And Influence: reference/http-api/deployment-trace-and-influence.md' "$mkdocs" || return 1

  require_count 1 '(http-api/context-and-stories.md)' "$route_table" || return 1
  require_count 1 '(http-api/story-routes.md)' "$route_table" || return 1
  require_count 1 '(http-api/deployment-trace-and-influence.md)' "$route_table" || return 1

  hub_routes=(
    'POST /api/v0/entities/resolve'
    'GET /api/v0/entities/{entity_id}/context'
    'GET /api/v0/workloads/{workload_id}/context'
    'GET /api/v0/services/{service_name}/context'
    'GET /api/v0/repositories/{repo_id}/context'
    'GET /api/v0/incidents/{incident_id}/context'
    'GET /api/v0/work-items/evidence'
    'GET /api/v0/catalog'
  )
  story_routes=(
    'GET /api/v0/repositories/{repo_id}/story'
    'GET /api/v0/workloads/{workload_id}/story'
    'GET /api/v0/services/{service_name}/story'
    'GET /api/v0/services/{service_name}/intelligence-report'
    'GET /api/v0/investigations/services/{service_name}'
  )
  deployment_routes=(
    'POST /api/v0/impact/trace-deployment-chain'
    'POST /api/v0/impact/deployment-config-influence'
  )

  for route in "${hub_routes[@]}"; do
    require_count 1 "$route" "$hub" || return 1
    require_count 1 "$route" "$hub" "$stories" "$deployment" || return 1
  done
  for route in "${story_routes[@]}"; do
    require_count 1 "$route" "$stories" || return 1
    require_count 1 "$route" "$hub" "$stories" "$deployment" || return 1
  done
  for route in "${deployment_routes[@]}"; do
    require_count 1 "$route" "$deployment" || return 1
    require_count 1 "$route" "$hub" "$stories" "$deployment" || return 1
  done

  require_count 1 '## Shared Response Contract' "$hub" || return 1
  require_count 1 '(context-and-stories.md#shared-response-contract)' "$stories" || return 1
  require_count 1 '(context-and-stories.md#shared-response-contract)' "$deployment" || return 1

  shared_terms=(
    'result_limits'
    'partial_reasons'
    'limitations'
    'evidence_boundaries'
    'entrypoint_candidates'
    'dependents_truncated'
    'consumer_repositories_truncated'
    'provisioning_source_chains_truncated'
  )
  for term in "${shared_terms[@]}"; do
    require_count 0 "$term" "$stories" "$deployment" || return 1
    actual="$(count_fixed "$term" "$hub")" || return 1
    [ "$actual" -gt 0 ] || fail "shared hub is missing ${term}" || return 1
  done

  require_count 1 '(story-routes.md)' "$hub" || return 1
  require_count 1 '(deployment-trace-and-influence.md)' "$hub" || return 1
  require_count 1 '(deployment-trace-and-influence.md)' "$stories" || return 1
  require_count 1 '(story-routes.md)' "$deployment" || return 1
}

seed_mutation_repo() {
  local name="$1"
  local root="${tmp_root}/${name}"
  mkdir -p "${root}/docs/public/reference/http-api" "${root}/scripts"
  cp "${repo_root}/docs/public/reference/http-api/context-and-stories.md" \
    "${root}/docs/public/reference/http-api/"
  cp "${repo_root}/docs/public/reference/http-api/story-routes.md" \
    "${root}/docs/public/reference/http-api/"
  cp "${repo_root}/docs/public/reference/http-api/deployment-trace-and-influence.md" \
    "${root}/docs/public/reference/http-api/"
  cp "${repo_root}/docs/public/reference/http-api.md" "${root}/docs/public/reference/"
  cp "${repo_root}/docs/mkdocs.yml" "${root}/docs/"
  printf '%s\n' "$root"
}

expect_mutation_failure() {
  local label="$1"
  local root="$2"
  if verify_layout "$root" >/dev/null 2>&1; then
    fail "mutation did not fail: ${label}"
    exit 1
  fi
  printf 'ok - %s\n' "$label"
}

verify_layout "$repo_root"
printf 'ok - real three-page route and anchor contract\n'

mutation_root="$(seed_mutation_repo oversize)"
awk 'BEGIN { for (i = 1; i <= 451; i++) print "extra line" }' \
  >>"${mutation_root}/docs/public/reference/http-api/context-and-stories.md"
expect_mutation_failure "oversized page is rejected" "$mutation_root"

mutation_root="$(seed_mutation_repo missing-nav)"
sed -i.bak '/Story Routes: reference\/http-api\/story-routes.md/d' \
  "${mutation_root}/docs/mkdocs.yml"
expect_mutation_failure "missing navigation entry is rejected" "$mutation_root"

mutation_root="$(seed_mutation_repo missing-anchor)"
sed -i.bak '/^## Stories$/d' \
  "${mutation_root}/docs/public/reference/http-api/context-and-stories.md"
expect_mutation_failure "missing legacy heading stub is rejected" "$mutation_root"

mutation_root="$(seed_mutation_repo duplicate-route)"
printf '%s\n' 'POST /api/v0/impact/trace-deployment-chain' \
  >>"${mutation_root}/docs/public/reference/http-api/story-routes.md"
expect_mutation_failure "route duplicated into the wrong owner is rejected" "$mutation_root"

mutation_root="$(seed_mutation_repo duplicated-shared-contract)"
printf '%s\n' 'partial_reasons' \
  >>"${mutation_root}/docs/public/reference/http-api/story-routes.md"
expect_mutation_failure "shared contract duplicated into a child is rejected" "$mutation_root"

mutation_root="$(seed_mutation_repo missing-shared-link)"
sed -i.bak 's/(context-and-stories.md#shared-response-contract)/(context-and-stories.md)/' \
  "${mutation_root}/docs/public/reference/http-api/story-routes.md"
expect_mutation_failure "child missing the canonical shared-contract link is rejected" "$mutation_root"

printf 'context-stories-doc-split: all checks passed\n'
