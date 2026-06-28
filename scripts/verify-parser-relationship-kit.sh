#!/usr/bin/env bash
set -euo pipefail

repo_root="${ESHU_PARSER_RELATIONSHIP_KIT_REPO_ROOT:-}"
script_dir="$(cd "$(dirname "$0")" && pwd)"
if [ -z "$repo_root" ]; then
  repo_root="$(git -C "$script_dir" rev-parse --show-toplevel 2>/dev/null \
    || (cd "$script_dir/.." && pwd))"
fi

base="${ESHU_PARSER_RELATIONSHIP_KIT_BASE:-}"
if [ -z "$base" ] && [ -n "${GITHUB_BASE_REF:-}" ]; then
  git -C "$repo_root" fetch --no-tags --depth=1 origin "$GITHUB_BASE_REF" >/dev/null 2>&1 || true
  if git -C "$repo_root" rev-parse --verify "origin/$GITHUB_BASE_REF" >/dev/null 2>&1; then
    base="origin/$GITHUB_BASE_REF"
  fi
fi
if [ -z "$base" ]; then
  if git -C "$repo_root" rev-parse --verify HEAD~1 >/dev/null 2>&1; then
    base="HEAD~1"
  else
    printf 'verify-parser-relationship-kit: no base commit available, skipping diff checks\n'
    base=""
  fi
fi

changed_files=()
tmp_file="$(mktemp)"
trap 'rm -f "$tmp_file"' EXIT
if [ -n "$base" ]; then
  if git -C "$repo_root" diff --name-only "$base"...HEAD >"$tmp_file" 2>/dev/null; then
    :
  else
    git -C "$repo_root" diff --name-only "$base" HEAD >"$tmp_file"
  fi
  while IFS= read -r file; do
    [ -n "$file" ] && changed_files+=("$file")
  done <"$tmp_file"
fi

trim_cell() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  value="${value//\`/}"
  printf '%s' "$value"
}

lower_cell() {
  trim_cell "$1" | tr '[:upper:]' '[:lower:]'
}

is_blank_cell() {
  local value
  value="$(lower_cell "$1")"
  [ -z "$value" ] || [ "$value" = "-" ]
}

# shellcheck source=scripts/lib/parser_relationship_language_ledger.sh
. "$script_dir/lib/parser_relationship_language_ledger.sh"

has_changed_file() {
  local matcher="$1"
  local file
  for file in "${changed_files[@]}"; do
    if "$matcher" "$file"; then
      return 0
    fi
  done
  return 1
}

is_parser_source() {
  local path="$1"
  case "$path" in
    go/internal/parser/*.go|go/internal/parser/*/*.go) ;;
    *) return 1 ;;
  esac
  case "$path" in
    *_test.go|*_bench_test.go|*/testdata/*|*/vendor/*|*/doc.go) return 1 ;;
    *) return 0 ;;
  esac
}

is_parser_test() {
  local path="$1"
  case "$path" in
    go/internal/parser/*_test.go|go/internal/parser/*/*_test.go) return 0 ;;
    *) return 1 ;;
  esac
}

is_parser_doc() {
  local path="$1"
  case "$path" in
    docs/public/contributing-language-support.md|\
docs/public/languages/*.md|\
docs/public/reference/language-query-dsl.md|\
docs/public/reference/dead-code-language-maturity.md) return 0 ;;
    *) return 1 ;;
  esac
}

is_language_query_source() {
  local path="$1"
  case "$path" in
    go/internal/query/language*.go|\
go/internal/query/*language*.go|\
go/internal/mcp/*language*.go|\
go/internal/mcp/language*.go) ;;
    *) return 1 ;;
  esac
  case "$path" in
    *_test.go|*_bench_test.go) return 1 ;;
    *) return 0 ;;
  esac
}

is_language_query_doc() {
  local path="$1"
  case "$path" in
    docs/public/reference/language-query-dsl.md) return 0 ;;
    *) return 1 ;;
  esac
}

is_dead_code_maturity_source() {
  local path="$1"
  [ "$path" = "go/internal/query/code_dead_code_language_maturity.go" ]
}

is_dead_code_maturity_doc() {
  local path="$1"
  case "$path" in
    docs/public/reference/dead-code-language-maturity.md|docs/public/languages/*.md) return 0 ;;
    *) return 1 ;;
  esac
}

is_relationship_source() {
  local path="$1"
  case "$path" in
    go/internal/relationships/*.go|go/internal/relationships/*/*.go) ;;
    *) return 1 ;;
  esac
  case "$path" in
    *_test.go|*_bench_test.go|*/testdata/*|*/vendor/*|*/doc.go) return 1 ;;
    *) return 0 ;;
  esac
}

is_relationship_test() {
  local path="$1"
  case "$path" in
    go/internal/relationships/*_test.go|go/internal/relationships/*/*_test.go) return 0 ;;
    *) return 1 ;;
  esac
}

is_relationship_doc() {
  local path="$1"
  case "$path" in
    docs/public/reference/relationship-mapping*.md|\
docs/public/guides/relationship-graphs.md|\
docs/public/extend/community-extension-authoring.md) return 0 ;;
    *) return 1 ;;
  esac
}

require_doc_text() {
  local file="$1"
  local text="$2"
  local label="$3"
  if [ ! -f "$repo_root/$file" ]; then
    printf 'verify-parser-relationship-kit: missing required doc %s\n' "$file" >&2
    return 1
  fi
  if ! rg -q -F "$text" "$repo_root/$file"; then
    printf 'verify-parser-relationship-kit: %s missing %s\n' "$file" "$label" >&2
    return 1
  fi
  return 0
}

require_doc_regex() {
  local file="$1"
  local pattern="$2"
  local label="$3"
  if [ ! -f "$repo_root/$file" ]; then
    printf 'verify-parser-relationship-kit: missing required doc %s\n' "$file" >&2
    return 1
  fi
  if ! rg -q "$pattern" "$repo_root/$file"; then
    printf 'verify-parser-relationship-kit: %s missing %s\n' "$file" "$label" >&2
    return 1
  fi
  return 0
}

validate_required_docs() {
  local issues=0
  require_doc_text \
    docs/public/contributing-language-support.md \
    "Parse-only behavior is not supported query behavior" \
    "parse-only support boundary" || issues=1
  require_doc_text \
    docs/public/contributing-language-support.md \
    "Support-Maturity Promotion Rules" \
    "support-maturity promotion rules" || issues=1
  require_doc_text \
    docs/public/contributing-language-support.md \
    "Query DSL And Language Page Updates" \
    "query DSL update expectations" || issues=1
  require_doc_text \
    docs/public/contributing-language-support.md \
    "Dynamic And Framework Guardrails" \
    "dynamic import/plugin/reflection guardrails" || issues=1
  require_doc_text \
    docs/public/reference/language-query-dsl.md \
    "Adding Or Promoting Language Query Support" \
    "language query promotion rules" || issues=1
  require_doc_text \
    docs/public/reference/relationship-mapping.md \
    "Relationship Extractor Contribution Kit" \
    "relationship contribution kit" || issues=1
  require_doc_text \
    docs/public/reference/relationship-mapping.md \
    "positive, negative, and ambiguous" \
    "positive/negative/ambiguous fixture rule" || issues=1
  return "$issues"
}

validate_parser_backing_ledger() {
  local issues=0
  local spec="specs/parser-backing-ledger.v1.yaml"
  local maturity="docs/public/languages/support-maturity.md"
  local parser

  require_doc_text "$maturity" "Parser Backing Ledger" "parser backing ledger section" || issues=1
  require_doc_text "$maturity" "structured-parser-backed-exception" "structured-parser exception status" || issues=1

  for parser in cloudformation dockerfile hcl yaml; do
    require_doc_regex "$spec" "^[[:space:]]*-[[:space:]]*parser:[[:space:]]*${parser}[[:space:]]*$" \
      "parser backing row for ${parser}" || issues=1
    require_doc_regex "$maturity" "^\\|[[:space:]]*[^|]*${parser}[^|]*\\|[[:space:]]*\`structured-parser-backed-exception\`.*specs/parser-backing-ledger\\.v1\\.yaml" \
      "support-maturity backing row for ${parser}" || issues=1
  done

  for parser in cloudformation dockerfile hcl yaml; do
    local block
    block="$(awk -v parser="$parser" '
      $0 ~ "^[[:space:]]*-[[:space:]]*parser:[[:space:]]*" parser "[[:space:]]*$" { in_block=1; print; next }
      in_block && $0 ~ "^[[:space:]]*-[[:space:]]*parser:" { in_block=0 }
      in_block { print }
    ' "$repo_root/$spec" 2>/dev/null || true)"

    if ! printf '%s\n' "$block" | rg -q "^[[:space:]]*implementation_class:[[:space:]]*structured-parser-backed-exception[[:space:]]*$"; then
      printf 'verify-parser-relationship-kit: %s row must be structured-parser-backed-exception\n' "$parser" >&2
      issues=1
    fi
    if ! printf '%s\n' "$block" | rg -q "^[[:space:]]*no_provider_required:[[:space:]]*true[[:space:]]*$"; then
      printf 'verify-parser-relationship-kit: %s row must declare no_provider_required: true\n' "$parser" >&2
      issues=1
    fi
    validate_parser_backing_path_list "$parser" "$block" source_files || issues=1
    validate_parser_backing_path_list "$parser" "$block" test_files || issues=1
    validate_parser_backing_path_list "$parser" "$block" docs || issues=1
  done

  return "$issues"
}

validate_parser_backing_path_list() {
  local parser="$1"
  local block="$2"
  local key="$3"
  local found=0
  local issues=0
  local path

  while IFS= read -r path; do
    [ -z "$path" ] && continue
    found=1
    if [ ! -e "$repo_root/$path" ]; then
      printf 'verify-parser-relationship-kit: %s %s path does not exist: %s\n' "$parser" "$key" "$path" >&2
      issues=1
    fi
  done < <(
    printf '%s\n' "$block" | awk -v key="$key" '
      $0 ~ "^[[:space:]]*" key ":[[:space:]]*$" { in_list=1; next }
      in_list && $0 ~ "^[[:space:]]*[A-Za-z_][A-Za-z0-9_]*:" { in_list=0 }
      in_list && $0 ~ "^[[:space:]]*-[[:space:]]*" {
        item=$0
        sub(/^[[:space:]]*-[[:space:]]*/, "", item)
        print item
      }
    '
  )

  if [ "$found" -eq 0 ]; then
    printf 'verify-parser-relationship-kit: %s row missing non-empty %s\n' "$parser" "$key" >&2
    issues=1
  fi
  return "$issues"
}

validate_support_maturity_matrix() {
  local file="$repo_root/docs/public/languages/support-maturity.md"
  local issues=0
  local line parser framework query e2e
  [ -f "$file" ] || return 0
  while IFS= read -r line; do
    case "$line" in
      "| Parser "*|"| ---"*|"") continue ;;
      "|"*) ;;
      *) continue ;;
    esac
    IFS='|' read -r _ parser _ _ _ framework _ query _ e2e _ <<<"$line"
    parser="$(trim_cell "$parser")"
    framework="$(lower_cell "$framework")"
    query="$(trim_cell "$query")"
    e2e="$(trim_cell "$e2e")"
    if [ "$framework" = "unsupported" ]; then
      if ! is_blank_cell "$query" || ! is_blank_cell "$e2e"; then
        printf '%s: unsupported framework/root row for %s cannot claim query surfacing or end-to-end indexing\n' \
          "$file" "$parser" >&2
        issues=1
      fi
    fi
  done <"$file"
  return "$issues"
}

validate_full_capability_table() {
  local file="$1"
  local issues=0
  local line capability status graph unit integration
  while IFS= read -r line; do
    case "$line" in
      "| Capability "*|"| ---"*|"") continue ;;
      "|"*) ;;
      *) continue ;;
    esac
    IFS='|' read -r _ capability _ status _ _ graph unit integration _ <<<"$line"
    capability="$(trim_cell "$capability")"
    status="$(lower_cell "$status")"
    case "$status" in
      supported|partial|derived)
        if is_blank_cell "$unit" || is_blank_cell "$integration"; then
          printf '%s: %s is %s but lacks unit or integration proof\n' \
            "$file" "$capability" "$status" >&2
          issues=1
        fi
        if [ "$status" = "supported" ] && is_blank_cell "$graph"; then
          printf '%s: %s is supported but lacks a graph/query surface\n' \
            "$file" "$capability" >&2
          issues=1
        fi
        ;;
      unsupported)
        if ! is_blank_cell "$graph" || ! is_blank_cell "$unit" || ! is_blank_cell "$integration"; then
          printf '%s: %s is unsupported but carries graph or proof claims\n' \
            "$file" "$capability" >&2
          issues=1
        fi
        ;;
    esac
  done <"$file"
  return "$issues"
}

validate_compact_capability_table() {
  local file="$1"
  local issues=0
  local line capability status evidence
  while IFS= read -r line; do
    case "$line" in
      "| Capability "*|"| ---"*|"") continue ;;
      "|"*) ;;
      *) continue ;;
    esac
    IFS='|' read -r _ capability _ status evidence _ <<<"$line"
    capability="$(trim_cell "$capability")"
    status="$(lower_cell "$status")"
    case "$status" in
      supported|partial|derived)
        if is_blank_cell "$evidence"; then
          printf '%s: %s is %s but lacks evidence\n' "$file" "$capability" "$status" >&2
          issues=1
        fi
        ;;
      unsupported)
        if ! is_blank_cell "$evidence"; then
          printf '%s: %s is unsupported but carries evidence claims\n' "$file" "$capability" >&2
          issues=1
        fi
        ;;
    esac
  done <"$file"
  return "$issues"
}

validate_language_pages() {
  local issues=0
  local file
  while IFS= read -r file; do
    case "$file" in
      */feature-matrix.md|*/support-maturity.md) continue ;;
    esac
    if rg -q -F '| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |' "$file"; then
      validate_full_capability_table "$file" || issues=1
    elif rg -q -F '| Capability | ID | Status | Evidence | Current truth |' "$file"; then
      validate_compact_capability_table "$file" || issues=1
    fi
  done < <(rg --files "$repo_root/docs/public/languages" -g '*.md' 2>/dev/null)
  return "$issues"
}

validate_diff_contracts() {
  local issues=0
  if has_changed_file is_parser_source; then
    if ! has_changed_file is_parser_test; then
      printf 'verify-parser-relationship-kit: parser source changed without parser *_test.go coverage\n' >&2
      issues=1
    fi
    if ! has_changed_file is_parser_doc; then
      printf 'verify-parser-relationship-kit: parser source changed without language/support docs update\n' >&2
      issues=1
    fi
  fi

  if has_changed_file is_language_query_source; then
    if ! has_changed_file is_language_query_doc; then
      printf 'verify-parser-relationship-kit: language query source changed without Language Query DSL or language page update\n' >&2
      issues=1
    fi
  fi

  if has_changed_file is_dead_code_maturity_source; then
    if ! has_changed_file is_dead_code_maturity_doc; then
      printf 'verify-parser-relationship-kit: dead-code maturity map changed without maturity/language docs update\n' >&2
      issues=1
    fi
  fi

  if has_changed_file is_relationship_source; then
    if ! has_changed_file is_relationship_test; then
      printf 'verify-parser-relationship-kit: relationship source changed without relationship *_test.go coverage\n' >&2
      issues=1
    fi
    if ! has_changed_file is_relationship_doc; then
      printf 'verify-parser-relationship-kit: relationship source changed without relationship mapping docs update\n' >&2
      issues=1
    fi
  fi
  return "$issues"
}

issues=0
validate_required_docs || issues=1
validate_parser_backing_ledger || issues=1
validate_language_feature_ledger || issues=1
validate_support_maturity_matrix || issues=1
validate_language_pages || issues=1
validate_diff_contracts || issues=1

if [ "$issues" -ne 0 ]; then
  {
    printf '\nParser and relationship extension changes must keep contribution docs,\n'
    printf 'language/query support claims, maturity matrices, tests, and\n'
    printf 'relationship proof paths in lockstep. Parse-only behavior is not\n'
    printf 'supported query behavior.\n'
  } >&2
  exit 1
fi

printf 'verify-parser-relationship-kit: parser, relationship, maturity, and docs gate passed\n'
