#!/usr/bin/env bash

# Sourced after the documented-selector fixture helpers are defined. These
# cases prove Markdown record discovery remains a superset of selector parsing.
# shellcheck disable=SC2154 # Parent defines fixture helpers.

documented_selector_fenced_scan_case regex-multiline-cd-absolute-go-parent-stale fail \
  "cd go/internal/parser
/usr/local/go/bin/go test -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_fenced_scan_case regex-multiline-cd-absolute-go-child-current pass \
  "cd go/internal/parser/rust
/usr/local/go/bin/go test -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case regex-prompt-env-absolute-go-parent-stale fail \
  "$ env -C go/internal/parser /usr/local/go/bin/go test -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case regex-prompt-env-absolute-go-child-current pass \
  "$ env -C go/internal/parser/rust /usr/local/go/bin/go test -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_fenced_scan_case regex-quoted-go-chdir-parent-stale fail \
  "\"/usr/local/go/bin/go\" -C go/internal/parser test -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_fenced_scan_case regex-quoted-go-chdir-child-current pass \
  "\"/usr/local/go/bin/go\" -C go/internal/parser/rust test -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case regex-env-command-absolute-go-parent-stale fail \
  "env -C go/internal/parser command /usr/local/go/bin/go test -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case regex-env-command-absolute-go-child-current pass \
  "env -C go/internal/parser/rust command /usr/local/go/bin/go test -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case quoted-bare-go-parent-stale fail \
  "cd go/internal/parser && \"go\" test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1"
documented_selector_scan_case quoted-bare-go-child-current pass \
  "cd go/internal/parser/rust && \"go\" test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1"
documented_selector_scan_case regex-single-quoted-bare-go-parent-stale fail \
  "env -C go/internal/parser 'go' test -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case regex-single-quoted-bare-go-child-current pass \
  "env -C go/internal/parser/rust 'go' test -run 'TestDefaultEngineParsePathRust.*' -count=1"
