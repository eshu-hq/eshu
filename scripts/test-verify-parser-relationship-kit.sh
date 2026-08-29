#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-parser-relationship-kit.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

write_required_docs() {
  local dir="$1"
  mkdir -p \
    "${dir}/docs/public/languages" \
    "${dir}/docs/public/reference" \
    "${dir}/go/internal/parser" \
    "${dir}/go/internal/parser/cloudformation" \
    "${dir}/go/internal/parser/dockerfile" \
    "${dir}/go/internal/parser/hcl" \
    "${dir}/go/internal/parser/yaml" \
    "${dir}/go/internal/relationships" \
    "${dir}/go/internal/query" \
    "${dir}/specs"

  # Delivered from a sibling fixture file, not a heredoc: Homebrew bash >= 5.1
  # writes an entire heredoc body to a pipe before forking the reader, and
  # macOS's 512-byte pipe buffer deadlocks on this ~588B body (#5074). The
  # body is fully static (was a quoted heredoc, no shell expansion), so the
  # file is byte-identical to the original heredoc body.
  cat "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-contributing-language-support.md" >"${dir}/docs/public/contributing-language-support.md"

  cat >"${dir}/docs/public/reference/language-query-dsl.md" <<'MD'
# Language Query DSL

## Adding Or Promoting Language Query Support

Parse-only behavior is not supported query behavior. Query changes update this
page and the affected language page.
MD

  cat >"${dir}/docs/public/reference/relationship-mapping.md" <<'MD'
# Relationship Mapping

## Relationship Extractor Contribution Kit

Relationship changes include positive, negative, and ambiguous fixtures plus
query/story proof.
MD

  cat >"${dir}/docs/public/languages/support-maturity.md" <<'MD'
# Parser Support Matrix

| Parser | Parser Class | Grammar Routing | Normalization | Framework Or Root Evidence | Modeled Evidence | Query Surfacing | Real-Repo Validation | End-to-End Indexing |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Python | `DefaultEngine (python)` | supported | supported | derived roots | FastAPI routes | supported | supported | supported |
| JSON Config | `DefaultEngine (json)` | - | - | unsupported | JSON metadata only | - | - | - |
MD

  # Delivered from a sibling fixture file, not a heredoc: Homebrew bash >= 5.1
  # writes an entire heredoc body to a pipe before forking the reader, and
  # macOS's 512-byte pipe buffer deadlocks on this ~817B body (#5074). The
  # body is fully static (was a quoted heredoc, no shell expansion), so the
  # file is byte-identical to the original heredoc body.
  cat "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-support-maturity-parser-backing-append.md" >>"${dir}/docs/public/languages/support-maturity.md"

  cat >>"${dir}/docs/public/languages/support-maturity.md" <<'MD'

## Language Feature Parity Ledger

See `specs/language-feature-parity-ledger.v1.yaml`.
MD

  # Delivered from a sibling fixture file, not a heredoc: Homebrew bash >= 5.1
  # writes an entire heredoc body to a pipe before forking the reader, and
  # macOS's 512-byte pipe buffer deadlocks on this ~1233B body (#5074). The
  # body is fully static (was a quoted heredoc, no shell expansion), so the
  # file is byte-identical to the original heredoc body.
  cat "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-parser-backing-ledger.yaml" >"${dir}/specs/parser-backing-ledger.v1.yaml"

  cat >"${dir}/specs/language-feature-parity-ledger.v1.yaml" <<'YAML'
version: 1
language_features:
  - language: python
    docs_claim: docs/public/languages/python.md
    parser_backing: tree-sitter-backed
    no_provider_required: true
    supported_features: [functions]
    partial_features: [django-drf-routes]
    derived_features: []
    source_files:
      - go/internal/parser/python_language.go
    test_files:
      - go/internal/parser/python_language_test.go
    docs:
      - docs/public/languages/python.md
    read_surfaces:
      - execute_language_query
YAML

  # Delivered from a sibling fixture file, not a heredoc: Homebrew bash >= 5.1
  # writes an entire heredoc body to a pipe before forking the reader, and
  # macOS's 512-byte pipe buffer deadlocks on this ~640B body (#5074). The
  # body is fully static (was a quoted heredoc, no shell expansion), so the
  # file is byte-identical to the original heredoc body.
  cat "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-python.md" >"${dir}/docs/public/languages/python.md"

  for path in \
    go/internal/parser/cloudformation/parser.go \
    go/internal/parser/cloudformation/parser_test.go \
    go/internal/parser/dockerfile/metadata.go \
    go/internal/parser/dockerfile/metadata_test.go \
    go/internal/parser/hcl/parser.go \
    go/internal/parser/hcl/parser_test.go \
    go/internal/parser/yaml/language.go \
    go/internal/parser/yaml/language_test.go \
    go/internal/parser/python_language.go \
    go/internal/parser/python_language_test.go
  do
    printf 'package placeholder\n' >"${dir}/${path}"
  done

  for path in \
    docs/public/languages/cloudformation.md \
    docs/public/languages/terraform.md \
    docs/public/languages/kubernetes.md
  do
    printf '# Fixture\n' >"${dir}/${path}"
  done

  cat >"${dir}/docs/public/reference/dead-code-language-maturity.md" <<'MD'
# Dead Code Language Maturity

## Promotion Rule

Promotions update parser tests, query tests, language pages, and this matrix.
MD
}

init_repo() {
  local name="$1"
  local dir="${tmp_root}/${name}"
  mkdir -p "${dir}"
  git -C "${dir}" init -q
  git -C "${dir}" config user.email "test@example.invalid"
  git -C "${dir}" config user.name "Eshu Test"
  write_required_docs "${dir}"
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m initial
  printf '%s\n' "${dir}"
}

run_verifier() {
  local dir="$1"
  ESHU_PARSER_RELATIONSHIP_KIT_REPO_ROOT="${dir}" \
    ESHU_PARSER_RELATIONSHIP_KIT_BASE=HEAD~1 \
    "${verifier}" >/tmp/eshu-parser-relationship-kit.out 2>/tmp/eshu-parser-relationship-kit.err
}

expect_pass() {
  local dir="$1"
  if ! run_verifier "${dir}"; then
    printf 'expected verifier to pass in %s\n' "${dir}" >&2
    sed -n '1,160p' /tmp/eshu-parser-relationship-kit.err >&2
    exit 1
  fi
}

expect_fail() {
  local dir="$1"
  if run_verifier "${dir}"; then
    printf 'expected verifier to fail in %s\n' "${dir}" >&2
    sed -n '1,160p' /tmp/eshu-parser-relationship-kit.out >&2
    exit 1
  fi
}

plain_repo="$(init_repo plain)"
printf '# docs only\n' >"${plain_repo}/README.md"
git -C "${plain_repo}" add .
git -C "${plain_repo}" commit -q -m 'docs only'
expect_pass "${plain_repo}"

parser_missing_docs_repo="$(init_repo parser-missing-docs)"
printf 'package parser\nfunc parseNewLanguage() {}\n' >"${parser_missing_docs_repo}/go/internal/parser/new_language.go"
printf 'package parser\nfunc TestNewLanguage(t interface{}) {}\n' >"${parser_missing_docs_repo}/go/internal/parser/new_language_test.go"
git -C "${parser_missing_docs_repo}" add .
git -C "${parser_missing_docs_repo}" commit -q -m 'parser without docs'
expect_fail "${parser_missing_docs_repo}"

parser_missing_tests_repo="$(init_repo parser-missing-tests)"
printf 'package parser\nfunc parseNewLanguage() {}\n' >"${parser_missing_tests_repo}/go/internal/parser/new_language.go"
printf '\nDocumented new parser behavior.\n' >>"${parser_missing_tests_repo}/docs/public/languages/python.md"
git -C "${parser_missing_tests_repo}" add .
git -C "${parser_missing_tests_repo}" commit -q -m 'parser without tests'
expect_fail "${parser_missing_tests_repo}"

parser_complete_repo="$(init_repo parser-complete)"
printf 'package parser\nfunc parseNewLanguage() {}\n' >"${parser_complete_repo}/go/internal/parser/new_language.go"
printf 'package parser\nfunc TestNewLanguage(t interface{}) {}\n' >"${parser_complete_repo}/go/internal/parser/new_language_test.go"
printf '\nDocumented new parser behavior.\n' >>"${parser_complete_repo}/docs/public/languages/python.md"
git -C "${parser_complete_repo}" add .
git -C "${parser_complete_repo}" commit -q -m 'parser with docs and tests'
expect_pass "${parser_complete_repo}"

relationship_missing_docs_repo="$(init_repo relationship-missing-docs)"
printf 'package relationships\nfunc discoverNewEvidence() {}\n' >"${relationship_missing_docs_repo}/go/internal/relationships/new_evidence.go"
printf 'package relationships\nfunc TestDiscoverNewEvidence(t interface{}) {}\n' >"${relationship_missing_docs_repo}/go/internal/relationships/new_evidence_test.go"
git -C "${relationship_missing_docs_repo}" add .
git -C "${relationship_missing_docs_repo}" commit -q -m 'relationship without docs'
expect_fail "${relationship_missing_docs_repo}"

relationship_complete_repo="$(init_repo relationship-complete)"
printf 'package relationships\nfunc discoverNewEvidence() {}\n' >"${relationship_complete_repo}/go/internal/relationships/new_evidence.go"
printf 'package relationships\nfunc TestDiscoverNewEvidence(t interface{}) {}\n' >"${relationship_complete_repo}/go/internal/relationships/new_evidence_test.go"
printf '\nDocumented new relationship evidence family.\n' >>"${relationship_complete_repo}/docs/public/reference/relationship-mapping.md"
git -C "${relationship_complete_repo}" add .
git -C "${relationship_complete_repo}" commit -q -m 'relationship with docs and tests'
expect_pass "${relationship_complete_repo}"

# shellcheck source=scripts/lib/parser_documented_test_commands.sh
. "${repo_root}/scripts/lib/parser_documented_test_commands.sh"
PARSER_SELECTOR_MATCHER_OUTPUT="${tmp_root}/parser-selector-matcher"
# shellcheck source=scripts/lib/test-verify-parser-relationship-kit-cargo-selector-cases.sh
. "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-cargo-selector-cases.sh"

query_missing_dsl_repo="$(init_repo query-missing-dsl)"
printf 'package query\nfunc languageQueryEntityType() {}\n' >"${query_missing_dsl_repo}/go/internal/query/language_queries.go"
printf '\nDocumented new query behavior.\n' >>"${query_missing_dsl_repo}/docs/public/languages/python.md"
git -C "${query_missing_dsl_repo}" add .
git -C "${query_missing_dsl_repo}" commit -q -m 'language query without dsl docs'
expect_fail "${query_missing_dsl_repo}"

# A *_language_inventory.go change (the repositories/by-language +
# language-inventory route handlers) is NOT a Language Query DSL source, so it
# must NOT require a language-query-dsl.md update. This is the paired positive
# for query_missing_dsl_repo above: together they prove is_language_query_source
# still fires for a real DSL source (language_queries.go) but is correctly
# narrowed to skip the by-language inventory handlers.
language_inventory_repo="$(init_repo language-inventory)"
printf 'package query\nfunc listRepositoriesByLanguage() {}\n' >"${language_inventory_repo}/go/internal/query/repository_language_inventory.go"
git -C "${language_inventory_repo}" add .
git -C "${language_inventory_repo}" commit -q -m 'by-language inventory handler without dsl docs'
expect_pass "${language_inventory_repo}"

# content_reader_language.go is the same class as the inventory handlers:
# ListRepoFilesByLanguage is a pushed-down files-by-language read for the
# repository-tree endpoint, called only from repository_tree.go, and
# execute_language_query never reaches it. It must NOT require a
# language-query-dsl.md update either. Paired with query_missing_dsl_repo
# above, which still expect_fails, so the carve-out is proven narrow rather
# than proven absent.
content_reader_language_repo="$(init_repo content-reader-language)"
printf 'package query\nfunc listRepoFilesByLanguage() {}\n' >"${content_reader_language_repo}/go/internal/query/content_reader_language.go"
git -C "${content_reader_language_repo}" add .
git -C "${content_reader_language_repo}" commit -q -m 'repository-tree files-by-language read without dsl docs'
expect_pass "${content_reader_language_repo}"

unsupported_claim_repo="$(init_repo unsupported-claim)"
cat >"${unsupported_claim_repo}/docs/public/languages/support-maturity.md" <<'MD'
# Parser Support Matrix

| Parser | Parser Class | Grammar Routing | Normalization | Framework Or Root Evidence | Modeled Evidence | Query Surfacing | Real-Repo Validation | End-to-End Indexing |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| JSON Config | `DefaultEngine (json)` | - | - | unsupported | JSON metadata only | supported | - | supported |
MD
git -C "${unsupported_claim_repo}" add .
git -C "${unsupported_claim_repo}" commit -q -m 'unsupported query claim'
expect_fail "${unsupported_claim_repo}"

missing_language_proof_repo="$(init_repo missing-language-proof)"
cat >"${missing_language_proof_repo}/docs/public/languages/python.md" <<'MD'
# Python Parser

| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Functions | `functions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/python_language_test.go::TestPythonFunctions` | - | - |
MD
git -C "${missing_language_proof_repo}" add .
git -C "${missing_language_proof_repo}" commit -q -m 'missing language proof'
expect_fail "${missing_language_proof_repo}"

parser_backing_missing_repo="$(init_repo parser-backing-missing)"
rm -f "${parser_backing_missing_repo}/specs/parser-backing-ledger.v1.yaml"
# Delivered from a sibling fixture file, not a heredoc: Homebrew bash >= 5.1
# writes an entire heredoc body to a pipe before forking the reader, and
# macOS's 512-byte pipe buffer deadlocks on this ~724B body (#5074). The
# body is fully static (was a quoted heredoc, no shell expansion), so the
# file is byte-identical to the original heredoc body.
cat "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-support-maturity-missing-ledger.md" >"${parser_backing_missing_repo}/docs/public/languages/support-maturity.md"
git -C "${parser_backing_missing_repo}" add .
git -C "${parser_backing_missing_repo}" commit -q -m 'missing parser backing ledger'
expect_fail "${parser_backing_missing_repo}"

parser_backing_incomplete_repo="$(init_repo parser-backing-incomplete)"
mkdir -p "${parser_backing_incomplete_repo}/specs"
cat >"${parser_backing_incomplete_repo}/specs/parser-backing-ledger.v1.yaml" <<'YAML'
version: 1
parser_backing:
  - parser: cloudformation
    implementation_class: structured-parser-backed-exception
    no_provider_required: true
    source_files:
      - go/internal/parser/cloudformation/parser.go
    test_files:
      - go/internal/parser/cloudformation/parser_test.go
    docs:
      - docs/public/languages/cloudformation.md
YAML
# Delivered from a sibling fixture file, not a heredoc: Homebrew bash >= 5.1
# writes an entire heredoc body to a pipe before forking the reader, and
# macOS's 512-byte pipe buffer deadlocks on this ~1059B body (#5074). The
# body is fully static (was a quoted heredoc, no shell expansion), so the
# file is byte-identical to the original heredoc body.
cat "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-support-maturity-incomplete-ledger.md" >"${parser_backing_incomplete_repo}/docs/public/languages/support-maturity.md"
git -C "${parser_backing_incomplete_repo}" add .
git -C "${parser_backing_incomplete_repo}" commit -q -m 'incomplete parser backing ledger'
expect_fail "${parser_backing_incomplete_repo}"

parser_backing_bad_path_repo="$(init_repo parser-backing-bad-path)"
# Delivered from a sibling fixture file, not a heredoc: Homebrew bash >= 5.1
# writes an entire heredoc body to a pipe before forking the reader, and
# macOS's 512-byte pipe buffer deadlocks on this ~1239B body (#5074). The
# body is fully static (was a quoted heredoc, no shell expansion), so the
# file is byte-identical to the original heredoc body.
cat "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-parser-backing-ledger-bad-path.yaml" >"${parser_backing_bad_path_repo}/specs/parser-backing-ledger.v1.yaml"
git -C "${parser_backing_bad_path_repo}" add .
git -C "${parser_backing_bad_path_repo}" commit -q -m 'stale parser backing ledger path'
expect_fail "${parser_backing_bad_path_repo}"

language_ledger_missing_repo="$(init_repo language-ledger-missing)"
rm -f "${language_ledger_missing_repo}/specs/language-feature-parity-ledger.v1.yaml"
git -C "${language_ledger_missing_repo}" add .
git -C "${language_ledger_missing_repo}" commit -q -m 'missing language feature ledger'
expect_fail "${language_ledger_missing_repo}"

language_ledger_missing_feature_repo="$(init_repo language-ledger-missing-feature)"
printf '| Classes | `classes` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/python_language_test.go::TestPythonClasses` | Compose-backed fixture verification | - |\n' \
  >>"${language_ledger_missing_feature_repo}/docs/public/languages/python.md"
git -C "${language_ledger_missing_feature_repo}" add .
git -C "${language_ledger_missing_feature_repo}" commit -q -m 'language docs claim missing ledger feature'
expect_fail "${language_ledger_missing_feature_repo}"

language_ledger_bad_path_repo="$(init_repo language-ledger-bad-path)"
perl -0pi -e 's#go/internal/parser/python_language.go#go/internal/parser/does_not_exist.go#' \
  "${language_ledger_bad_path_repo}/specs/language-feature-parity-ledger.v1.yaml"
git -C "${language_ledger_bad_path_repo}" add .
git -C "${language_ledger_bad_path_repo}" commit -q -m 'language ledger stale path'
expect_fail "${language_ledger_bad_path_repo}"

parser_backing_complete_repo="$(init_repo parser-backing-complete)"
mkdir -p "${parser_backing_complete_repo}/specs"
# Delivered from a sibling fixture file, not a heredoc: Homebrew bash >= 5.1
# writes an entire heredoc body to a pipe before forking the reader, and
# macOS's 512-byte pipe buffer deadlocks on this ~1233B body (#5074). The
# body is fully static (was a quoted heredoc, no shell expansion), so the
# file is byte-identical to the original heredoc body.
cat "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-parser-backing-ledger-complete.yaml" >"${parser_backing_complete_repo}/specs/parser-backing-ledger.v1.yaml"
# Delivered from a sibling fixture file, not a heredoc: Homebrew bash >= 5.1
# writes an entire heredoc body to a pipe before forking the reader, and
# macOS's 512-byte pipe buffer deadlocks on this ~1625B body (#5074). The
# body is fully static (was a quoted heredoc, no shell expansion), so the
# file is byte-identical to the original heredoc body.
cat "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-support-maturity-complete.md" >"${parser_backing_complete_repo}/docs/public/languages/support-maturity.md"
git -C "${parser_backing_complete_repo}" add .
git -C "${parser_backing_complete_repo}" commit -q -m 'complete parser backing ledger'
expect_pass "${parser_backing_complete_repo}"

# Regression: with neither ESHU_PARSER_RELATIONSHIP_KIT_BASE nor
# GITHUB_BASE_REF set -- the shape of this gate's registry command, which every
# test above bypasses by pinning HEAD~1 -- the base must be the merge base with
# origin/main. A HEAD~1 default scopes the gate to the last commit, so a parser
# added in an earlier commit escapes whenever the tip commit is innocuous.
#
# Gives the fixture a real origin/main to resolve a merge base against: clone
# the repo at its initial commit into a bare origin, then branch away from it.
merge_base_repo="$(init_repo merge-base)"
git -C "${merge_base_repo}" branch -M main
merge_base_origin="${tmp_root}/merge-base-origin"
git clone -q --bare "${merge_base_repo}" "${merge_base_origin}"
git -C "${merge_base_repo}" remote add origin "${merge_base_origin}"
git -C "${merge_base_repo}" fetch -q origin
git -C "${merge_base_repo}" checkout -q -b feature

run_verifier_local_base() {
  local dir="$1"
  env -u ESHU_PARSER_RELATIONSHIP_KIT_BASE -u GITHUB_BASE_REF \
    ESHU_PARSER_RELATIONSHIP_KIT_REPO_ROOT="${dir}" \
    "${verifier}" >/tmp/eshu-parser-relationship-kit.out \
    2>/tmp/eshu-parser-relationship-kit.err
}

# Commit A: a parser with no docs -- the gate must reject this.
printf 'package parser\nfunc parseNewLanguage() {}\n' \
  >"${merge_base_repo}/go/internal/parser/new_language.go"
printf 'package parser\nfunc TestNewLanguage(t interface{}) {}\n' \
  >"${merge_base_repo}/go/internal/parser/new_language_test.go"
git -C "${merge_base_repo}" add .
git -C "${merge_base_repo}" commit -q -m 'branch commit A: parser without docs'
# Commit B: an innocuous tip commit that used to hide commit A from the gate.
printf '# readme\n' >"${merge_base_repo}/README.md"
git -C "${merge_base_repo}" add .
git -C "${merge_base_repo}" commit -q -m 'branch commit B: readme touch'

if run_verifier_local_base "${merge_base_repo}"; then
  printf 'expected the gate to FAIL: the branch adds an undocumented parser in\n' >&2
  printf 'an earlier commit and its tip commit is innocuous. A pass here means\n' >&2
  printf 'the base fell back to HEAD~1 and scoped the gate to the last commit.\n' >&2
  sed -n '1,160p' /tmp/eshu-parser-relationship-kit.out >&2
  exit 1
fi

# The widened window must not fire on a branch with no parser change at all.
merge_base_clean_repo="$(init_repo merge-base-clean)"
git -C "${merge_base_clean_repo}" branch -M main
merge_base_clean_origin="${tmp_root}/merge-base-clean-origin"
git clone -q --bare "${merge_base_clean_repo}" "${merge_base_clean_origin}"
git -C "${merge_base_clean_repo}" remote add origin "${merge_base_clean_origin}"
git -C "${merge_base_clean_repo}" fetch -q origin
git -C "${merge_base_clean_repo}" checkout -q -b feature
printf '# docs only\n' >"${merge_base_clean_repo}/README.md"
git -C "${merge_base_clean_repo}" add .
git -C "${merge_base_clean_repo}" commit -q -m 'branch commit A: docs only'
printf '# docs only, again\n' >"${merge_base_clean_repo}/README.md"
git -C "${merge_base_clean_repo}" add .
git -C "${merge_base_clean_repo}" commit -q -m 'branch commit B: docs only'
if ! run_verifier_local_base "${merge_base_clean_repo}"; then
  printf 'expected a docs-only branch to PASS under the merge-base window\n' >&2
  sed -n '1,160p' /tmp/eshu-parser-relationship-kit.err >&2
  exit 1
fi

# Regression: the CI base path (GITHUB_BASE_REF -> origin/$GITHUB_BASE_REF),
# which every test above bypasses -- they either pin the base env var or leave
# GITHUB_BASE_REF unset. `git fetch origin <branch>` with no `<src>:<dst>`
# destination refspec only ever updates FETCH_HEAD, never
# refs/remotes/origin/<branch>, so under the verify-contracts job's
# fetch-depth: 2 checkout origin/$GITHUB_BASE_REF failed to resolve, the
# merge-base branch found no origin/main either, and the gate ran against
# HEAD~1 -- the tip commit alone -- on every PR run.
#
# The fixture is that checkout: a real shallow clone, a narrow fetch refspec
# that never names the base branch, and no origin/main. A default `git clone`
# configures the wildcard refspec under which even the old bareword fetch
# creates origin/main, and the fixture would prove nothing.
ci_base_repo="${tmp_root}/ci-base"
ci_base_origin="$(init_repo ci-base-origin)"
git -C "${ci_base_origin}" branch -M main
git clone -q --depth=1 "file://${ci_base_origin}" "${ci_base_repo}"
git -C "${ci_base_repo}" config user.email "test@example.invalid"
git -C "${ci_base_repo}" config user.name "Eshu Test"
git -C "${ci_base_repo}" config --unset-all remote.origin.fetch
git -C "${ci_base_repo}" config --add remote.origin.fetch \
  '+refs/heads/unrelated-pr-branch:refs/remotes/origin/unrelated-pr-branch'
git -C "${ci_base_repo}" update-ref -d refs/remotes/origin/main
git -C "${ci_base_repo}" checkout -q -b feature
if git -C "${ci_base_repo}" rev-parse --verify origin/main >/dev/null 2>&1; then
  printf 'fixture is wrong: origin/main resolves before the gate ever runs\n' >&2
  exit 1
fi

# Commit A adds an undocumented parser; commit B is the innocuous tip commit
# that a HEAD~1 base would have scoped the gate to.
printf 'package parser\nfunc parseNewLanguage() {}\n' \
  >"${ci_base_repo}/go/internal/parser/new_language.go"
printf 'package parser\nfunc TestNewLanguage(t interface{}) {}\n' \
  >"${ci_base_repo}/go/internal/parser/new_language_test.go"
git -C "${ci_base_repo}" add .
git -C "${ci_base_repo}" commit -q -m 'PR commit A: parser without docs'
printf '# readme\n' >"${ci_base_repo}/README.md"
git -C "${ci_base_repo}" add .
git -C "${ci_base_repo}" commit -q -m 'PR commit B: readme touch'

if env -u ESHU_PARSER_RELATIONSHIP_KIT_BASE \
  ESHU_PARSER_RELATIONSHIP_KIT_REPO_ROOT="${ci_base_repo}" \
  GITHUB_BASE_REF=main \
  "${verifier}" >/tmp/eshu-parser-relationship-kit.out \
  2>/tmp/eshu-parser-relationship-kit.err; then
  printf 'expected the CI-shaped gate to FAIL: the branch adds an undocumented\n' >&2
  printf 'parser in commit A and ends on an innocuous commit. A pass means the\n' >&2
  printf 'fetch never created origin/main and the base fell back to HEAD~1.\n' >&2
  sed -n '1,160p' /tmp/eshu-parser-relationship-kit.out >&2
  exit 1
fi
# Exit status alone does not say WHICH base the gate used. These two do: only
# the verifier's own fetch could have created origin/main in this clone, and
# that message fires only when a parser source file is inside the diff window.
if ! git -C "${ci_base_repo}" rev-parse --verify origin/main >/dev/null 2>&1; then
  printf 'expected the verifier fetch to create origin/main (destination refspec)\n' >&2
  exit 1
fi
if ! rg -q 'parser source changed without language/support docs update' \
  /tmp/eshu-parser-relationship-kit.err; then
  printf 'the gate failed, but not for commit A -- the CI base was not used\n' >&2
  sed -n '1,160p' /tmp/eshu-parser-relationship-kit.err >&2
  exit 1
fi

printf 'verify-parser-relationship-kit tests passed\n'
