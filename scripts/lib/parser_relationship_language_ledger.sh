#!/usr/bin/env bash

language_feature_ledger_path() {
  printf '%s/specs/language-feature-parity-ledger.v1.yaml' "$repo_root"
}

language_feature_ledger_blocks() {
  local spec
  spec="$(language_feature_ledger_path)"
  [ -f "$spec" ] || return 0
  awk '
    /^[[:space:]]*-[[:space:]]*language:/ {
      if (block != "") {
        print block "\n---BLOCK---"
      }
      block=$0 ORS
      next
    }
    block != "" { block=block $0 ORS }
    END {
      if (block != "") {
        print block
      }
    }
  ' "$spec"
}

language_feature_block_for_doc() {
  local doc="$1"
  language_feature_ledger_blocks | awk -v doc="$doc" '
    $0 == "---BLOCK---" {
      if (found) {
        print block
        exit
      }
      block=""
      found=0
      next
    }
    {
      block=block $0 ORS
      line=$0
      sub(/^[[:space:]]*docs_claim:[[:space:]]*/, "", line)
      if (line == doc) {
        found=1
      }
    }
    END {
      if (found) {
        print block
      }
    }
  '
}

language_feature_block_field() {
  local block="$1"
  local key="$2"
  printf '%s\n' "$block" | awk -v key="$key" '
    $0 ~ "^[[:space:]]*(-[[:space:]]*)?" key ":" {
      line=$0
      sub("^[[:space:]]*(-[[:space:]]*)?" key ":[[:space:]]*", "", line)
      print line
      exit
    }
  '
}

language_feature_block_contains() {
  local block="$1"
  local key="$2"
  local feature="$3"
  local values
  values="$(language_feature_block_field "$block" "$key")"
  values="${values#[}"
  values="${values%]}"
  values="${values//,/ }"
  values="${values//\`/}"
  case " $values " in
    *" $feature "*) return 0 ;;
    *) return 1 ;;
  esac
}

language_feature_state_key() {
  case "$1" in
    supported) printf 'supported_features' ;;
    partial) printf 'partial_features' ;;
    derived) printf 'derived_features' ;;
    *) return 1 ;;
  esac
}

validate_language_feature_path_list() {
  local label="$1"
  local block="$2"
  local key="$3"
  local found=0
  local issues=0
  local path

  while IFS= read -r path; do
    [ -z "$path" ] && continue
    found=1
    if [ ! -e "$repo_root/$path" ]; then
      printf 'verify-parser-relationship-kit: language feature %s %s path does not exist: %s\n' \
        "$label" "$key" "$path" >&2
      issues=1
    fi
  done < <(
    printf '%s\n' "$block" | awk -v key="$key" '
      $0 ~ "^[[:space:]]*" key ":[[:space:]]*\\[" {
        line=$0
        sub("^[[:space:]]*" key ":[[:space:]]*\\[", "", line)
        sub("\\][[:space:]]*$", "", line)
        n=split(line, items, ",")
        for (i=1; i<=n; i++) {
          item=items[i]
          gsub(/^[[:space:]]+|[[:space:]]+$/, "", item)
          if (item != "") print item
        }
      }
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
    printf 'verify-parser-relationship-kit: language feature %s missing non-empty %s\n' \
      "$label" "$key" >&2
    issues=1
  fi
  return "$issues"
}

language_feature_list_has_items() {
  local block="$1"
  local key="$2"
  printf '%s\n' "$block" | awk -v key="$key" '
    $0 ~ "^[[:space:]]*" key ":[[:space:]]*\\[[^]]+\\][[:space:]]*$" { found=1 }
    $0 ~ "^[[:space:]]*" key ":[[:space:]]*$" { in_list=1; next }
    in_list && $0 ~ "^[[:space:]]*[A-Za-z_][A-Za-z0-9_]*:" { in_list=0 }
    in_list && $0 ~ "^[[:space:]]*-[[:space:]]*" { found=1 }
    END { exit found ? 0 : 1 }
  '
}

validate_language_feature_block() {
  local block="$1"
  local issues=0
  local label backing no_provider
  label="$(language_feature_block_field "$block" language)"
  label="${label:-unknown}"
  backing="$(language_feature_block_field "$block" parser_backing)"
  no_provider="$(language_feature_block_field "$block" no_provider_required)"

  case "$backing" in
    tree-sitter-backed|structured-parser-backed-exception|official-format-ast-exception|content-metadata-exception) ;;
    *)
      printf 'verify-parser-relationship-kit: language feature %s has invalid parser_backing %s\n' \
        "$label" "$backing" >&2
      issues=1
      ;;
  esac
  if [ "$no_provider" != "true" ]; then
    printf 'verify-parser-relationship-kit: language feature %s must declare no_provider_required: true\n' \
      "$label" >&2
    issues=1
  fi

  validate_language_feature_path_list "$label" "$block" source_files || issues=1
  validate_language_feature_path_list "$label" "$block" test_files || issues=1
  validate_language_feature_path_list "$label" "$block" docs || issues=1
  if ! language_feature_list_has_items "$block" read_surfaces; then
    printf 'verify-parser-relationship-kit: language feature %s missing read_surfaces\n' "$label" >&2
    issues=1
  fi
  return "$issues"
}

validate_language_feature_claim() {
  local file="$1"
  local feature="$2"
  local status="$3"
  local block key
  block="$(language_feature_block_for_doc "${file#$repo_root/}")"
  if [ -z "$block" ]; then
    printf 'verify-parser-relationship-kit: %s has supported language claims but no language feature ledger row\n' "$file" >&2
    return 1
  fi
  key="$(language_feature_state_key "$status" || true)"
  if [ -z "$key" ] || ! language_feature_block_contains "$block" "$key" "$feature"; then
    printf 'verify-parser-relationship-kit: %s claims %s as %s without matching language feature ledger row\n' \
      "$file" "$feature" "$status" >&2
    return 1
  fi
}

validate_language_feature_table() {
  local file="$1"
  local issues=0
  local line feature status
  while IFS= read -r line; do
    case "$line" in
      "| Capability "*|"| ---"*|"") continue ;;
      "|"*) ;;
      *) continue ;;
    esac
    IFS='|' read -r _ _ feature status _ <<<"$line"
    feature="$(trim_cell "$feature")"
    status="$(lower_cell "$status")"
    [ -z "$feature" ] && continue
    case "$status" in
      supported|partial|derived)
        validate_language_feature_claim "$file" "$feature" "$status" || issues=1
        ;;
    esac
  done <"$file"
  return "$issues"
}

validate_language_feature_ledger() {
  local issues=0
  local spec="specs/language-feature-parity-ledger.v1.yaml"
  local maturity="docs/public/languages/support-maturity.md"
  local block file line

  require_doc_text "$maturity" "Language Feature Parity Ledger" "language feature parity ledger section" || issues=1
  require_doc_text "$maturity" "$spec" "language feature parity ledger link" || issues=1
  if [ ! -f "$repo_root/$spec" ]; then
    printf 'verify-parser-relationship-kit: missing %s\n' "$spec" >&2
    return 1
  fi

  block=""
  while IFS= read -r line; do
    if [ "$line" = "---BLOCK---" ]; then
      [ -n "$block" ] && validate_language_feature_block "$block" || issues=1
      block=""
      continue
    fi
    block="${block}${line}"$'\n'
  done < <(language_feature_ledger_blocks)
  if [ -n "$block" ]; then
    validate_language_feature_block "$block" || issues=1
  fi

  while IFS= read -r file; do
    case "$file" in
      */feature-matrix.md|*/support-maturity.md) continue ;;
    esac
    if rg -q -F '| Capability | ID | Status |' "$file"; then
      validate_language_feature_table "$file" || issues=1
    fi
  done < <(rg --files "$repo_root/docs/public/languages" -g '*.md' 2>/dev/null)
  return "$issues"
}
