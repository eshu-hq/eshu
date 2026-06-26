#!/usr/bin/env bash
set -euo pipefail

repo_root="${ESHU_PACKAGE_DOCS_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  # Derive the repo root from the script's own location, NOT
  # `git rev-parse --show-toplevel`. Git hooks (pre-commit/pre-push) export
  # GIT_DIR, and with GIT_DIR set `git -C scripts rev-parse --show-toplevel`
  # returns the -C directory (<repo>/scripts) instead of the repo root, so the
  # `$repo_root/<path>` checks fail. The script always lives at <repo>/scripts/,
  # so dirname/.. is the repo root and is both worktree- and hook-safe.
  repo_root="$(cd "$(dirname "$0")/.." && pwd)"
fi

base="${ESHU_PACKAGE_DOCS_BASE:-}"
if [ -z "$base" ] && [ -n "${GITHUB_BASE_REF:-}" ]; then
  git -C "$repo_root" fetch --no-tags --depth=1 origin "$GITHUB_BASE_REF" >/dev/null 2>&1 || true
  if git -C "$repo_root" rev-parse --verify "origin/$GITHUB_BASE_REF" >/dev/null 2>&1; then
    base="origin/$GITHUB_BASE_REF"
  fi
fi
if [ -z "$base" ]; then
  # Local (non-CI) runs: diff against the branch's divergence point from
  # origin/main, not HEAD~1. On a branch based on a squash-merge commit, HEAD~1
  # is the pre-merge commit, so HEAD~1...HEAD diffs the MERGE's files (packages
  # the developer never touched) and false-positives — which pushes developers to
  # `commit --no-verify` and thereby skips the other gates (hot-path evidence,
  # telemetry) too. The merge-base with origin/main yields only the branch's own
  # changes. CI keeps using GITHUB_BASE_REF above.
  #
  # Use the origin/main ref the clone already has rather than fetching: this runs
  # at the pre-commit stage on every go/ commit, and a network round-trip (or a
  # slow-network hang) per commit is not worth it. A slightly stale base only
  # widens the changed-file set conservatively.
  if git -C "$repo_root" rev-parse --verify origin/main >/dev/null 2>&1; then
    base="$(git -C "$repo_root" merge-base origin/main HEAD 2>/dev/null || echo origin/main)"
  elif git -C "$repo_root" rev-parse --verify HEAD~1 >/dev/null 2>&1; then
    base="HEAD~1"
  else
    printf 'verify-package-docs: no base commit available, skipping\n'
    exit 0
  fi
fi

changed_files=()
if git -C "$repo_root" diff --name-status "$base"...HEAD >/tmp/eshu-package-doc-files 2>/dev/null; then
  :
else
  git -C "$repo_root" diff --name-status "$base" HEAD >/tmp/eshu-package-doc-files
fi
while IFS=$'\t' read -r status first second; do
  case "$status" in
    D*) continue ;;
    R*|C*) file="$second" ;;
    *) file="$first" ;;
  esac
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

package_dirs=()
package_dir_keys=" "
if [ "${#changed_files[@]}" -ne 0 ]; then
  for file in "${changed_files[@]}"; do
    is_package_source "$file" || continue
    dir="${file%/*}"
    case "$package_dir_keys" in
      *" ${dir} "*) ;;
      *)
        package_dirs+=("$dir")
        package_dir_keys="${package_dir_keys}${dir} "
        ;;
    esac
  done
fi

if [ "${#package_dirs[@]}" -eq 0 ]; then
  printf 'verify-package-docs: no changed Go package source files\n'
  exit 0
fi

missing=0
for dir in "${package_dirs[@]}"; do
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
    printf '  - AGENTS.md for scoped agent instructions\n'
    printf '\nThis keeps new collectors and runtime packages from bypassing the concrete\n'
    printf 'package guidance that reviewers and AI agents rely on.\n'
  } >&2
  exit 1
fi

printf 'verify-package-docs: changed Go package docs present\n'
