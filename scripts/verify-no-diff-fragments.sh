#!/usr/bin/env bash
#
# verify-no-diff-fragments.sh — fail if a source file contains a raw unified-diff
# fragment or an unresolved merge-conflict marker.
#
# Why this exists: commit 09e87532e (PR #5598) landed a literal diff blob inside
# a Go test file's godoc. The module then failed to compile with "expected
# declaration, found diff", and it reached main. The pre-commit framework
# stashes unstaged changes as a patch before running file-modifying hooks and
# restores them afterward; the bytes that ended up in the source match a unified
# diff of the intended one-line change, so the restore appears to have written
# the patch TEXT rather than applying it (issue #5612).
#
# The corruption is self-masking: the gate that would catch it cannot build the
# module, so it reports a parse error rather than the cause. It is introduced at
# commit time on the author machine, after review and after the PR's CI ran on
# the pre-corruption content, so nothing on the originating PR flags it.
#
# This check does not need to know which hook misbehaved. A diff fragment in a
# source file is never intentional, so rejecting it fails closed on exactly this
# corruption whatever produced it.
#
# Modes:
#   (default)      tree mode — scan every tracked file. Used by CI so a
#                  self-masking break cannot persist on main.
#   --staged       scan staged file content. Used by pre-commit, which is the
#                  point where the corruption is introduced.
#
# Exit 0 when clean; 1 listing each offending file, line number and line.
set -euo pipefail

repo_root="${ESHU_DIFF_FRAGMENT_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(cd "$(dirname "$0")/.." && pwd)"
fi
cd "$repo_root"

mode="tree"
case "${1:-}" in
  --staged) mode="staged" ;;
  "") ;;
  *) echo "usage: $(basename "$0") [--staged]" >&2; exit 2 ;;
esac

# A unified-diff header, a hunk header, or a conflict marker. Anchored at line
# start: an "@@" mid-sentence in prose is not a hunk header, and a Go operator
# line beginning with "<<" is not a conflict marker (a conflict marker is
# exactly seven angle brackets followed by a space or end of line).
pattern='^diff --git |^@@ -[0-9]+(,[0-9]+)? \+[0-9]+(,[0-9]+)? @@|^<<<<<<<( |$)|^>>>>>>>( |$)|^\|\|\|\|\|\|\|( |$)'

# Markdown legitimately shows diffs and conflict markers when documenting them,
# and .diff/.patch files ARE diffs. Everything else is source. Expressed as git
# pathspecs so the exclusion happens inside the single git grep rather than in a
# shell loop.
#
# The gate and its own test necessarily contain the patterns they match.
exclusions=(
  ':(exclude)*.md'
  ':(exclude)*.mdx'
  ':(exclude)*.diff'
  ':(exclude)*.patch'
  ':(exclude)scripts/verify-no-diff-fragments.sh'
  ':(exclude)scripts/test-verify-no-diff-fragments.sh'
)

# One git grep over the whole set, not a grep per line. The first version of
# this script spawned a subprocess per line and took minutes on this repo; a
# gate too slow to run is a gate that gets disabled.
#
# --cached searches the INDEX, which is what makes staged mode correct: the
# corruption is in what is about to be committed, which need not match the
# worktree.
grep_args=(grep -n -E "$pattern")
if [ "$mode" = "staged" ]; then
  grep_args=(grep --cached -n -E "$pattern")
fi

matches=""
if ! matches="$(git "${grep_args[@]}" -- . "${exclusions[@]}" 2>/dev/null)"; then
  # git grep exits 1 when nothing matched, which is the clean case.
  matches=""
fi

violations=0
if [ -n "$matches" ]; then
  while IFS= read -r match; do
    [ -n "$match" ] || continue
    printf 'verify-no-diff-fragments: %s\n' "$match" >&2
    violations=$((violations + 1))
  done <<< "$matches"
fi

if [ "$violations" -gt 0 ]; then
  cat >&2 <<'EOF'

A source file contains raw diff or conflict-marker text. This is corruption,
not code: it does not compile, and the compiler error names the symptom rather
than the cause.

If this appeared during a commit, the pre-commit patch-restore is the suspect
(issue #5612). Recover the intended content from the file's last good state
rather than hand-deleting the fragment, since the restore may have dropped real
changes as well as added the blob.
EOF
  exit 1
fi

echo "verify-no-diff-fragments: no diff fragments or conflict markers in tracked source files"
