#!/usr/bin/env bash
#
# verify-route-coverage.sh — fail if a NEW HTTP route registered via HandleFunc
# in go/internal/query/ or go/cmd/api/ lacks handler test coverage.
#
# In CI, compares against the PR base ref (GITHUB_BASE_REF). Locally, compares
# against origin/main. Routes that pre-date the base ref are not flagged.
#
# A route is "covered" when at least one Test* function in a *_test.go file
# under the same directories references the handler method name or the handler
# file's canonical CamelCase identifier.
#
# Exit 0 when every new HandleFunc has a matching test; non-zero with a gap
# report.
set -euo pipefail

repo_root="${ESHU_ROUTE_COVERAGE_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(cd "$(dirname "$0")/.." && pwd)"
fi

query_dir="${repo_root}/go/internal/query"
api_dir="${repo_root}/go/cmd/api"

base="${ESHU_ROUTE_COVERAGE_BASE:-}"
if [ -z "$base" ] && [ -n "${GITHUB_BASE_REF:-}" ]; then
  git -C "$repo_root" fetch --no-tags --depth=1 origin "$GITHUB_BASE_REF" >/dev/null 2>&1 || true
  if git -C "$repo_root" rev-parse --verify "origin/$GITHUB_BASE_REF" >/dev/null 2>&1; then
    base="origin/$GITHUB_BASE_REF"
  fi
fi
if [ -z "$base" ]; then
  if git -C "$repo_root" rev-parse --verify origin/main >/dev/null 2>&1; then
    base="$(git -C "$repo_root" merge-base origin/main HEAD 2>/dev/null || echo origin/main)"
  elif git -C "$repo_root" rev-parse --verify HEAD~1 >/dev/null 2>&1; then
    base="HEAD~1"
  else
    printf 'verify-route-coverage: no base ref available, checking all routes\n'
    base=""
  fi
fi

failures=0
total=0

pascal_case() {
  awk '{
    result = ""
    split($0, chars, "")
    cap = 1
    for (i = 1; i <= length($0); i++) {
      c = chars[i]
      if (c == "-" || c == "_") { cap = 1; continue }
      if (cap) { result = result toupper(c); cap = 0 }
      else { result = result c }
    }
    print result
  }' <<< "$1"
}

# test_search_words returns space-separated search terms derived from the
# handler method name and the file that contains it.
test_search_words() {
  local method="$1" file_stem="$2"

  local pascal
  pascal="$(pascal_case "$method")"
  printf '%s' "$pascal"

  local stripped="$pascal"
  for prefix in Handle Get Post Put Delete List; do
    if [[ "$stripped" == "${prefix}"* ]]; then
      stripped="${stripped#$prefix}"
      break
    fi
  done
  if [ "$stripped" != "$pascal" ]; then
    printf ' %s' "$stripped"
  fi

  # For very short/common method names (e.g. "list", "detail", "getFamily"),
  # add a concatenated file-stem+method search term. This avoids false matches
  # from an unrelated sibling test in the same file (e.g. a bare "Repository"
  # word matching TestRepositoryListCatalog when a new short route has no test).
  local short_threshold=7
  if [ ${#pascal} -lt "$short_threshold" ] || [ ${#stripped} -lt "$short_threshold" ]; then
    local file_pascal
    file_pascal="$(pascal_case "$file_stem")"
    if [ -n "$file_pascal" ] && [ "$file_pascal" != "$pascal" ] && [ "$file_pascal" != "$stripped" ]; then
      printf ' %s%s' "$file_pascal" "$stripped"
    fi
  fi
}

print_failure() {
  local file="$1" method="$2" route="$3"
  printf 'UNCOVERED: %s:%s (route %s) has no matching test function\n' "$file" "$method" "$route"
  failures=$((failures + 1))
}

get_changed_files() {
  if [ -n "$base" ] && git -C "$repo_root" rev-parse --verify "$base" >/dev/null 2>&1; then
    (git -C "$repo_root" diff --name-only --diff-filter=AM -z "$base" HEAD -- \
       "$query_dir" "$api_dir" 2>/dev/null
     git -C "$repo_root" diff --name-only --diff-filter=AM -z HEAD -- \
       "$query_dir" "$api_dir" 2>/dev/null
     git -C "$repo_root" diff --name-only --diff-filter=AM -z --cached -- \
       "$query_dir" "$api_dir" 2>/dev/null) \
    | tr '\0' '\n' | sort -u | grep -v '_test\.go$' | grep '\.go$' | \
    while IFS= read -r f; do [ -n "$f" ] && echo "${repo_root}/${f}"; done
  else
    find "$query_dir" "$api_dir" -maxdepth 1 -name '*.go' ! -name '*_test.go' 2>/dev/null
  fi
}

while IFS= read -r gofile; do
  [ -z "$gofile" ] && continue
  file_rel="${gofile#$repo_root/}"
  file_stem="$(basename "$gofile" .go)"
  while IFS= read -r line; do
    handle=$(echo "$line" | sed -n 's/.*HandleFunc("\([^"]*\)".*[. ]\([a-zA-Z][a-zA-Z0-9]*\)).*/\1|\2/p')
    if [ -z "$handle" ]; then
      continue
    fi
    route="${handle%%|*}"
    method="${handle##*|}"
    total=$((total + 1))

    search_words="$(test_search_words "$method" "$file_stem")"
    found=0
    for word in $search_words; do
      if [ ${#word} -lt 4 ]; then
        continue
      fi
      if rg -q "func Test\w*${word}\w*\(" \
           --glob '*_test.go' \
           --max-depth 1 \
           "$query_dir" "$api_dir" 2>/dev/null; then
        found=1
        break
      fi
    done

    if [ "$found" -eq 0 ]; then
      print_failure "$file_rel" "$method" "$route"
    fi
  done < <(rg --no-filename -n 'HandleFunc\(' "$gofile")
done < <(get_changed_files)

printf '%d routes checked, %d uncovered\n' "$total" "$failures"

if [ "$failures" -gt 0 ]; then
  exit 1
fi
exit 0
