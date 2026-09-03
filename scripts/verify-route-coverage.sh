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

# A moved-away or renamed surface must fail loudly, not vanish from route
# coverage checking (#6055): both scan paths below (the git-diff path and the
# no-base-ref fallback) end in an empty file list — indistinguishable from a
# genuinely quiet PR — when either directory no longer exists at all.
for required_dir in "$query_dir" "$api_dir"; do
  if [ ! -d "$required_dir" ]; then
    printf 'verify-route-coverage: %s does not exist; route coverage cannot be checked\n' "$required_dir" >&2
    exit 1
  fi
done

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

# The filter is AMR, not AM: a handler that MOVES is reported by git as R, and
# an AM filter drops it, so relocating a route-registering file made this gate
# print "0 routes checked" and exit 0 on exactly the change the header above
# says must fail loudly (#6060). R lists the destination path under
# --name-only, which is the file to scan. Note that rename classification is
# pathspec-dependent: with a pathspec covering only the destination directory
# git cannot pair the source and reports A, so a narrow-pathspec spot check
# will wrongly suggest AM is sufficient. The pathspecs here span both.
#
# Both branches below exclude testdata/. The git-diff branch is the one CI
# actually takes, so an exclusion applied only to the fallback would leave the
# real path unguarded while a fallback-driven test still passed (#6055 review
# finding). git diff has always listed nested paths, so a non-test .go fixture
# under testdata/ would be scanned as a route and reported UNCOVERED — loud
# rather than false-green, but wrong either way.
get_changed_files() {
  if [ -n "$base" ] && git -C "$repo_root" rev-parse --verify "$base" >/dev/null 2>&1; then
    (git -C "$repo_root" diff --name-only --diff-filter=AMR -z "$base" HEAD -- \
       "$query_dir" "$api_dir" 2>/dev/null
     git -C "$repo_root" diff --name-only --diff-filter=AMR -z HEAD -- \
       "$query_dir" "$api_dir" 2>/dev/null
     git -C "$repo_root" diff --name-only --diff-filter=AMR -z --cached -- \
       "$query_dir" "$api_dir" 2>/dev/null) \
    | tr '\0' '\n' | sort -u | grep -v '_test\.go$' | grep -v '/testdata/' | grep '\.go$' | \
    while IFS= read -r f; do [ -n "$f" ] && echo "${repo_root}/${f}"; done
  else
    # Recursive (not -maxdepth 1, #6055): a handler that moved into a
    # subdirectory of query_dir/api_dir must still be found when no base ref
    # is available and every route is being checked.
    # !testdata/** for the same reason parseReducerDir and globFilesRecursive
    # exclude it in this PR: filepath/`find -maxdepth 1` never crossed into a
    # subdirectory, so making this recursive newly exposes fixture handlers that
    # must not be treated as real routes.
    rg --files --glob '*.go' --glob '!*_test.go' --glob '!**/testdata/**' \
      "$query_dir" "$api_dir" 2>/dev/null
  fi
}

while IFS= read -r gofile; do
  [ -z "$gofile" ] && continue
  file_rel="${gofile#$repo_root/}"
  file_stem="$(basename "$gofile" .go)"
  # handler_dir scopes the test-existence search to the handler's own
  # directory tree (not the whole of query_dir/api_dir, #6055 review finding):
  # searching the full trees let a test in an unrelated SIBLING package
  # satisfy coverage for a handler it cannot possibly exercise, as long as the
  # test function's name happened to fuzzily match the derived search word
  # (e.g. a coincidental TestRepoNew in query/b covering an untested handler
  # in query/a). The lookup itself is depth-limited to handler_dir; the
  # reasoning for that is at the rg call below, next to the flag.
  handler_dir="$(dirname "$gofile")"
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
      # Case-insensitive: pascal_case() title-cases each snake_case segment
      # (e.g. "saml_handler" -> "SamlHandler"), but idiomatic Go test names
      # preserve initialisms as written in the source identifier (e.g.
      # "SAMLHandler", matching the "SAML" acronym in the handler struct
      # name). An exact-case search would false-positive as "uncovered" on
      # any acronym-bearing handler/route even when a matching test exists.
      # EXACTLY the handler's own directory, not a recursive walk below it.
      # A Go package is one directory, so a test that can actually exercise
      # this handler is in the same directory -- either the same package or
      # its external <pkg>_test. Recursing admitted a DIFFERENT package: a
      # handler at query/repo.go was satisfied by query/child/x_test.go
      # containing a fuzzily matching TestRepoNew (#6055 review finding).
      # The "handler and test moved together" case this originally recursed
      # for is already covered, because when both move, handler_dir IS the
      # new directory.
      if rg -qi "func Test\w*${word}\w*\(" \
           --glob '*_test.go' --glob '!**/testdata/**' --max-depth 1 \
           "$handler_dir" 2>/dev/null; then
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
