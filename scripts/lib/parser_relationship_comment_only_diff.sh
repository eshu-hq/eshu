#!/usr/bin/env bash

# Sourced by verify-parser-relationship-kit.sh, after is_language_query_source
# and $base/$repo_root/$changed_files are set. Kept out of the parent script
# so that script stays below the repository's 500-line cap.
# shellcheck disable=SC2154 # Parent defines base, repo_root, script_dir, changed_files.

# Owner ruling (#6647): a comment-only edit to a language-query-source file
# cannot change the DSL behavior language-query-dsl.md documents, so it is
# exempt from the doc-update requirement below. "Comment-only" is decided by
# comparing the base and head versions' Go TOKEN STREAMS (go/cmd/tokendiff),
# not by pattern-matching diff lines: a line-based "starts with //" rule
# cannot tell a real comment from a `//go:build`/`//go:generate`/`//go:embed`/
# `//line`/`// +build` directive, or from a raw-string line that happens to
# start with `//` (embedded Cypher, SQL, or other query text is data, not a
# comment). See go/cmd/tokendiff/doc.go for the exact rule tokendiff applies
# (plain `//` comments dropped; block comments, directives, and the
# automatically-inserted end-of-line SEMICOLON tokens kept; any `import "C"`
# file never exempt).
#
# This function owns everything git-shaped: resolving the same base blob the
# gate's own diff uses (three-dot merge-base semantics, falling back to a
# direct two-dot diff against $base exactly like the top-level diff above),
# and refusing the exemption outright for an added, deleted, or renamed path
# -- there is no meaningful "base version" to compare in those cases, so
# tokendiff is never even invoked for them. Fails closed throughout: any git,
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

  local head_path="$repo_root/$file"
  if [ ! -f "$head_path" ]; then
    rm -f "$base_tmp"
    return 1 # no head version (deleted, or the path changed) -- always a change.
  fi

  # tokendiff is a real Go tool that lives in THIS checkout (script_dir's
  # repo), not in the arbitrary $repo_root under test -- a throwaway fixture
  # repo (as the self-tests use) has no go/cmd/tokendiff at all, so it must
  # be built/run from script_dir's own go/ module, never from $repo_root/go.
  local rc=1
  if ( cd "$script_dir/../go" && env -u GOROOT go run ./cmd/tokendiff \
    -base "$base_tmp" -head "$head_path" >/dev/null 2>&1 ); then
    rc=0
  fi
  rm -f "$base_tmp"
  return "$rc"
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
