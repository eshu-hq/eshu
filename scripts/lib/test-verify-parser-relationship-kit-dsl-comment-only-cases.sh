#!/usr/bin/env bash

# Sourced by test-verify-parser-relationship-kit.sh after its fixture helpers
# are defined. Keep the comment-only-diff exemption cases outside the parent
# test driver so that driver stays below the repository's 500-line cap.
# shellcheck disable=SC2154 # Parent defines fixture helpers and repo paths.

# Owner ruling (#6647): a comment-only edit to a language-query-source file
# is exempt from the doc-update requirement only when its Go TOKEN STREAM is
# unchanged (go/cmd/tokendiff), never by pattern-matching diff lines -- see
# that package's doc.go for the exact rule and why a line-based "starts with
# //" check is unsafe. Each case below adds a second, baseline commit (the
# DSL source file, untouched) before the commit under test, so
# `ESHU_PARSER_RELATIONSHIP_KIT_BASE=HEAD~1` diffs a real edit rather than a
# whole-file addition. This is a representative subset of the full
# adversarial acceptance list; it is not exhaustive by construction.
dsl_base_source() {
  cat <<'GO'
package query

// ExecuteLanguageQuery documents the DSL entry point.
func ExecuteLanguageQuery() {}
GO
}

dsl_case() { # name base-content head-content expect(pass|fail)
  local name="$1" base="$2" head="$3" expect="$4" r
  r="$(init_repo "$name")"
  printf '%s' "$base" >"${r}/go/internal/query/language_dsl_base.go"
  git -C "${r}" add .
  git -C "${r}" commit -q -m 'dsl source baseline'
  printf '%s' "$head" >"${r}/go/internal/query/language_dsl_base.go"
  git -C "${r}" add .
  git -C "${r}" commit -q -m 'dsl source change'
  if [ "$expect" = pass ]; then
    expect_pass "${r}"
  else
    expect_fail "${r}"
  fi
}

# A: a plain // comment's text changes -- exempt.
dsl_case dsl-a-comment-only \
  "$(dsl_base_source)" \
  'package query

// ExecuteLanguageQuery documents the DSL entry point.
// Comment-only edit, no behavior change.
func ExecuteLanguageQuery() {}
' \
  pass

# G: only a trailing // comment's text changes on an otherwise-identical
# code line -- exempt (token-equal; the comment is dropped from both sides).
dsl_case dsl-g-trailing-comment \
  'package query

const languageTrailing = 1 // a
' \
  'package query

const languageTrailing = 1 // b
' \
  pass

# K: whitespace-only change on a code line (extra space) -- exempt
# (go/scanner ignores inter-token whitespace).
dsl_case dsl-k-whitespace \
  'package query

const languageWhitespace = 1
' \
  'package query

const  languageWhitespace = 1
' \
  pass

# K2: a newline moves the automatic-semicolon statement boundary without
# changing any comment -- NOT exempt. Base ends its first line on the
# operator (no semicolon inserted there); head ends it on the operand (a
# semicolon IS inserted there), so the SEMICOLON-inclusive token streams
# differ even though only whitespace/newlines moved.
dsl_case dsl-k2-semicolon-boundary \
  'package query

func languageBoundary(a, b int) int {
	x := a +
		b
	return x
}
' \
  'package query

func languageBoundary(a, b int) int {
	x := a
	+b
	return x
}
' \
  fail

# D: a new /* ... */ block comment is added -- NOT exempt (block comments
# are kept in the compared token stream, never dropped).
dsl_case dsl-d-block-comment-added \
  "$(dsl_base_source)" \
  'package query

// ExecuteLanguageQuery documents the DSL entry point.
/* block */
func ExecuteLanguageQuery() {}
' \
  fail

# E: a //go:build directive changes value -- NOT exempt (directive comments
# are kept in the compared token stream).
dsl_case dsl-e-gobuild-directive \
  '//go:build linux

package query

const languageBuild = 1
' \
  '//go:build darwin

package query

const languageBuild = 1
' \
  fail

# F: a raw-string line happens to start with // -- it is query text, not a
# Go comment, so go/scanner tokenizes the whole backtick string as one
# STRING token and the edit is NOT exempt.
dsl_case dsl-f-rawstring-slashslash \
  'package query

const languageQuery = `
MATCH (n)
// keep
RETURN n
`
' \
  'package query

const languageQuery = `
MATCH (n)
// changed
RETURN n
`
' \
  fail

# H: a raw-string line is removed, and its diff hunk incidentally renders as
# "-- note" -- a plain-diff classifier could mistake that for a "---" file
# header and skip it. This design never parses diff text at all, so the
# removed line just shows up as a shorter STRING token literal.
dsl_case dsl-h-rawstring-removed-line \
  'package query

const languageRemoved = `
SELECT 1
-- note
`
' \
  'package query

const languageRemoved = `
SELECT 1
`
' \
  fail

# I1: a brand-new language-query-source file is added -- always a change;
# there is no base version to compare, so tokendiff is never even invoked.
i1_repo="$(init_repo dsl-i1-new-file)"
printf '%s' "$(dsl_base_source)" >"${i1_repo}/go/internal/query/language_dsl_base.go"
git -C "${i1_repo}" add . && git -C "${i1_repo}" commit -q -m 'dsl source baseline'
printf 'package query\n// only a comment\n' >"${i1_repo}/go/internal/query/language_dsl_new.go"
git -C "${i1_repo}" add . && git -C "${i1_repo}" commit -q -m 'add new language file'
expect_fail "${i1_repo}"

# CGO: a file with `import "C"` is never exempt, even for an edit confined
# to its cgo preamble comment -- that comment compiles as C source.
dsl_case dsl-cgo-preamble \
  'package query

// #include <stdio.h>
import "C"

const languageCgo = 1
' \
  'package query

// #include <stdlib.h>
import "C"

const languageCgo = 1
' \
  fail

# O: code_dead_code_language_maturity.go also matches is_language_query_source's
# glob (no explicit exclusion there, unlike *language_inventory.go and
# content_reader_language.go) and always fired the language rule
# unconditionally before this exemption existed, on top of its own
# dead-code rule. The exemption must not change that: even a comment-only
# edit to this exact file still fires BOTH rules.
o_repo="$(init_repo dsl-o-deadcode-comment)"
printf 'package query\n\n// maturity one\nconst deadCodeX = 1\n' >"${o_repo}/go/internal/query/code_dead_code_language_maturity.go"
git -C "${o_repo}" add . && git -C "${o_repo}" commit -q -m 'dead-code maturity baseline'
printf 'package query\n\n// maturity two\nconst deadCodeX = 1\n' >"${o_repo}/go/internal/query/code_dead_code_language_maturity.go"
git -C "${o_repo}" add . && git -C "${o_repo}" commit -q -m 'dead-code maturity comment-only edit'
expect_fail "${o_repo}"
if ! rg -qF 'dead-code maturity map changed' /tmp/eshu-parser-relationship-kit.err; then
  printf 'dsl-o-deadcode-comment: expected the dead-code rule to also fire\n' >&2
  sed -n '1,160p' /tmp/eshu-parser-relationship-kit.err >&2
  exit 1
fi
