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

  cat >>"${dir}/docs/public/languages/support-maturity.md" <<'MD'

## Parser Backing Ledger

See `specs/parser-backing-ledger.v1.yaml`.

| Parser Key | Implementation Class | Decision | Evidence |
| --- | --- | --- | --- |
| cloudformation | `structured-parser-backed-exception` | Decoded YAML/JSON plus bounded CloudFormation evaluation is the canonical parser. | `specs/parser-backing-ledger.v1.yaml` |
| dockerfile | `structured-parser-backed-exception` | Dockerfile instruction scanning is the canonical build-manifest parser. | `specs/parser-backing-ledger.v1.yaml` |
| hcl | `structured-parser-backed-exception` | HashiCorp HCL v2 is the canonical Terraform/Terragrunt parser. | `specs/parser-backing-ledger.v1.yaml` |
| yaml | `structured-parser-backed-exception` | YAML v3 document decoding is the canonical declarative-data parser. | `specs/parser-backing-ledger.v1.yaml` |
MD

  cat >>"${dir}/docs/public/languages/support-maturity.md" <<'MD'

## Language Feature Parity Ledger

See `specs/language-feature-parity-ledger.v1.yaml`.
MD

  cat >"${dir}/specs/parser-backing-ledger.v1.yaml" <<'YAML'
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
  - parser: dockerfile
    implementation_class: structured-parser-backed-exception
    no_provider_required: true
    source_files:
      - go/internal/parser/dockerfile/metadata.go
    test_files:
      - go/internal/parser/dockerfile/metadata_test.go
    docs:
      - docs/public/languages/support-maturity.md
  - parser: hcl
    implementation_class: structured-parser-backed-exception
    no_provider_required: true
    source_files:
      - go/internal/parser/hcl/parser.go
    test_files:
      - go/internal/parser/hcl/parser_test.go
    docs:
      - docs/public/languages/terraform.md
  - parser: yaml
    implementation_class: structured-parser-backed-exception
    no_provider_required: true
    source_files:
      - go/internal/parser/yaml/language.go
    test_files:
      - go/internal/parser/yaml/language_test.go
    docs:
      - docs/public/languages/kubernetes.md
YAML

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

  cat >"${dir}/docs/public/languages/python.md" <<'MD'
# Python Parser

| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Functions | `functions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/python_language_test.go::TestPythonFunctions` | Compose-backed fixture verification | - |
| Django/DRF routes | `django-drf-routes` | partial | - | - | - | `go/internal/parser/python_language_test.go::TestPythonFunctions` | Explicit unsupported-route wording | Not audited as route_entries or HANDLES_ROUTE truth. |
MD

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
cat >"${parser_backing_missing_repo}/docs/public/languages/support-maturity.md" <<'MD'
# Parser Support Matrix

| Parser | Parser Class | Grammar Routing | Normalization | Framework Or Root Evidence | Modeled Evidence | Query Surfacing | Real-Repo Validation | End-to-End Indexing |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| CloudFormation | `DefaultEngine (yaml)` | - | - | unsupported | template/resource evidence only | - | - | - |
| Dockerfile | `DefaultEngine (dockerfile)` | - | - | unsupported | build-manifest evidence only | - | - | - |
| HCL | `DefaultEngine (hcl)` | supported | supported | non-code evidence | Terraform and Terragrunt evidence | supported | supported | supported |
| YAML | `DefaultEngine (yaml)` | - | - | unsupported | declarative-data evidence only | - | - | - |
MD
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
cat >"${parser_backing_incomplete_repo}/docs/public/languages/support-maturity.md" <<'MD'
# Parser Support Matrix

| Parser | Parser Class | Grammar Routing | Normalization | Framework Or Root Evidence | Modeled Evidence | Query Surfacing | Real-Repo Validation | End-to-End Indexing |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| CloudFormation | `DefaultEngine (yaml)` | - | - | unsupported | template/resource evidence only | - | - | - |
| Dockerfile | `DefaultEngine (dockerfile)` | - | - | unsupported | build-manifest evidence only | - | - | - |
| HCL | `DefaultEngine (hcl)` | supported | supported | non-code evidence | Terraform and Terragrunt evidence | supported | supported | supported |
| YAML | `DefaultEngine (yaml)` | - | - | unsupported | declarative-data evidence only | - | - | - |

## Parser Backing Ledger

See `specs/parser-backing-ledger.v1.yaml`.

| Parser | Implementation Class | Decision | Evidence |
| --- | --- | --- | --- |
| cloudformation | `structured-parser-backed-exception` | Decoded YAML/JSON plus bounded CloudFormation evaluation is the canonical parser. | `specs/parser-backing-ledger.v1.yaml` |
MD
git -C "${parser_backing_incomplete_repo}" add .
git -C "${parser_backing_incomplete_repo}" commit -q -m 'incomplete parser backing ledger'
expect_fail "${parser_backing_incomplete_repo}"

parser_backing_bad_path_repo="$(init_repo parser-backing-bad-path)"
cat >"${parser_backing_bad_path_repo}/specs/parser-backing-ledger.v1.yaml" <<'YAML'
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
  - parser: dockerfile
    implementation_class: structured-parser-backed-exception
    no_provider_required: true
    source_files:
      - go/internal/parser/dockerfile/does_not_exist.go
    test_files:
      - go/internal/parser/dockerfile/metadata_test.go
    docs:
      - docs/public/languages/support-maturity.md
  - parser: hcl
    implementation_class: structured-parser-backed-exception
    no_provider_required: true
    source_files:
      - go/internal/parser/hcl/parser.go
    test_files:
      - go/internal/parser/hcl/parser_test.go
    docs:
      - docs/public/languages/terraform.md
  - parser: yaml
    implementation_class: structured-parser-backed-exception
    no_provider_required: true
    source_files:
      - go/internal/parser/yaml/language.go
    test_files:
      - go/internal/parser/yaml/language_test.go
    docs:
      - docs/public/languages/kubernetes.md
YAML
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
cat >"${parser_backing_complete_repo}/specs/parser-backing-ledger.v1.yaml" <<'YAML'
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
  - parser: dockerfile
    implementation_class: structured-parser-backed-exception
    no_provider_required: true
    source_files:
      - go/internal/parser/dockerfile/metadata.go
    test_files:
      - go/internal/parser/dockerfile/metadata_test.go
    docs:
      - docs/public/languages/support-maturity.md
  - parser: hcl
    implementation_class: structured-parser-backed-exception
    no_provider_required: true
    source_files:
      - go/internal/parser/hcl/parser.go
    test_files:
      - go/internal/parser/hcl/parser_test.go
    docs:
      - docs/public/languages/terraform.md
  - parser: yaml
    implementation_class: structured-parser-backed-exception
    no_provider_required: true
    source_files:
      - go/internal/parser/yaml/language.go
    test_files:
      - go/internal/parser/yaml/language_test.go
    docs:
      - docs/public/languages/kubernetes.md
YAML
cat >"${parser_backing_complete_repo}/docs/public/languages/support-maturity.md" <<'MD'
# Parser Support Matrix

| Parser | Parser Class | Grammar Routing | Normalization | Framework Or Root Evidence | Modeled Evidence | Query Surfacing | Real-Repo Validation | End-to-End Indexing |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| CloudFormation | `DefaultEngine (yaml)` | - | - | unsupported | template/resource evidence only | - | - | - |
| Dockerfile | `DefaultEngine (dockerfile)` | - | - | unsupported | build-manifest evidence only | - | - | - |
| HCL | `DefaultEngine (hcl)` | supported | supported | non-code evidence | Terraform and Terragrunt evidence | supported | supported | supported |
| YAML | `DefaultEngine (yaml)` | - | - | unsupported | declarative-data evidence only | - | - | - |

## Parser Backing Ledger

See `specs/parser-backing-ledger.v1.yaml`.

| Parser | Implementation Class | Decision | Evidence |
| --- | --- | --- | --- |
| cloudformation | `structured-parser-backed-exception` | Decoded YAML/JSON plus bounded CloudFormation evaluation is the canonical parser. | `specs/parser-backing-ledger.v1.yaml` |
| dockerfile | `structured-parser-backed-exception` | Dockerfile instruction scanning is the canonical build-manifest parser. | `specs/parser-backing-ledger.v1.yaml` |
| hcl | `structured-parser-backed-exception` | HashiCorp HCL v2 is the canonical Terraform/Terragrunt parser. | `specs/parser-backing-ledger.v1.yaml` |
| yaml | `structured-parser-backed-exception` | YAML v3 document decoding is the canonical declarative-data parser. | `specs/parser-backing-ledger.v1.yaml` |

## Language Feature Parity Ledger

See `specs/language-feature-parity-ledger.v1.yaml`.
MD
git -C "${parser_backing_complete_repo}" add .
git -C "${parser_backing_complete_repo}" commit -q -m 'complete parser backing ledger'
expect_pass "${parser_backing_complete_repo}"

printf 'verify-parser-relationship-kit tests passed\n'
