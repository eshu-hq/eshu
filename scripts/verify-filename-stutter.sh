#!/usr/bin/env bash
# Filename-stutter gate (issue #6627) -- a file stem must not repeat its leaf
# directory name. `semantic/entity/entity_intents.go` repeats `entity`: the
# directory already says where the file lives, so the extra word is glued
# naming-rule debt that every later reader pays. The #6736 semantic nest
# shipped exactly this shape and needed a follow-up rename to fix it; this
# gate catches it at commit time instead of review time.
#
# The rule is deliberately exact-word, not stemming: `satisfied_by_intents.go`
# under `satisfaction/` does NOT flag (`satisfied` != `satisfaction`), and a
# package-root file named for its package (`catalog/catalog.go`) is idiomatic
# Go, not stutter, so stem == leaf is exempt. Only newly introduced names are
# checked -- Added and Renamed destinations -- so legacy family conventions
# (`correlation/rules/*_rules.go`, `parser/elixir/engine_elixir_*`,
# `cmd/*` binary prefixes) never block unrelated work. v1 covers Go files
# only; other suffixes exit 0 so the gate can widen later.
#
# Modes:
#   (default)      scan Added + Renamed destinations in the merge-base
#                  range plus staged files. Used by local promotion
#                  (registry local.command), where the changes are normally
#                  already committed and the index is clean, so --staged
#                  alone would false-green.
#   --staged       scan files staged in the index (Added + Renamed).
#                  Used by pre-commit only, the point where the name is
#                  introduced.
#   --files <f..>  scan explicit repo-relative paths. Used by tests.
#   --range <base> scan Added + Renamed destinations in <base>...HEAD.
#                  Used by CI so web edits and bot commits are covered too.
#
# Exit 0 when clean; 1 listing each offending path and why; 2 on usage or
# unresolvable-base errors (fail closed, never green on an unscanned tree).
set -euo pipefail

repo_root="${ESHU_STUTTER_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(cd "$(dirname "$0")/.." && pwd)"
fi
cd "$repo_root"

mode="default"
range_base=""
paths=()
case "${1:-}" in
  --staged) mode="staged" ;;
  --range)
    mode="range"
    range_base="${2:-}"
    if [ -z "$range_base" ]; then
      echo "usage: $(basename "$0") --range <base>" >&2
      exit 2
    fi
    ;;
  --files) mode="files"; shift; paths=("$@") ;;
  "") mode="default" ;;
  -*) echo "usage: $(basename "$0") [--staged | --files <f>... | --range <base>]" >&2; exit 2 ;;
  *) paths=("$@") ;;
esac

# The merge-base upstream for default mode. Overridable for tests; mirrors
# the factschema-diff precedent of diffing against the integration branch.
upstream="${ESHU_STUTTER_UPSTREAM:-origin/main}"

# Materialize the candidate list in the main shell before scanning. A
# failing producer (bogus base, disconnected history) must fail the gate,
# but an exit inside the process substitution feeding the scan loop would
# die in the subshell, hand the loop EOF, and report a clean tree.
candidates_file="$(mktemp)"
trap 'rm -f "$candidates_file"' EXIT
case "$mode" in
  staged)
    git diff --cached --name-status --diff-filter=AR -z >"$candidates_file"
    ;;
  range)
    if ! git rev-parse --verify --quiet "$range_base^{commit}" >/dev/null; then
      printf 'filename-stutter: cannot resolve range base %q\n' "$range_base" >&2
      exit 2
    fi
    if ! git diff --name-status "$range_base...HEAD" --diff-filter=AR -z >"$candidates_file"; then
      printf 'filename-stutter: cannot diff range %q...HEAD (no merge base?)\n' "$range_base" >&2
      exit 1
    fi
    ;;
  default)
    if ! merge_base="$(git merge-base HEAD "$upstream" 2>/dev/null)"; then
      printf 'filename-stutter: cannot resolve merge base with %q\n' "$upstream" >&2
      exit 2
    fi
    {
      git diff --name-status "$merge_base...HEAD" --diff-filter=AR -z
      git diff --cached --name-status --diff-filter=AR -z
    } >"$candidates_file"
    ;;
  files) printf '%s\0' "${paths[@]}" >"$candidates_file" ;;
esac

# Collect candidate repo-relative paths, one per line.
list_candidates() {
  cat "$candidates_file"
}

failures=0
while IFS= read -r -d '' status; do
  path="$status"
  case "$mode" in
    staged|range|default)
      # --name-status -z emits <status>\0<path>\0, or for renames
      # <status>\0<old>\0<new>\0: the destination (last field) is the
      # introduced name.
      if [[ "$status" =~ ^R ]]; then
        IFS= read -r -d '' _old
        IFS= read -r -d '' path
      else
        IFS= read -r -d '' path
      fi
      ;;
  esac
  case "$path" in
    *.go) ;;
    *) continue ;;
  esac
  base="$(basename "$path" .go)"
  base="${base%_test}"
  leaf="$(basename "$(dirname "$path")")"
  # Idiomatic package-root file: catalog/catalog.go is the package's own
  # front door, not stutter.
  if [ "$base" = "$leaf" ]; then
    continue
  fi
  # Namespace trios carry conventional names, never content words.
  case "$base" in
    doc) continue ;;
  esac
  stutters=0
  while IFS= read -r segment; do
    if [ "$segment" = "$leaf" ]; then
      stutters=1
      break
    fi
  done < <(printf '%s\n' "$base" | tr '_' '\n')
  if [ "$stutters" = "1" ]; then
    printf 'filename-stutter: %s repeats directory %s\n' "$path" "$leaf" >&2
    failures=$((failures + 1))
  fi
done < <(list_candidates)

if [ "$failures" != "0" ]; then
  printf 'filename-stutter: %d offending file(s) (naming rule: never repeat the directory name in the file name)\n' "$failures" >&2
  exit 1
fi
exit 0
