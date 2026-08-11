#!/usr/bin/env bash
# Verify the three-page context/story reference contract and its failure modes.
# This guards route ownership, legacy anchors, navigation, shared-contract
# placement, and the tighter per-page maintainability limit introduced by #5947.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
visible_scanner="${repo_root}/scripts/lib/context-stories-markdown-visible.awk"
tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fail() {
  printf 'context-stories-doc-split: %s\n' "$*" >&2
  return 1
}

[ -f "$visible_scanner" ] || {
  fail "missing scripts/lib/context-stories-markdown-visible.awk"
  exit 1
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

count_regex() {
  local pattern="$1"
  shift
  local output code
  set +e
  output="$(rg --no-filename --regexp "$pattern" "$@" 2>/dev/null)"
  code=$?
  set -e
  if [ "$code" -gt 1 ]; then
    fail "rg failed while matching: ${pattern}"
    return 1
  fi
  printf '%s\n' "$output" | awk 'NF { count++ } END { print count + 0 }'
}

require_regex_count() {
  local expected="$1"
  local pattern="$2"
  shift 2
  local actual
  actual="$(count_regex "$pattern" "$@")" || return 1
  [ "$actual" -eq "$expected" ] \
    || fail "expected ${expected} line(s) matching ${pattern}; found ${actual}"
}

escape_regex() {
  printf '%s\n' "$1" | sed 's/[][\\.^$*+?(){}|]/\\&/g'
}

scan_visible_markdown_regex() {
  local pattern="$1"
  local file="$2"
  local mode="$3"
  awk -v pattern="$pattern" -v mode="$mode" -f "$visible_scanner" "$file"
}

count_visible_heading_regex() {
  local pattern="$1"
  local file="$2"
  local output code
  set +e
  output="$(scan_visible_markdown_regex "$pattern" "$file" count)"
  code=$?
  set -e
  if [ "$code" -ne 0 ]; then
    fail "awk failed while matching visible heading: ${pattern}"
    return 1
  fi
  printf '%s\n' "$output"
}

require_visible_heading_count() {
  local expected="$1"
  local pattern="$2"
  local file="$3"
  local actual
  actual="$(count_visible_heading_regex "$pattern" "$file")" || return 1
  [ "$actual" -eq "$expected" ] \
    || fail "expected ${expected} visible heading(s) matching ${pattern}; found ${actual}"
}

count_visible_table_fixed() {
  local text="$1"
  local file="$2"
  local escaped pattern output code
  escaped="$(escape_regex "$text")"
  pattern="^ {0,3}[|].*${escaped}.*[|][ \t]*$"
  set +e
  output="$(scan_visible_markdown_regex "$pattern" "$file" count)"
  code=$?
  set -e
  if [ "$code" -ne 0 ]; then
    fail "visible Markdown scan failed for table row: ${text}"
    return 1
  fi
  printf '%s\n' "$output"
}

require_visible_table_count() {
  local expected="$1"
  local text="$2"
  local file="$3"
  local actual
  actual="$(count_visible_table_fixed "$text" "$file")" || return 1
  [ "$actual" -eq "$expected" ] \
    || fail "expected ${expected} visible table row(s) containing ${text}; found ${actual}"
}

heading_line() {
  local pattern="$1"
  local file="$2"
  local output code
  set +e
  output="$(scan_visible_markdown_regex "$pattern" "$file" line)"
  code=$?
  set -e
  if [ "$code" -ne 0 ]; then
    fail "missing visible heading: ${pattern}"
    return 1
  fi
  printf '%s\n' "$output"
}

verify_layout() {
  local root="$1"
  local docs_dir="${root}/docs/public/reference/http-api"
  local hub="${docs_dir}/context-and-stories.md"
  local stories="${docs_dir}/story-routes.md"
  local deployment="${docs_dir}/deployment-trace-and-influence.md"
  local mkdocs="${root}/docs/mkdocs.yml"
  local route_table="${root}/docs/public/reference/http-api.md"
  local sessions="${docs_dir}/dashboard-sessions.md"
  local ask="${docs_dir}/ask.md"
  local cloud="${docs_dir}/cloud-inventory.md"
  local file lines heading escaped_heading term route actual shared_line stories_line investigation_line

  for file in "$hub" "$stories" "$deployment" "$sessions" "$ask" "$cloud" "$mkdocs" "$route_table"; do
    [ -f "$file" ] || fail "missing ${file#"${root}/"}" || return 1
  done

  for file in "$hub" "$stories" "$deployment" "$sessions" "$ask" "$cloud" "$route_table"; do
    lines="$(wc -l <"$file" | tr -d ' ')"
    [ "$lines" -le 450 ] \
      || fail "${file#"${root}/"} has ${lines} lines; limit is 450" || return 1
  done

  # #5953: the http-api.md hub stays a route map. Each section it used to carry
  # inline lives on exactly one child page, reachable from the Route Families
  # table and the mkdocs nav.
  require_visible_heading_count 1 '^# Dashboard Browser Sessions$' "$sessions" || return 1
  require_visible_heading_count 1 '^# Ask Eshu' "$ask" || return 1
  require_visible_heading_count 1 '^# Cloud Inventory And Resource Paging$' "$cloud" || return 1

  # The moved headings must not linger in the hub as well as the child page.
  require_visible_heading_count 0 '^## Dashboard Browser Sessions$' "$route_table" || return 1
  require_visible_heading_count 0 '^## Ask Eshu' "$route_table" || return 1
  require_visible_heading_count 0 '^## Cloud Inventory Readback$' "$route_table" || return 1
  require_visible_heading_count 0 '^## Cloud Resource Graph Paging$' "$route_table" || return 1

  require_count 1 'Dashboard Browser Sessions: reference/http-api/dashboard-sessions.md' "$mkdocs" || return 1
  require_regex_count 1 '^        - Dashboard Browser Sessions: reference/http-api/dashboard-sessions\.md$' "$mkdocs" || return 1
  require_count 1 'Ask Eshu: reference/http-api/ask.md' "$mkdocs" || return 1
  require_regex_count 1 '^        - Ask Eshu: reference/http-api/ask\.md$' "$mkdocs" || return 1
  require_count 1 'Cloud Inventory And Resource Paging: reference/http-api/cloud-inventory.md' "$mkdocs" || return 1
  require_regex_count 1 '^        - Cloud Inventory And Resource Paging: reference/http-api/cloud-inventory\.md$' "$mkdocs" || return 1

  require_visible_table_count 1 '(http-api/dashboard-sessions.md)' "$route_table" || return 1
  require_visible_table_count 1 '(http-api/ask.md)' "$route_table" || return 1
  require_visible_table_count 1 '(http-api/cloud-inventory.md)' "$route_table" || return 1

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
    escaped_heading="$(escape_regex "$heading")"
    require_visible_heading_count 1 "^${escaped_heading}$" "$hub" || return 1
  done

  require_visible_heading_count 1 '^# HTTP Context And Story Routes$' "$hub" || return 1
  require_visible_heading_count 1 '^# HTTP Story Routes$' "$stories" || return 1
  require_visible_heading_count 1 '^# HTTP Deployment Trace And Influence Routes$' "$deployment" || return 1

  require_count 1 'Context And Stories: reference/http-api/context-and-stories.md' "$mkdocs" || return 1
  require_regex_count 1 '^        - Context And Stories: reference/http-api/context-and-stories\.md$' "$mkdocs" || return 1
  require_count 1 'Story Routes: reference/http-api/story-routes.md' "$mkdocs" || return 1
  require_regex_count 1 '^        - Story Routes: reference/http-api/story-routes\.md$' "$mkdocs" || return 1
  require_count 1 'Deployment Trace And Influence: reference/http-api/deployment-trace-and-influence.md' "$mkdocs" || return 1
  require_regex_count 1 '^        - Deployment Trace And Influence: reference/http-api/deployment-trace-and-influence\.md$' "$mkdocs" || return 1

  require_count 1 '(http-api/context-and-stories.md)' "$route_table" || return 1
  require_visible_table_count 1 '(http-api/context-and-stories.md)' "$route_table" || return 1
  require_count 1 '(http-api/story-routes.md)' "$route_table" || return 1
  require_visible_table_count 1 '(http-api/story-routes.md)' "$route_table" || return 1
  require_count 1 '(http-api/deployment-trace-and-influence.md)' "$route_table" || return 1
  require_visible_table_count 1 '(http-api/deployment-trace-and-influence.md)' "$route_table" || return 1

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
    require_visible_table_count 1 "$route" "$hub" || return 1
  done
  for route in "${story_routes[@]}"; do
    require_count 1 "$route" "$stories" || return 1
    require_count 1 "$route" "$hub" "$stories" "$deployment" || return 1
    require_visible_table_count 1 "$route" "$stories" || return 1
  done
  for route in "${deployment_routes[@]}"; do
    require_count 1 "$route" "$deployment" || return 1
    require_count 1 "$route" "$hub" "$stories" "$deployment" || return 1
    require_visible_table_count 1 "$route" "$deployment" || return 1
  done

  require_count 1 '## Shared Response Contract' "$hub" || return 1
  require_count 1 '## Shared Response Contract' "$hub" "$stories" "$deployment" || return 1
  require_count 1 '(context-and-stories.md#shared-response-contract)' "$stories" || return 1
  require_regex_count 1 '^[^<]*\[shared response contract\]\(context-and-stories\.md#shared-response-contract\)\.[[:space:]]*$' "$stories" || return 1
  require_count 1 '(context-and-stories.md#shared-response-contract)' "$deployment" || return 1
  require_regex_count 1 '^[^<]*\[shared response contract\]\(context-and-stories\.md#shared-response-contract\)\.[[:space:]]*$' "$deployment" || return 1

  shared_definition_headings=(
    '### Evidence boundaries'
    '### Hostname classification'
    '### Cross-repository truncation'
  )
  shared_line="$(heading_line '^## Shared Response Contract$' "$hub")" || return 1
  investigation_line="$(heading_line '^## Service Investigation$' "$hub")" || return 1
  if awk -v first="$shared_line" -v last="$investigation_line" \
    'NR > first && NR < last && /^## / { found = 1 } END { exit found ? 0 : 1 }' "$hub"; then
    fail 'Shared Response Contract must own its subsections through the next level-two heading'
    return 1
  fi
  for heading in "${shared_definition_headings[@]}"; do
    require_regex_count 1 "^${heading}$" "$hub" || return 1
    require_count 0 "$heading" "$stories" "$deployment" || return 1
    actual="$(heading_line "^${heading}$" "$hub")" || return 1
    [ "$actual" -gt "$shared_line" ] && [ "$actual" -lt "$investigation_line" ] \
      || fail "${heading} must be defined beneath Shared Response Contract" || return 1
  done

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
    actual="$(count_fixed "$term" "$hub")" || return 1
    [ "$actual" -gt 0 ] || fail "shared hub is missing ${term}" || return 1
  done

  stories_line="$(heading_line '^## Stories$' "$hub")" || return 1
  [ "$stories_line" -lt "$shared_line" ] \
    || fail 'Stories compatibility heading must precede Shared Response Contract' || return 1
  [ $((shared_line - stories_line)) -le 4 ] \
    || fail 'Stories compatibility section must remain a concise link stub' || return 1
  if awk -v first="$stories_line" -v last="$shared_line" \
    'NR > first && NR < last && /^### / { found = 1 } END { exit found ? 0 : 1 }' "$hub"; then
    fail 'Stories compatibility section must not own shared subsections'
    return 1
  fi
  require_regex_count 1 '^See \[story response details\]\(story-routes\.md#story-response-details\)\.$' "$hub" || return 1

  require_count 0 'configuration influence reports missing or inconsistent bound' "$hub" || return 1
  require_count 1 'configuration influence reports missing or inconsistent bound' "$deployment" || return 1

  require_visible_table_count 1 '(story-routes.md)' "$hub" || return 1
  require_visible_table_count 1 '(deployment-trace-and-influence.md)' "$hub" || return 1
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
  cp "${repo_root}/docs/public/reference/http-api/dashboard-sessions.md" \
    "${root}/docs/public/reference/http-api/"
  cp "${repo_root}/docs/public/reference/http-api/ask.md" \
    "${root}/docs/public/reference/http-api/"
  cp "${repo_root}/docs/public/reference/http-api/cloud-inventory.md" \
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

if [ "${1:-}" = "--verify-root" ]; then
  [ "$#" -eq 2 ] || fail 'usage: test-context-stories-doc-split.sh --verify-root <root>'
  verify_layout "$2"
  exit
fi

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
printf '%s\n' '### Evidence boundaries' \
  >>"${mutation_root}/docs/public/reference/http-api/story-routes.md"
expect_mutation_failure "shared contract duplicated into a child is rejected" "$mutation_root"

mutation_root="$(seed_mutation_repo missing-shared-link)"
sed -i.bak 's/(context-and-stories.md#shared-response-contract)/(context-and-stories.md)/' \
  "${mutation_root}/docs/public/reference/http-api/story-routes.md"
expect_mutation_failure "child missing the canonical shared-contract link is rejected" "$mutation_root"

mutation_root="$(seed_mutation_repo oversize-hub)"
awk 'BEGIN { for (i = 1; i <= 451; i++) print "extra line" }' \
  >>"${mutation_root}/docs/public/reference/http-api.md"
expect_mutation_failure "oversized http-api.md hub is rejected" "$mutation_root"

mutation_root="$(seed_mutation_repo hub-section-regrown)"
printf '%s\n' '## Cloud Inventory Readback' \
  >>"${mutation_root}/docs/public/reference/http-api.md"
expect_mutation_failure "section moved back into the hub is rejected" "$mutation_root"

mutation_root="$(seed_mutation_repo missing-ask-nav)"
sed -i.bak '/Ask Eshu: reference\/http-api\/ask.md/d' \
  "${mutation_root}/docs/mkdocs.yml"
expect_mutation_failure "missing Ask Eshu navigation entry is rejected" "$mutation_root"

mutation_root="$(seed_mutation_repo changed-title)"
sed -i.bak 's/^# HTTP Story Routes$/# HTTP Story Route Reference/' \
  "${mutation_root}/docs/public/reference/http-api/story-routes.md"
expect_mutation_failure "changed child title is rejected" "$mutation_root"

mutation_root="$(seed_mutation_repo hidden-title)"
sed -i.bak 's/^# HTTP Story Routes$/<!-- # HTTP Story Routes -->/' \
  "${mutation_root}/docs/public/reference/http-api/story-routes.md"
expect_mutation_failure "comment-hidden child title is rejected" "$mutation_root"

mutation_root="$(seed_mutation_repo commented-nav)"
sed -i.bak 's/^        - Story Routes:/        # - Story Routes:/' \
  "${mutation_root}/docs/mkdocs.yml"
expect_mutation_failure "commented navigation entry is rejected" "$mutation_root"

printf 'context-stories-doc-split: all checks passed\n'
