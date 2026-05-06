#!/usr/bin/env bash
# scripts/check-docs-stale.sh
#
# Detect drift between Go source files under go/ and their accompanying
# README.md and doc.go. Tool-neutral: invoked by the Claude Code PostToolUse
# hook, the AGENTS.md "Doc-keeper workflow" path that Codex follows, and any
# git pre-commit hook that wants the same check.
#
# Usage:
#   scripts/check-docs-stale.sh [--changed FILE...] [--all]
#
#   --changed FILE...   Check only the named .go files (used by hooks).
#   --all               Walk every directory under go/ and report missing or
#                       stale README.md / doc.go pairs.
#   (no arg)            Equivalent to --all.
#
# Output: appends one JSON line per stale or missing file to
# .eshu-doc-state/stale.jsonl and prints a short reminder to stderr. Exit code
# is 0 (the script never blocks edits or commits — it only signals).

set -u
set -o pipefail

repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
  || (cd "$(dirname "$0")/.." && pwd))"
state_dir="$repo_root/.eshu-doc-state"
state_file="$state_dir/stale.jsonl"
mkdir -p "$state_dir"

tool="${ESHU_DOC_KEEPER_TOOL:-cli}"

now() { date -u +"%Y-%m-%dT%H:%M:%SZ"; }

mtime() {
  # GNU stat differs from BSD stat. Try BSD first (macOS default), then GNU.
  stat -f %m "$1" 2>/dev/null || stat -c %Y "$1" 2>/dev/null || echo 0
}

emit() {
  # emit DIR REASON CHANGED_FILE
  local dir="$1" reason="$2" changed="$3"
  local rel_dir="${dir#$repo_root/}"
  local rel_changed="${changed#$repo_root/}"
  printf '{"dir":"%s","reason":"%s","changed":"%s","tool":"%s","ts":"%s"}\n' \
    "$rel_dir" "$reason" "$rel_changed" "$tool" "$(now)" >>"$state_file"
  printf 'doc-keeper: %s — %s (changed: %s)\n' \
    "$rel_dir" "$reason" "$rel_changed" >&2
}

check_dir() {
  local dir="$1" changed="${2:-$1}"
  local readme="$dir/README.md"
  local docgo="$dir/doc.go"

  # If the changed source file no longer exists (deleted or renamed away in
  # the same edit), treat that as drift directly: a removed export is exactly
  # the kind of change docs need to reflect. We cannot mtime-compare against
  # a missing file, so flag the directory unconditionally.
  if [ ! -e "$changed" ]; then
    [ -f "$readme" ] && emit "$dir" "stale-readme-source-missing" "$changed" \
      || emit "$dir" "missing-readme" "$changed"
    [ -f "$docgo" ]  && emit "$dir" "stale-docgo-source-missing"  "$changed" \
      || emit "$dir" "missing-docgo" "$changed"
    return
  fi

  if [ ! -f "$readme" ]; then
    emit "$dir" "missing-readme" "$changed"
  elif [ "$(mtime "$readme")" -lt "$(mtime "$changed")" ]; then
    emit "$dir" "stale-readme" "$changed"
  fi

  if [ ! -f "$docgo" ]; then
    emit "$dir" "missing-docgo" "$changed"
  elif [ "$(mtime "$docgo")" -lt "$(mtime "$changed")" ]; then
    emit "$dir" "stale-docgo" "$changed"
  fi
}

is_relevant_go_file() {
  local f="$1"
  case "$f" in
    *.go) ;;
    *) return 1 ;;
  esac
  case "$f" in
    *_test.go) return 1 ;;
    */vendor/*) return 1 ;;
    */testdata/*) return 1 ;;
    */doc.go) return 1 ;;
  esac
  case "$f" in
    "$repo_root"/go/*) return 0 ;;
    *) return 1 ;;
  esac
}

mode="all"
changed_files=()
while [ $# -gt 0 ]; do
  case "$1" in
    --changed)
      mode="changed"
      shift
      while [ $# -gt 0 ] && [ "${1:0:2}" != "--" ]; do
        changed_files+=("$1")
        shift
      done
      ;;
    --all) mode="all"; shift ;;
    *) shift ;;
  esac
done

if [ "$mode" = "changed" ]; then
  for f in "${changed_files[@]}"; do
    # Resolve to absolute path so the prefix match works.
    case "$f" in
      /*) abs="$f" ;;
      *) abs="$repo_root/$f" ;;
    esac
    is_relevant_go_file "$abs" || continue
    check_dir "$(dirname "$abs")" "$abs"
  done
  exit 0
fi

# --all is a full snapshot of current drift. Truncate first so consumers see
# only the latest state instead of an ever-growing append log.
: > "$state_file"

if [ ! -d "$repo_root/go" ]; then
  exit 0
fi

# Per repo policy (AGENTS.md / CLAUDE.md), use rg, not find. Enumerate every
# non-test, non-doc.go Go source file under go/ and group by directory. rg
# already respects .gitignore and .ignore, which keeps vendor/testdata
# implicitly excluded if they are listed there; we add explicit globs as
# belt-and-suspenders so the behavior does not depend on ignore-file state.
declare -A dir_newest=()
declare -A dir_newest_path=()

while IFS= read -r gf; do
  d=$(dirname "$gf")
  m=$(mtime "$gf")
  prev="${dir_newest[$d]:-0}"
  if [ "$m" -gt "$prev" ]; then
    dir_newest[$d]=$m
    dir_newest_path[$d]=$gf
  fi
done < <(rg --files \
  -g '*.go' \
  -g '!*_test.go' \
  -g '!**/doc.go' \
  -g '!**/vendor/**' \
  -g '!**/testdata/**' \
  "$repo_root/go" 2>/dev/null)

for d in "${!dir_newest_path[@]}"; do
  check_dir "$d" "${dir_newest_path[$d]}"
done

exit 0
