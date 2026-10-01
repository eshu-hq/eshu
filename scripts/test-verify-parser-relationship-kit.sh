#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-parser-relationship-kit.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

# shellcheck source=scripts/lib/test-verify-parser-relationship-kit-fixture-git.sh
. "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-fixture-git.sh"

# Self-tests for the fixture-git safety layer (#7229): they run before any
# fixture so a broken helper fails in seconds, not after minutes of setup.
# Failures use the file's convention (message to stderr, exit 1).
selftest_with_timeout_bounds_hang() {
  local rc start elapsed
  rc=0
  with_timeout 30 "true probe" true || rc=$?
  if [ "$rc" -ne 0 ]; then
    printf 'with_timeout failed a trivial command with rc=%s\n' "$rc" >&2
    exit 1
  fi
  start=$SECONDS
  with_timeout 3 "sleep probe" sleep 30 || rc=$?
  elapsed=$((SECONDS - start))
  if [ "$rc" -ne 124 ]; then
    printf 'with_timeout rc=%s, want 124 after killing the hang\n' "$rc" >&2
    exit 1
  fi
  if [ "$elapsed" -ge 10 ]; then
    printf 'with_timeout took %ss to kill the hang, want under 10s\n' "$elapsed" >&2
    exit 1
  fi
  rc=0
  with_timeout 2 "term-ignoring hang" bash -c 'trap "" TERM; sleep 30' || rc=$?
  if [ "$rc" -ne 124 ]; then
    printf 'with_timeout KILL-escalation rc=%s, want 124\n' "$rc" >&2
    exit 1
  fi
}

selftest_with_timeout_no_pipe_stall() {
  local repo probe_out start elapsed
  repo="${tmp_root}/pipe-stall-probe"
  mkdir -p "${repo}"
  fixture_git "probe init" -C "${repo}" init -q
  # Regression: the watchdog's background sleep must not inherit a
  # command substitution's capture pipe, or every $(init_repo) stalls
  # until the orphaned sleep exits.
  start=$SECONDS
  probe_out="$(fixture_git "rev-parse probe" -C "${repo}" rev-parse --show-toplevel)"
  elapsed=$((SECONDS - start))
  if [ ! -d "${probe_out}/.git" ]; then
    printf 'fixture_git in $() returned %s, want the repo root\n' "${probe_out}" >&2
    exit 1
  fi
  if [ "$elapsed" -ge 20 ]; then
    printf 'fixture_git in $() took %ss, want under 20s (watcher sleep held the pipe)\n' "$elapsed" >&2
    exit 1
  fi
}

selftest_fixture_git_isolation() {
  local poison_dir repo
  poison_dir="${tmp_root}/poison-config"
  mkdir -p "${poison_dir}/hooks"
  printf '#!/usr/bin/env bash\nexit 1\n' >"${poison_dir}/hooks/pre-commit"
  chmod +x "${poison_dir}/hooks/pre-commit"
  printf '[core]\n\thooksPath = %s/hooks\n' "${poison_dir}" >"${poison_dir}/poison.gitconfig"
  # Sensitivity: the poison is live — the same commit through raw git must
  # fail in the planted hook, proving this test is not vacuous.
  repo="${tmp_root}/poison-sensitivity"
  mkdir -p "${repo}"
  printf 'probe\n' >"${repo}/file.txt"
  GIT_CONFIG_GLOBAL="${poison_dir}/poison.gitconfig" git -C "${repo}" init -q
  GIT_CONFIG_GLOBAL="${poison_dir}/poison.gitconfig" git -C "${repo}" config user.email "test@example.invalid"
  GIT_CONFIG_GLOBAL="${poison_dir}/poison.gitconfig" git -C "${repo}" config user.name "Eshu Test"
  if GIT_CONFIG_GLOBAL="${poison_dir}/poison.gitconfig" git -C "${repo}" add . 2>/dev/null &&
    GIT_CONFIG_GLOBAL="${poison_dir}/poison.gitconfig" git -C "${repo}" commit -q -m probe 2>/dev/null; then
    printf 'poison sensitivity probe unexpectedly passed through the hook\n' >&2
    exit 1
  fi
  # The wrapper must bypass every operator-config vector, including the
  # GIT_CONFIG_COUNT pairs that /dev/null files cannot stop.
  repo="${tmp_root}/isolation-probe"
  mkdir -p "${repo}"
  printf 'probe\n' >"${repo}/file.txt"
  git -C "${repo}" init -q
  git -C "${repo}" config user.email "test@example.invalid"
  git -C "${repo}" config user.name "Eshu Test"
  GIT_CONFIG_GLOBAL="${poison_dir}/poison.gitconfig" \
    GIT_CONFIG_COUNT=1 \
    GIT_CONFIG_KEY_0=core.hooksPath \
    GIT_CONFIG_VALUE_0="${poison_dir}/hooks" \
    fixture_add_commit "${repo}" probe || {
    printf 'fixture_add_commit did not isolate the poisoned operator config\n' >&2
    exit 1
  }
  # GIT_TEMPLATE_DIR acts at init time (commit never consults templates),
  # so this arm inits through the wrapper: a template carrying a failing
  # pre-commit hook must plant nothing in the wrapper-built repo.
  mkdir -p "${poison_dir}/template/hooks"
  printf '#!/usr/bin/env bash\nexit 1\n' >"${poison_dir}/template/hooks/pre-commit"
  chmod +x "${poison_dir}/template/hooks/pre-commit"
  repo="${tmp_root}/template-sensitivity"
  mkdir -p "${repo}"
  printf 'probe\n' >"${repo}/file.txt"
  GIT_TEMPLATE_DIR="${poison_dir}/template" git -C "${repo}" init -q
  if GIT_TEMPLATE_DIR="${poison_dir}/template" git -C "${repo}" add . 2>/dev/null &&
    GIT_TEMPLATE_DIR="${poison_dir}/template" git -C "${repo}" -c user.email=t@e.invalid -c user.name=t commit -q -m probe 2>/dev/null; then
    printf 'template sensitivity probe unexpectedly passed\n' >&2
    exit 1
  fi
  repo="${tmp_root}/template-isolation-probe"
  mkdir -p "${repo}"
  printf 'probe\n' >"${repo}/file.txt"
  GIT_TEMPLATE_DIR="${poison_dir}/template" fixture_git "template probe init" -C "${repo}" init -q
  GIT_TEMPLATE_DIR="${poison_dir}/template" fixture_git "template probe config" -C "${repo}" config user.email "test@example.invalid"
  GIT_TEMPLATE_DIR="${poison_dir}/template" fixture_git "template probe config" -C "${repo}" config user.name "Eshu Test"
  GIT_TEMPLATE_DIR="${poison_dir}/template" fixture_add_commit "${repo}" probe || {
    printf 'fixture_git did not isolate GIT_TEMPLATE_DIR\n' >&2
    exit 1
  }
}

selftest_with_timeout_bounds_hang
selftest_with_timeout_no_pipe_stall
selftest_fixture_git_isolation

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
  fixture_git "init ${name}" -C "${dir}" init -q
  fixture_git "config ${name}" -C "${dir}" config user.email "test@example.invalid"
  fixture_git "config ${name}" -C "${dir}" config user.name "Eshu Test"
  write_required_docs "${dir}"
  fixture_add_commit "${dir}" initial
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
fixture_add_commit "${plain_repo}" 'docs only'
expect_pass "${plain_repo}"

parser_missing_docs_repo="$(init_repo parser-missing-docs)"
printf 'package parser\nfunc parseNewLanguage() {}\n' >"${parser_missing_docs_repo}/go/internal/parser/new_language.go"
printf 'package parser\nfunc TestNewLanguage(t interface{}) {}\n' >"${parser_missing_docs_repo}/go/internal/parser/new_language_test.go"
fixture_add_commit "${parser_missing_docs_repo}" 'parser without docs'
expect_fail "${parser_missing_docs_repo}"

parser_missing_tests_repo="$(init_repo parser-missing-tests)"
printf 'package parser\nfunc parseNewLanguage() {}\n' >"${parser_missing_tests_repo}/go/internal/parser/new_language.go"
printf '\nDocumented new parser behavior.\n' >>"${parser_missing_tests_repo}/docs/public/languages/python.md"
fixture_add_commit "${parser_missing_tests_repo}" 'parser without tests'
expect_fail "${parser_missing_tests_repo}"

parser_complete_repo="$(init_repo parser-complete)"
printf 'package parser\nfunc parseNewLanguage() {}\n' >"${parser_complete_repo}/go/internal/parser/new_language.go"
printf 'package parser\nfunc TestNewLanguage(t interface{}) {}\n' >"${parser_complete_repo}/go/internal/parser/new_language_test.go"
printf '\nDocumented new parser behavior.\n' >>"${parser_complete_repo}/docs/public/languages/python.md"
fixture_add_commit "${parser_complete_repo}" 'parser with docs and tests'
expect_pass "${parser_complete_repo}"

relationship_missing_docs_repo="$(init_repo relationship-missing-docs)"
printf 'package relationships\nfunc discoverNewEvidence() {}\n' >"${relationship_missing_docs_repo}/go/internal/relationships/new_evidence.go"
printf 'package relationships\nfunc TestDiscoverNewEvidence(t interface{}) {}\n' >"${relationship_missing_docs_repo}/go/internal/relationships/new_evidence_test.go"
fixture_add_commit "${relationship_missing_docs_repo}" 'relationship without docs'
expect_fail "${relationship_missing_docs_repo}"

relationship_complete_repo="$(init_repo relationship-complete)"
printf 'package relationships\nfunc discoverNewEvidence() {}\n' >"${relationship_complete_repo}/go/internal/relationships/new_evidence.go"
printf 'package relationships\nfunc TestDiscoverNewEvidence(t interface{}) {}\n' >"${relationship_complete_repo}/go/internal/relationships/new_evidence_test.go"
printf '\nDocumented new relationship evidence family.\n' >>"${relationship_complete_repo}/docs/public/reference/relationship-mapping.md"
fixture_add_commit "${relationship_complete_repo}" 'relationship with docs and tests'
expect_pass "${relationship_complete_repo}"

relationship_comment_only_repo="$(init_repo relationship-comment-only)"
printf 'package relationships\n\n// Evidence points at the original reducer path.\nfunc Evidence() {}\n' \
  >"${relationship_comment_only_repo}/go/internal/relationships/gcp_evidence.go"
fixture_add_commit "${relationship_comment_only_repo}" 'relationship source baseline'
printf 'package relationships\n\n// Evidence points at the current reducer path.\nfunc Evidence() {}\n' \
  >"${relationship_comment_only_repo}/go/internal/relationships/gcp_evidence.go"
fixture_add_commit "${relationship_comment_only_repo}" 'relationship comment-only correction'
expect_pass "${relationship_comment_only_repo}"

relationship_code_only_repo="$(init_repo relationship-code-only)"
printf 'package relationships\n\nfunc Evidence() {}\n' \
  >"${relationship_code_only_repo}/go/internal/relationships/gcp_evidence.go"
fixture_add_commit "${relationship_code_only_repo}" 'relationship source baseline'
printf 'package relationships\n\nfunc Evidence() { println("changed") }\n' \
  >"${relationship_code_only_repo}/go/internal/relationships/gcp_evidence.go"
fixture_add_commit "${relationship_code_only_repo}" 'relationship code change without proof'
expect_fail "${relationship_code_only_repo}"

# shellcheck source=scripts/lib/parser_documented_test_commands.sh
. "${repo_root}/scripts/lib/parser_documented_test_commands.sh"
PARSER_SELECTOR_MATCHER_OUTPUT="${tmp_root}/parser-selector-matcher"
# shellcheck source=scripts/lib/test-verify-parser-relationship-kit-cargo-selector-cases.sh
. "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-cargo-selector-cases.sh"
# shellcheck source=scripts/lib/test-verify-parser-relationship-kit-documented-command-regressions.sh
. "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-documented-command-regressions.sh"

query_missing_dsl_repo="$(init_repo query-missing-dsl)"
mkdir -p "${query_missing_dsl_repo}/go/internal/query/language"
printf 'package language\nfunc languageQueryEntityType() {}\n' >"${query_missing_dsl_repo}/go/internal/query/language/handler.go"
printf '\nDocumented new query behavior.\n' >>"${query_missing_dsl_repo}/docs/public/languages/python.md"
fixture_add_commit "${query_missing_dsl_repo}" 'language query without dsl docs'
expect_fail "${query_missing_dsl_repo}"

# A *_language_inventory.go change (the repositories/by-language +
# language-inventory route handlers) is NOT a Language Query DSL source, so it
# must NOT require a language-query-dsl.md update. This is the paired positive
# for query_missing_dsl_repo above: together they prove is_language_query_source
# still fires for a real DSL source (language_queries.go) but is correctly
# narrowed to skip the by-language inventory handlers.
language_inventory_repo="$(init_repo language-inventory)"
printf 'package query\nfunc listRepositoriesByLanguage() {}\n' >"${language_inventory_repo}/go/internal/query/repository_language_inventory.go"
fixture_add_commit "${language_inventory_repo}" 'by-language inventory handler without dsl docs'
expect_pass "${language_inventory_repo}"

# content_reader_language.go is the same class as the inventory handlers:
# ListRepoFilesByLanguage is a pushed-down files-by-language read for the
# repository-tree endpoint, called only from repository/tree.go, and
# execute_language_query never reaches it. It must NOT require a
# language-query-dsl.md update either. Paired with query_missing_dsl_repo
# above, which still expect_fails, so the carve-out is proven narrow rather
# than proven absent.
content_reader_language_repo="$(init_repo content-reader-language)"
printf 'package query\nfunc listRepoFilesByLanguage() {}\n' >"${content_reader_language_repo}/go/internal/query/content_reader_language.go"
fixture_add_commit "${content_reader_language_repo}" 'repository-tree files-by-language read without dsl docs'
expect_pass "${content_reader_language_repo}"

# shellcheck source=scripts/lib/test-verify-parser-relationship-kit-dsl-comment-only-cases.sh
. "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-dsl-comment-only-cases.sh"
# shellcheck source=scripts/lib/test-verify-parser-relationship-kit-import-rename-cases.sh
. "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-import-rename-cases.sh"
unsupported_claim_repo="$(init_repo unsupported-claim)"
cat >"${unsupported_claim_repo}/docs/public/languages/support-maturity.md" <<'MD'
# Parser Support Matrix

| Parser | Parser Class | Grammar Routing | Normalization | Framework Or Root Evidence | Modeled Evidence | Query Surfacing | Real-Repo Validation | End-to-End Indexing |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| JSON Config | `DefaultEngine (json)` | - | - | unsupported | JSON metadata only | supported | - | supported |
MD
fixture_add_commit "${unsupported_claim_repo}" 'unsupported query claim'
expect_fail "${unsupported_claim_repo}"

missing_language_proof_repo="$(init_repo missing-language-proof)"
cat >"${missing_language_proof_repo}/docs/public/languages/python.md" <<'MD'
# Python Parser

| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Functions | `functions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/python_language_test.go::TestPythonFunctions` | - | - |
MD
fixture_add_commit "${missing_language_proof_repo}" 'missing language proof'
expect_fail "${missing_language_proof_repo}"

parser_backing_missing_repo="$(init_repo parser-backing-missing)"
rm -f "${parser_backing_missing_repo}/specs/parser-backing-ledger.v1.yaml"
# Delivered from a sibling fixture file, not a heredoc: Homebrew bash >= 5.1
# writes an entire heredoc body to a pipe before forking the reader, and
# macOS's 512-byte pipe buffer deadlocks on this ~724B body (#5074). The
# body is fully static (was a quoted heredoc, no shell expansion), so the
# file is byte-identical to the original heredoc body.
cat "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-support-maturity-missing-ledger.md" >"${parser_backing_missing_repo}/docs/public/languages/support-maturity.md"
fixture_add_commit "${parser_backing_missing_repo}" 'missing parser backing ledger'
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
fixture_add_commit "${parser_backing_incomplete_repo}" 'incomplete parser backing ledger'
expect_fail "${parser_backing_incomplete_repo}"

parser_backing_bad_path_repo="$(init_repo parser-backing-bad-path)"
# Delivered from a sibling fixture file, not a heredoc: Homebrew bash >= 5.1
# writes an entire heredoc body to a pipe before forking the reader, and
# macOS's 512-byte pipe buffer deadlocks on this ~1239B body (#5074). The
# body is fully static (was a quoted heredoc, no shell expansion), so the
# file is byte-identical to the original heredoc body.
cat "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-parser-backing-ledger-bad-path.yaml" >"${parser_backing_bad_path_repo}/specs/parser-backing-ledger.v1.yaml"
fixture_add_commit "${parser_backing_bad_path_repo}" 'stale parser backing ledger path'
expect_fail "${parser_backing_bad_path_repo}"

language_ledger_missing_repo="$(init_repo language-ledger-missing)"
rm -f "${language_ledger_missing_repo}/specs/language-feature-parity-ledger.v1.yaml"
fixture_add_commit "${language_ledger_missing_repo}" 'missing language feature ledger'
expect_fail "${language_ledger_missing_repo}"

language_ledger_missing_feature_repo="$(init_repo language-ledger-missing-feature)"
printf '| Classes | `classes` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/python_language_test.go::TestPythonClasses` | Compose-backed fixture verification | - |\n' \
  >>"${language_ledger_missing_feature_repo}/docs/public/languages/python.md"
fixture_add_commit "${language_ledger_missing_feature_repo}" 'language docs claim missing ledger feature'
expect_fail "${language_ledger_missing_feature_repo}"

language_ledger_bad_path_repo="$(init_repo language-ledger-bad-path)"
perl -0pi -e 's#go/internal/parser/python_language.go#go/internal/parser/does_not_exist.go#' \
  "${language_ledger_bad_path_repo}/specs/language-feature-parity-ledger.v1.yaml"
fixture_add_commit "${language_ledger_bad_path_repo}" 'language ledger stale path'
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
fixture_add_commit "${parser_backing_complete_repo}" 'complete parser backing ledger'
expect_pass "${parser_backing_complete_repo}"

# shellcheck source=scripts/lib/test-verify-parser-relationship-kit-base-resolution-regressions.sh
. "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-base-resolution-regressions.sh"

printf 'verify-parser-relationship-kit tests passed\n'
