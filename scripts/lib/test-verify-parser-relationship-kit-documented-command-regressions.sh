#!/usr/bin/env bash

# Sourced after the documented-selector fixture helpers are defined. These
# cases cover command forms and Markdown locations that previously escaped the
# documented Rust selector check.
# shellcheck disable=SC2154 # Parent defines fixture helpers.

root_markdown_selector_case() {
  local name="$1" expected="$2" content="$3" fixture
  fixture="$(init_repo "$name")"
  printf '%s\n' "$content" >"${fixture}/README.md"
  git -C "$fixture" add .
  git -C "$fixture" commit -q -m "$name"
  case "$expected" in
    fail) expect_fail "$fixture" ;;
    pass) expect_pass "$fixture" ;;
    *) printf 'invalid root Markdown expectation: %s\n' "$expected" >&2; exit 1 ;;
  esac
}

root_markdown_selector_case root-markdown-parent-selector-stale fail \
  '`go test ./internal/parser -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1`'
root_markdown_selector_case root-markdown-non-command-current pass \
  'Run the Rust parser tests from the package that owns them.'

documented_selector_scan_case env-separated-long-chdir-parent-stale fail \
  'env --chdir go/internal/parser go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case env-separated-long-chdir-child-current pass \
  'env --chdir go/internal/parser/rust go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case env-separated-long-chdir-dynamic-stale fail \
  'env --chdir "$PARSER_DIR" go test . -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'

documented_selector_scan_case env-split-separated-long-chdir-parent-stale fail \
  "env -S '--chdir go/internal/parser go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'"
documented_selector_scan_case env-split-separated-long-chdir-child-current pass \
  "env -S '--chdir go/internal/parser/rust go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'"
documented_selector_scan_case env-split-separated-long-chdir-dynamic-stale fail \
  "env -S '--chdir \"\$PARSER_DIR\" go test . -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'"
