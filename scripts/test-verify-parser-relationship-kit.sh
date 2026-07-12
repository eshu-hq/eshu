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

query_missing_dsl_repo="$(init_repo query-missing-dsl)"
printf 'package query\nfunc languageQueryEntityType() {}\n' >"${query_missing_dsl_repo}/go/internal/query/language_queries.go"
printf '\nDocumented new query behavior.\n' >>"${query_missing_dsl_repo}/docs/public/languages/python.md"
git -C "${query_missing_dsl_repo}" add .
git -C "${query_missing_dsl_repo}" commit -q -m 'language query without dsl docs'
expect_fail "${query_missing_dsl_repo}"

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

printf 'verify-parser-relationship-kit tests passed\n'
