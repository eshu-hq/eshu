#!/usr/bin/env bash
# Exercise Markdown visibility parsing and its production route/heading owners.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/test-context-stories-doc-split.sh"
visible_scanner="${repo_root}/scripts/lib/context-stories-markdown-visible.awk"
bash_bin="$BASH"
tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fail() {
  printf 'context-stories-markdown-visible: %s\n' "$*" >&2
  return 1
}

for file in "$verifier" "$visible_scanner"; do
  [ -f "$file" ] || fail "missing ${file#"${repo_root}/"}"
done

scan_count() {
  local pattern="$1"
  local file="$2"
  local output code
  set +e
  output="$(awk -v pattern="$pattern" -v mode=count -f "$visible_scanner" "$file")"
  code=$?
  set -e
  [ "$code" -eq 0 ] || fail "visible Markdown scan failed: ${pattern}" || return 1
  printf '%s\n' "$output"
}

require_scan_count() {
  local expected="$1"
  local pattern="$2"
  local file="$3"
  local label="$4"
  local actual
  actual="$(scan_count "$pattern" "$file")" || return 1
  [ "$actual" -eq "$expected" ] || fail "$label; expected ${expected}, found ${actual}"
}

seed_mutation_repo() {
  local name="$1"
  local root="${tmp_root}/${name}"
  mkdir -p "${root}/docs/public/reference/http-api"
  cp "${repo_root}/docs/public/reference/http-api/context-and-stories.md" \
    "${repo_root}/docs/public/reference/http-api/story-routes.md" \
    "${repo_root}/docs/public/reference/http-api/deployment-trace-and-influence.md" \
    "${root}/docs/public/reference/http-api/"
  cp "${repo_root}/docs/public/reference/http-api.md" "${root}/docs/public/reference/"
  cp "${repo_root}/docs/mkdocs.yml" "${root}/docs/"
  printf '%s\n' "$root"
}

hide_line_in_long_fence() {
  local marker="$1"
  local target="$2"
  local file="$3"
  local output="${file}.nested-fence.tmp"
  awk -v marker="$marker" -v target="$target" '
    BEGIN { outer = marker marker marker marker; inner = marker marker marker }
    $0 == target { print outer; print inner; print; print inner; print outer; next }
    { print }
  ' "$file" >"$output"
  mv "$output" "$file"
}

hide_line_in_multiline_comment() {
  local target="$1"
  local file="$2"
  local output="${file}.multiline-comment.tmp"
  awk -v target="$target" '
    $0 == target { print "<!--"; print; print "-->"; next }
    { print }
  ' "$file" >"$output"
  mv "$output" "$file"
}

hide_line_after_chained_comment() {
  local target="$1"
  local file="$2"
  local output="${file}.chained-comment.tmp"
  awk -v target="$target" '
    $0 == target { print "<!-- closed --> <!--"; print; print "-->"; next }
    { print }
  ' "$file" >"$output"
  mv "$output" "$file"
}

expect_mutation_failure() {
  local label="$1"
  local root="$2"
  if "$bash_bin" "$verifier" --verify-root "$root" >/dev/null 2>&1; then
    fail "mutation did not fail: ${label}"
    exit 1
  fi
  printf 'ok - %s\n' "$label"
}

scanner_fixture="${tmp_root}/visible-scanner-transitions.md"
printf '%s\n' \
  'visible <!-- one -->prefix<!-- two --> suffix' \
  '<!-- open' \
  '-->close suffix' \
  '~~~~' \
  '<!-- unmatched opener inside fence' \
  '~~~~' \
  'visible after fence' >"$scanner_fixture"
require_scan_count 1 '^visible.*prefix.* suffix$' "$scanner_fixture" \
  'visible scanner lost text around multiple closed comments'
require_scan_count 1 '^.*close suffix$' "$scanner_fixture" \
  'visible scanner lost text after a multiline comment close'
require_scan_count 1 '^visible after fence$' "$scanner_fixture" \
  'comment tokens inside a fence changed scanner state'

"$bash_bin" "$verifier" --verify-root "$repo_root"
printf 'ok - real three-page visibility contract\n'

mutation_root="$(seed_mutation_repo hidden-legacy-heading)"
sed -i.bak 's/^## Incident Context$/<!-- ## Incident Context -->/' \
  "${mutation_root}/docs/public/reference/http-api/context-and-stories.md"
expect_mutation_failure "comment-hidden legacy heading is rejected" "$mutation_root"

mutation_root="$(seed_mutation_repo long-backtick-fence)"
hide_line_in_long_fence '`' '## Incident Context' \
  "${mutation_root}/docs/public/reference/http-api/context-and-stories.md"
expect_mutation_failure "heading hidden by a four-backtick fence is rejected" "$mutation_root"

mutation_root="$(seed_mutation_repo long-tilde-fence)"
hide_line_in_long_fence '~' '## Incident Context' \
  "${mutation_root}/docs/public/reference/http-api/context-and-stories.md"
expect_mutation_failure "heading hidden by a four-tilde fence is rejected" "$mutation_root"

mutation_root="$(seed_mutation_repo commented-route-row)"
sed -i.bak '/^| Repository story |/s/^/<!-- /; /^<!-- | Repository story |/s/$/ -->/' \
  "${mutation_root}/docs/public/reference/http-api/story-routes.md"
expect_mutation_failure "comment-hidden route row is rejected" "$mutation_root"

owned_route_row="| Repository story | \`GET /api/v0/repositories/{repo_id}/story\` |"

mutation_root="$(seed_mutation_repo multiline-commented-route-row)"
hide_line_in_multiline_comment "$owned_route_row" \
  "${mutation_root}/docs/public/reference/http-api/story-routes.md"
expect_mutation_failure "route row hidden by a multiline comment is rejected" "$mutation_root"

mutation_root="$(seed_mutation_repo chained-commented-route-row)"
hide_line_after_chained_comment "$owned_route_row" \
  "${mutation_root}/docs/public/reference/http-api/story-routes.md"
expect_mutation_failure "route row hidden after chained comments is rejected" "$mutation_root"

mutation_root="$(seed_mutation_repo comment-close-prefixed-route-row)"
story_file="${mutation_root}/docs/public/reference/http-api/story-routes.md"
awk -v target="$owned_route_row" '
  $0 == target { print "<!--"; print "-->" $0; next }
  { print }
' "$story_file" >"${story_file}.tmp"
mv "${story_file}.tmp" "$story_file"
expect_mutation_failure "route row prefixed by a comment close is rejected" "$mutation_root"

mutation_root="$(seed_mutation_repo fenced-backtick-route-row)"
hide_line_in_long_fence '`' "$owned_route_row" \
  "${mutation_root}/docs/public/reference/http-api/story-routes.md"
expect_mutation_failure "route row hidden by a four-backtick fence is rejected" "$mutation_root"

mutation_root="$(seed_mutation_repo fenced-tilde-route-row)"
hide_line_in_long_fence '~' "$owned_route_row" \
  "${mutation_root}/docs/public/reference/http-api/story-routes.md"
expect_mutation_failure "route row hidden by a four-tilde fence is rejected" "$mutation_root"

printf 'context-stories-markdown-visible: all checks passed\n'
