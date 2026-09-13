#!/usr/bin/env bash

# Sourced by verify-parser-relationship-kit.sh, before any call to
# is_dead_code_maturity_source -- that includes parser_relationship_comment_
# only_diff.sh's case-O guard, which calls it at run time, so this file must
# be sourced before that helper. Kept in its own file rather than folded into
# parser_relationship_language_ledger.sh: the dead-code maturity map
# (code_dead_code_language_maturity.go) is a small, self-contained
# diff-contract pair, not part of the language feature ledger's own subject
# matter, and did not belong there just because that file had spare room
# under the 500-line cap.

# is_dead_code_maturity_source reports whether $1 is the dead-code language
# maturity map source file.
is_dead_code_maturity_source() {
  local path="$1"
  [ "$path" = "go/internal/query/code_dead_code_language_maturity.go" ]
}

# is_dead_code_maturity_doc reports whether $1 is a doc page that documents
# the dead-code language maturity map.
is_dead_code_maturity_doc() {
  local path="$1"
  case "$path" in
    docs/public/reference/dead-code-language-maturity.md|docs/public/languages/*.md) return 0 ;;
    *) return 1 ;;
  esac
}
