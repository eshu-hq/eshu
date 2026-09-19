#!/usr/bin/env bash

# Sourced by test-verify-parser-relationship-kit.sh after its fixture helpers
# are defined. Kept out of the parent test driver so that driver stays below
# the repository's 500-line cap.
# shellcheck disable=SC2154 # Parent defines fixture helpers and repo paths.

# #6818: a repository-internal package move rewrites the import path and the
# package qualifier in every importer. When that is the file's only change,
# the language-query and relationship rules must not demand doc or test
# updates. go/cmd/token-diff -allow-internal-import-rename proves the file
# differs only by one import-path-plus-qualifier substitution; the gate then
# proves the substitution is a real move from git: the old package directory
# had Go files at the base and has none at HEAD, and the new one had none at
# the base and has some at HEAD. Swapping an importer to a different package
# that already existed, or to one that did not replace the old, is a real
# change. These cases run end to end through the real gate.

# rename_case name importer base-src head-src base-pkgs head-pkgs expect
#   importer   repo-relative path of the file under test.
#   base-pkgs  space-separated repo-relative package dirs present at the base.
#   head-pkgs  the package dirs present at HEAD.
#   expect     pass or fail.
rename_case() {
  local name="$1" importer="$2" base_src="$3" head_src="$4"
  local base_pkgs="$5" head_pkgs="$6" expect="$7" r dir
  r="$(init_repo "$name")"
  mkdir -p "${r}/$(dirname "$importer")"
  for dir in $base_pkgs; do
    mkdir -p "${r}/${dir}"
    printf 'package %s\n\n// X is a placeholder.\nfunc X() {}\n' "$(basename "$dir")" >"${r}/${dir}/pkg.go"
  done
  printf '%s\n' "$base_src" >"${r}/${importer}"
  git -C "${r}" add .
  git -C "${r}" commit -q -m 'rename baseline'
  for dir in $base_pkgs; do
    case " $head_pkgs " in
      *" $dir "*) ;;
      *) git -C "${r}" rm -q -r "${dir}" ;;
    esac
  done
  for dir in $head_pkgs; do
    mkdir -p "${r}/${dir}"
    printf 'package %s\n\n// X is a placeholder.\nfunc X() {}\n' "$(basename "$dir")" >"${r}/${dir}/pkg.go"
  done
  printf '%s\n' "$head_src" >"${r}/${importer}"
  git -C "${r}" add -A .
  git -C "${r}" commit -q -m 'rename change'
  if [ "$expect" = pass ]; then
    expect_pass "${r}"
  else
    expect_fail "${r}"
  fi
}

# importer_source PACKAGE-CLAUSE IMPORT-PATH QUALIFIER [ROUTE]
importer_source() {
  printf 'package %s

import (
	"net/http"

	"github.com/eshu-hq/eshu/%s"
)

// languageTracer is seeded from %s.X.
var languageTracer = %s.X

func languageSpan(r *http.Request) {
	_ = %s.X
	_ = "%s"
}' "$1" "$2" "$3" "$3" "$3" "${4:-route}"
}

lang=go/internal/query/language_dsl_base.go
old_pkg=go/internal/query/queryspan
new_pkg=go/internal/query/tracing
old_src="$(importer_source query "$old_pkg" queryspan)"
new_src="$(importer_source query "$new_pkg" tracing)"

# R1: a real move, old package gone and new one added -- exempt.
rename_case rename-r1-real-move "$lang" "$old_src" "$new_src" \
  "$old_pkg" "$new_pkg" pass

# R2: the same move plus a real code change in the importer -- NOT exempt.
rename_case rename-r2-plus-code-change "$lang" "$old_src" \
  "$(importer_source query "$new_pkg" tracing other-route)" \
  "$old_pkg" "$new_pkg" fail

# R3: the qualifier renamed inconsistently (two different new names) -- NOT
# exempt.
rename_case rename-r3-inconsistent-qualifier "$lang" "$old_src" \
  "$(importer_source query "$new_pkg" tracing | sed 's/_ = tracing\.X/_ = tracer.X/')" \
  "$old_pkg" "$new_pkg" fail

# R4: a non-internal import path changes the same way -- NOT exempt; only a
# move under github.com/eshu-hq/eshu/go/internal/ qualifies.
rename_case rename-r4-non-internal-import "$lang" \
  "$(printf '%s' "$old_src" | sed 's#github.com/eshu-hq/eshu/go/internal/query/queryspan#example.com/lib/queryspan#')" \
  "$(printf '%s' "$new_src" | sed 's#github.com/eshu-hq/eshu/go/internal/query/tracing#example.com/lib/tracing#')" \
  "$old_pkg" "$new_pkg" fail

# R5: the importer switches to a different package that already existed, and
# the old one still exists -- a behavior swap, NOT exempt.
rename_case rename-r5-swap-existing "$lang" \
  "$(importer_source query go/internal/query/langv1 langv1)" \
  "$(importer_source query go/internal/query/langv2 langv2)" \
  "go/internal/query/langv1 go/internal/query/langv2" \
  "go/internal/query/langv1 go/internal/query/langv2" fail

# R6: same-qualifier swap (v1/parser -> v2/parser, body unchanged) while
# v1/parser still exists -- NOT exempt.
rename_case rename-r6-same-qualifier-swap "$lang" \
  "$(importer_source query go/internal/query/v1/parser parser)" \
  "$(importer_source query go/internal/query/v2/parser parser)" \
  "go/internal/query/v1/parser go/internal/query/v2/parser" \
  "go/internal/query/v1/parser go/internal/query/v2/parser" fail

# R7: the old package is deleted but the new one already existed at the
# base -- a consolidation onto another implementation, NOT exempt.
rename_case rename-r7-onto-preexisting "$lang" "$old_src" "$new_src" \
  "$old_pkg $new_pkg" "$new_pkg" fail

# R8: the old package is deleted and the new path has no Go files at HEAD --
# not a move, NOT exempt.
rename_case rename-r8-new-missing "$lang" "$old_src" "$new_src" \
  "$old_pkg" "" fail

# R11: the old import path never had a package at the base (the importer
# was already broken) -- nothing moved, NOT exempt.
rename_case rename-r11-old-missing "$lang" "$old_src" "$new_src" \
  "" "$new_pkg" fail

# R12: the new package is added but the old one is kept (a copy or fork, not
# a move) -- the importer now runs different code, NOT exempt.
rename_case rename-r12-copy-not-move "$lang" "$old_src" "$new_src" \
  "$old_pkg" "$old_pkg $new_pkg" fail

# R9/R10: the same exemption guards the relationship rule. A real move in a
# go/internal/relationships source is exempt from the relationship test/doc
# requirement; a swap to an existing package is not.
rel=go/internal/relationships/mapper.go
rename_case rename-r9-relationship-move "$rel" \
  "$(importer_source relationships go/internal/rtrace/oldspan oldspan)" \
  "$(importer_source relationships go/internal/rtrace/newspan newspan)" \
  go/internal/rtrace/oldspan go/internal/rtrace/newspan pass
rename_case rename-r10-relationship-swap "$rel" \
  "$(importer_source relationships go/internal/rtrace/oldspan oldspan)" \
  "$(importer_source relationships go/internal/rtrace/newspan newspan)" \
  "go/internal/rtrace/oldspan go/internal/rtrace/newspan" \
  "go/internal/rtrace/oldspan go/internal/rtrace/newspan" fail
