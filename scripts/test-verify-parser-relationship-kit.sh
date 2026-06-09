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
    "${dir}/go/internal/relationships" \
    "${dir}/go/internal/query"

  cat >"${dir}/docs/public/contributing-language-support.md" <<'MD'
# Contributing Parser Support

## Parser And Language Contribution Checklist

Parse-only behavior is not supported query behavior. Parser claims need parser
tests and a language page update.

## Query DSL And Language Page Updates

Update the language page and Language Query DSL when a query language or entity
type changes.

## Support-Maturity Promotion Rules

Unsupported, partial, and supported claims need matching evidence.

## Dynamic And Framework Guardrails

Dynamic imports, plugin loading, reflection, generated code, and framework roots
stay unclaimed without focused proof.
MD

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

  cat >"${dir}/docs/public/languages/python.md" <<'MD'
# Python Parser

| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Functions | `functions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/python_language_test.go::TestPythonFunctions` | Compose-backed fixture verification | - |
MD

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

printf 'verify-parser-relationship-kit tests passed\n'
