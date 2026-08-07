#!/usr/bin/env bash
#
# Reject explicit abbreviated Git commit citations in query package Markdown. Eshu
# squash-merges feature branches, so those branch revisions do not become
# stable references on main. Other short hexadecimal evidence values remain
# valid unless the surrounding prose explicitly calls them a commit.
set -euo pipefail

repo_root="${ESHU_QUERY_DOC_COMMIT_REFS_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fi

command -v rg >/dev/null 2>&1 || {
  printf '%s\n' 'QUERY DOC COMMIT REF SCAN FAILED: rg is required' >&2
  exit 1
}

query_dir="${repo_root}/go/internal/query"
queryplan_dir="${repo_root}/go/internal/queryplan"
scan_dirs=("$query_dir" "$queryplan_dir")
for dir in "${scan_dirs[@]}"; do
  if [ ! -d "$dir" ]; then
    printf 'QUERY DOC COMMIT REF SCAN FAILED: directory missing: %s\n' "$dir" >&2
    exit 1
  fi
done

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
markdown_files="${tmp_dir}/markdown-files.txt"
findings="${tmp_dir}/findings.txt"

set +e
rg --files --max-depth 1 -g '*.md' "${scan_dirs[@]}" >"$markdown_files"
list_rc=$?
set -e
if [ "$list_rc" -gt 1 ]; then
  printf 'QUERY DOC COMMIT REF SCAN FAILED: rg could not list Markdown files (exit %s)\n' \
    "$list_rc" >&2
  exit 1
fi
if [ ! -s "$markdown_files" ]; then
  printf '%s\n' 'QUERY DOC COMMIT REF SCAN FAILED: no Markdown files found' >&2
  exit 1
fi

markdown_args=()
while IFS= read -r file; do
  [ -n "$file" ] && markdown_args+=("$file")
done <"$markdown_files"

# A citation must explicitly say "commit" or "commit SHA" before a 7-39 digit
# hexadecimal token. A colon may separate the label from the value. The word
# boundary prevents a prefix match on a full source revision. Digests, external
# tags, raw evidence tokens, and Git command arguments have no such citation
# phrase and remain outside this gate.
# shellcheck disable=SC2016 # Literal PCRE; backslashes and backticks must not expand.
commit_citation_pattern='(?i:\bcommit(?:[[:space:]]+SHA)?(?:[[:space:]]+|[[:space:]]*:[[:space:]]*)`?[0-9a-f]{7,39}`?\b)'
set +e
rg -n --no-heading --pcre2 "$commit_citation_pattern" \
  "${markdown_args[@]}" >"$findings"
scan_rc=$?
set -e
if [ "$scan_rc" -gt 1 ]; then
  printf 'QUERY DOC COMMIT REF SCAN FAILED: rg could not scan Markdown files (exit %s)\n' \
    "$scan_rc" >&2
  exit 1
fi
if [ "$scan_rc" -eq 0 ]; then
  printf '%s\n' 'QUERY DOC COMMIT REF VIOLATION: explicit short commit citation found' >&2
  cat "$findings" >&2
  printf '%s\n' \
    'Use the merged PR plus a landed file, symbol, or test. Keep pre-squash measurement provenance explicit.' >&2
  exit 1
fi

printf 'QUERY DOC COMMIT REFS OK: scanned %s Markdown files\n' "${#markdown_args[@]}"
