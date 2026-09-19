#!/usr/bin/env bash

# Sourced by verify-parser-relationship-kit.sh, after is_language_query_source
# and $base/$repo_root/$changed_files are set. Kept out of the parent script
# so that script stays below the repository's 500-line cap.
# shellcheck disable=SC2154 # Parent defines base, repo_root, script_dir, changed_files.

# A comment-only edit to a language-query or relationship source cannot change
# the behavior their contribution docs and tests prove, so it is exempt from
# those paired-update requirements. This covers path corrections in Go doc
# comments as well as ordinary prose maintenance. "Comment-only" is decided by
# comparing the base and head versions' Go TOKEN STREAMS (go/cmd/token-diff),
# not by pattern-matching diff lines: a line-based "starts with //" rule
# cannot tell a real comment from a `//go:build`/`//go:generate`/`//go:embed`/
# `//line`/`// +build` directive, or from a raw-string line that happens to
# start with `//` (embedded Cypher, SQL, or other query text is data, not a
# comment). See go/cmd/token-diff/doc.go for the exact rule token-diff applies
# (plain `//` comments dropped; block comments, directives, and the
# automatically-inserted end-of-line SEMICOLON tokens kept; any `import "C"`
# file never exempt).
#
# The same exemption also covers a repository-internal package move seen from
# an importing file (#6818), in two steps that must both hold:
#
#   1. token-diff -allow-internal-import-rename proves the file's only
#      difference is one unaliased import path under
#      github.com/eshu-hq/eshu/go/internal/ replaced by another, plus every
#      `old.X` qualifier rewritten to `new.X` (old/new are the paths' last
#      elements), with no other token changed. That is decided on the token
#      stream, never by rewriting diff text: a string literal edited from
#      "old.x" to "new.x" is still a real change. See
#      go/cmd/token-diff/doc.go for the full rule.
#   2. is_internal_package_move proves, from git, that the substitution is a
#      real move rather than a swap between implementations: the old
#      package directory had Go files at the merge base and has none at
#      HEAD, and the new one had none at the merge base and has some at
#      HEAD. Pointing an importer at a package that already existed, or at
#      one that does not replace the old, is a real change.
#
# The exemption guards both the language-query rule and the relationship
# rule (has_non_comment_language_query_change and
# has_non_comment_relationship_change below).
#
# This function owns everything git-shaped: resolving the same base blob the
# gate's own diff uses (three-dot merge-base semantics, falling back to a
# direct two-dot diff against $base exactly like the top-level diff above),
# and refusing the exemption outright for an added, deleted, or renamed path
# -- there is no meaningful "base version" to compare in those cases, so
# token-diff is never even invoked for them. Fails closed throughout: any git,
# read, or tool failure counts as a real change, same as if this function
# did not exist.
is_comment_only_diff() {
  local file="$1"
  if [ -z "$base" ]; then
    return 1
  fi
  if is_dead_code_maturity_source "$file"; then
    # code_dead_code_language_maturity.go also matches is_language_query_source's
    # glob (it has no explicit exclusion there, unlike *language_inventory.go
    # and content_reader_language.go) and always fired the language rule
    # unconditionally before this exemption existed, on top of its own
    # dead-code rule. Keep that pre-existing double coverage: the exemption
    # never applies to this file, comment-only or not.
    return 1
  fi
  local merge_base
  merge_base="$(git -C "$repo_root" merge-base "$base" HEAD 2>/dev/null)" || merge_base="$base"

  local base_tmp
  base_tmp="$(mktemp)" || return 1
  if ! git -C "$repo_root" show "${merge_base}:${file}" >"$base_tmp" 2>/dev/null; then
    rm -f "$base_tmp"
    return 1 # no base version (added, or the path changed) -- always a change.
  fi

  # Read the head version from the committed HEAD, never the worktree: the
  # diff this gate judges is base...HEAD (changed_files comes from that same
  # range), so a worktree read fails open both directions -- an uncommitted
  # revert back to comment-only would wrongly exempt a real committed code
  # change, and an uncommitted code edit on top of a committed comment-only
  # change would wrongly block it.
  local head_tmp
  head_tmp="$(mktemp)" || { rm -f "$base_tmp"; return 1; }
  if ! git -C "$repo_root" show "HEAD:${file}" >"$head_tmp" 2>/dev/null; then
    rm -f "$base_tmp" "$head_tmp"
    return 1 # no head version (deleted, or the path changed) -- always a change.
  fi

  # token-diff is a real Go tool that lives in THIS checkout (script_dir's
  # repo), not in the arbitrary $repo_root under test -- a throwaway fixture
  # repo (as the self-tests use) has no go/cmd/token-diff at all, so it must
  # be built/run from script_dir's own go/ module, never from $repo_root/go.
  local rc=1 verdict rename_paths
  if verdict="$( cd "$script_dir/../go" && env -u GOROOT go run ./cmd/token-diff \
    -allow-internal-import-rename \
    -base "$base_tmp" -head "$head_tmp" 2>/dev/null )"; then
    # token-diff's stdout contract (go/cmd/token-diff/doc.go): a rename
    # verdict is exactly one line naming both import paths. Anything else
    # on exit 0 is the token-identical comment-only verdict.
    rename_paths="$(printf '%s\n' "$verdict" | sed -n 's/^token-diff: internal import rename only: \([^ ]*\) -> \([^ ]*\) .*/\1 \2/p')"
    case "$verdict" in
      *"internal import rename only: "*)
        # shellcheck disable=SC2086 # two space-free import paths, split on purpose.
        if [ -n "$rename_paths" ] && is_internal_package_move "$merge_base" $rename_paths; then
          rc=0
        fi
        ;;
      *) rc=0 ;;
    esac
  fi
  rm -f "$base_tmp" "$head_tmp"
  return "$rc"
}

# go_package_dir_has_files REF DIR succeeds when DIR holds at least one .go
# file directly (not in a subdirectory) at REF. Any git failure counts as no
# files. The ls-tree output is captured before matching instead of piped
# into `rg -q`: under the verifier's pipefail, rg -q exits on the first match
# and a large directory listing can SIGPIPE ls-tree, turning "has files" into
# "no files" at random. That fails open in the negated move checks below.
go_package_dir_has_files() {
  local listing
  listing="$(git -C "$repo_root" ls-tree --name-only "$1" -- "$2/" 2>/dev/null || true)"
  [ -n "$listing" ] && printf '%s\n' "$listing" | rg '\.go$' >/dev/null
}

# is_internal_package_move MERGE_BASE OLD_IMPORT NEW_IMPORT succeeds only when
# the import-path substitution token-diff reported is a real package move in
# this diff: OLD's directory had Go files at MERGE_BASE and has none at HEAD,
# and NEW's directory had none at MERGE_BASE and has some at HEAD. Both import
# paths map to repo directories through the go/ module root
# (github.com/eshu-hq/eshu/go/...). Anything else fails closed.
is_internal_package_move() {
  local merge_base="$1" old_import="$2" new_import="$3" module="github.com/eshu-hq/eshu/"
  case "$old_import" in "$module"go/internal/*) ;; *) return 1 ;; esac
  case "$new_import" in "$module"go/internal/*) ;; *) return 1 ;; esac
  local old_dir="${old_import#"$module"}" new_dir="${new_import#"$module"}"
  go_package_dir_has_files "$merge_base" "$old_dir" || return 1
  ! go_package_dir_has_files HEAD "$old_dir" || return 1
  ! go_package_dir_has_files "$merge_base" "$new_dir" || return 1
  go_package_dir_has_files HEAD "$new_dir" || return 1
  return 0
}

has_non_comment_language_query_change() {
  local file
  for file in "${changed_files[@]}"; do
    if is_language_query_source "$file" && ! is_comment_only_diff "$file"; then
      return 0
    fi
  done
  return 1
}

has_non_comment_relationship_change() {
  local file
  for file in "${changed_files[@]}"; do
    if is_relationship_source "$file" && ! is_comment_only_diff "$file"; then
      return 0
    fi
  done
  return 1
}
