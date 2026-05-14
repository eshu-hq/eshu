#!/usr/bin/env bash
set -euo pipefail

repo_root="${ESHU_PACKAGE_DOCS_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
    || (cd "$(dirname "$0")/.." && pwd))"
fi

base="${ESHU_PACKAGE_DOCS_BASE:-}"
if [ -z "$base" ] && [ -n "${GITHUB_BASE_REF:-}" ]; then
  git -C "$repo_root" fetch --no-tags --depth=1 origin "$GITHUB_BASE_REF" >/dev/null 2>&1 || true
  if git -C "$repo_root" rev-parse --verify "origin/$GITHUB_BASE_REF" >/dev/null 2>&1; then
    base="origin/$GITHUB_BASE_REF"
  fi
fi
if [ -z "$base" ]; then
  if git -C "$repo_root" rev-parse --verify HEAD~1 >/dev/null 2>&1; then
    base="HEAD~1"
  else
    printf 'verify-package-docs: no base commit available, skipping\n'
    exit 0
  fi
fi

changed_files=()
if git -C "$repo_root" diff --name-only "$base"...HEAD >/tmp/eshu-package-doc-files 2>/dev/null; then
  :
else
  git -C "$repo_root" diff --name-only "$base" HEAD >/tmp/eshu-package-doc-files
fi
while IFS= read -r file; do
  [ -n "$file" ] && changed_files+=("$file")
done </tmp/eshu-package-doc-files
rm -f /tmp/eshu-package-doc-files

is_package_source() {
  local path="$1"
  case "$path" in
    *.go) ;;
    *) return 1 ;;
  esac
  case "$path" in
    *_test.go|*_bench_test.go|*/testdata/*|*/vendor/*|*/doc.go) return 1 ;;
  esac
  case "$path" in
    go/internal/*|go/cmd/*) return 0 ;;
    *) return 1 ;;
  esac
}

declare -A package_dirs=()
for file in "${changed_files[@]}"; do
  is_package_source "$file" || continue
  dir="${file%/*}"
  package_dirs["$dir"]=1
done

if [ "${#package_dirs[@]}" -eq 0 ]; then
  printf 'verify-package-docs: no changed Go package source files\n'
  exit 0
fi

missing=0
for dir in "${!package_dirs[@]}"; do
  for required in doc.go README.md AGENTS.md; do
    if [ ! -f "$repo_root/$dir/$required" ]; then
      printf 'verify-package-docs: %s is missing %s\n' "$dir" "$required" >&2
      missing=1
    fi
  done
done

if [ "$missing" -ne 0 ]; then
  {
    printf '\nEvery changed Go package under go/internal or go/cmd must carry:\n'
    printf '  - doc.go for go doc users\n'
    printf '  - README.md for human architecture and operations context\n'
    printf '  - AGENTS.md for package-local AI editing rules\n'
    printf '\nThis keeps new collectors and runtime packages from bypassing the concrete\n'
    printf 'package guidance that reviewers and AI agents rely on.\n'
  } >&2
  exit 1
fi

printf 'verify-package-docs: changed Go package docs present\n'
