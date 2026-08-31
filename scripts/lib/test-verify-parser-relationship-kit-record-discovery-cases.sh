#!/usr/bin/env bash

# Sourced after the documented-selector fixture helpers are defined. These
# cases prove Markdown record discovery remains a superset of selector parsing.
# shellcheck disable=SC2154 # Parent defines fixture helpers.

documented_selector_raw_scan_case() {
  local name="$1" expected="$2" command="$3" fixture
  fixture="$(init_repo "$name")"
  printf '\n%s\n' "$command" \
    >>"${fixture}/docs/public/contributing-language-support.md"
  git -C "$fixture" add .
  git -C "$fixture" commit -q -m "$name"
  case "$expected" in
    fail) expect_fail "$fixture" ;;
    pass) expect_pass "$fixture" ;;
    *) printf 'invalid raw selector expectation: %s\n' "$expected" >&2; exit 1 ;;
  esac
}

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
documented_selector_scan_case regex-env-long-chdir-parent-stale fail \
  "env --chdir=go/internal/parser go test -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case regex-env-long-chdir-child-current pass \
  "env --chdir=go/internal/parser/rust go test -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case regex-time-parent-stale fail \
  "time go test ./internal/parser -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case regex-time-child-current pass \
  "time go test ./internal/parser/rust -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case regex-time-posix-parent-stale fail \
  "time -p go test ./internal/parser -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case regex-time-posix-child-current pass \
  "time -p go test ./internal/parser/rust -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case time-goflags-parent-stale fail \
  "time GOFLAGS='-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$' go test ./internal/parser -count=1"
documented_selector_scan_case time-goflags-child-current pass \
  "time GOFLAGS='-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$' go test ./internal/parser/rust -count=1"
documented_selector_scan_case regex-time-env-long-chdir-parent-stale fail \
  "time env --chdir=go/internal/parser go test -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case regex-time-env-long-chdir-child-current pass \
  "time env --chdir=go/internal/parser/rust go test -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case regex-env-long-chdir-time-parent-stale fail \
  "env --chdir=go/internal/parser time -p go test -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case regex-env-long-chdir-time-child-current pass \
  "env --chdir=go/internal/parser/rust time -p go test -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case regex-env-split-long-chdir-parent-stale fail \
  "env -S '--chdir=go/internal/parser go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'"
documented_selector_scan_case regex-env-split-long-chdir-child-current pass \
  "env -S '--chdir=go/internal/parser/rust go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'"
documented_selector_scan_case nested-env-chdir-parent-stale fail \
  "env -C go/internal env -C parser go test -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case nested-env-chdir-child-current pass \
  "env -C go/internal env -C parser/rust go test -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case nested-env-long-chdir-parent-stale fail \
  "env --chdir=go/internal env --chdir=parser go test -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case nested-env-long-chdir-child-current pass \
  "env --chdir=go/internal env --chdir=parser/rust go test -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case command-time-parent-stale fail \
  "command time -p go test ./internal/parser -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case command-time-child-current pass \
  "command time -p go test ./internal/parser/rust -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case command-env-chdir-parent-stale fail \
  "command env -C go/internal/parser go test . -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case command-env-chdir-child-current pass \
  "command env -C go/internal/parser/rust go test . -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case command-path-parent-stale fail \
  "command -p go test ./internal/parser -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case command-path-child-current pass \
  "command -p go test ./internal/parser/rust -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case env-last-chdir-parent-stale fail \
  "env -C go/internal/query -C go/internal/parser go test . -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case env-last-chdir-child-current pass \
  "env -C go/internal/query -C go/internal/parser/rust go test . -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case env-last-long-chdir-parent-stale fail \
  "env --chdir=go/internal/query --chdir=go/internal/parser go test . -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case env-last-long-chdir-child-current pass \
  "env --chdir=go/internal/query --chdir=go/internal/parser/rust go test . -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case env-last-mixed-chdir-parent-stale fail \
  "env --chdir=go/internal/query -C go/internal/parser go test . -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case env-last-mixed-chdir-child-current pass \
  "env -C go/internal/query --chdir=go/internal/parser/rust go test . -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case exec-clear-parent-stale fail \
  "exec -c go test ./internal/parser -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case exec-clear-child-current pass \
  "exec -c go test ./internal/parser/rust -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case exec-login-parent-stale fail \
  "exec -l go test ./internal/parser -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case exec-login-child-current pass \
  "exec -l go test ./internal/parser/rust -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case exec-combined-parent-stale fail \
  "exec -cl go test ./internal/parser -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case exec-combined-child-current pass \
  "exec -lc go test ./internal/parser/rust -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case exec-argv-name-parent-stale fail \
  "exec -a go-wrapper go test ./internal/parser -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case exec-argv-name-child-current pass \
  "exec -a go-wrapper go test ./internal/parser/rust -run 'TestDefaultEngineParsePathRust.*' -count=1"
documented_selector_scan_case time-shell-parent-stale fail \
  "time bash -lc \"go test ./internal/parser -run 'CapturesImplLifetimes' -count=1\""
documented_selector_scan_case time-shell-child-current pass \
  "time bash -lc \"go test ./internal/parser/rust -run 'CapturesImplLifetimes' -count=1\""
documented_selector_scan_case command-shell-parent-stale fail \
  "command -p bash -lc \"go test ./internal/parser -run 'TestDefaultEngineParsePathRust.*' -count=1\""
documented_selector_scan_case command-shell-child-current pass \
  "command -p bash -lc \"go test ./internal/parser/rust -run 'TestDefaultEngineParsePathRust.*' -count=1\""
documented_selector_scan_case exec-shell-parent-stale fail \
  "exec -c -l -a wrap -- bash -lc \"go test ./internal/parser -run 'TestDefaultEngineParsePathRust.*' -count=1\""
documented_selector_scan_case exec-shell-child-current pass \
  "exec -c -l -a wrap -- bash -lc \"go test ./internal/parser/rust -run 'TestDefaultEngineParsePathRust.*' -count=1\""
documented_selector_scan_case env-chdir-shell-parent-stale fail \
  "env -C go/internal/parser bash -lc \"go test . -run 'TestDefaultEngineParsePathRust.*' -count=1\""
documented_selector_scan_case env-chdir-shell-child-current pass \
  "env -C go/internal/parser/rust bash -lc \"go test . -run 'TestDefaultEngineParsePathRust.*' -count=1\""
documented_selector_scan_case time-env-chdir-shell-parent-stale fail \
  "time -p env --chdir=go/internal/parser exec -a wrap bash -lc \"go test . -run 'TestDefaultEngineParsePathRust.*' -count=1\""
documented_selector_scan_case time-env-chdir-shell-child-current pass \
  "time -p env --chdir=go/internal/parser/rust exec -a wrap bash -lc \"go test . -run 'TestDefaultEngineParsePathRust.*' -count=1\""
documented_selector_scan_case exec-clear-goflags-current pass \
  "GOFLAGS='-run=TestDefaultEngineParsePathRustCapturesImplLifetimes' exec -c go test ./internal/parser -count=1"
documented_selector_scan_case exec-clear-reset-goflags-parent-stale fail \
  "GOFLAGS='-run=TestDefaultEngineParsePathRustCapturesImplLifetimes' exec -c env GOFLAGS='-run=TestDefaultEngineParsePathRustCapturesImplLifetimes' bash -lc 'go test ./internal/parser -count=1'"
documented_selector_scan_case exec-clear-reset-goflags-child-current pass \
  "GOFLAGS='-run=TestDefaultEngineParsePathRustCapturesImplLifetimes' exec -c env GOFLAGS='-run=TestDefaultEngineParsePathRustCapturesImplLifetimes' bash -lc 'go test ./internal/parser/rust -count=1'"
documented_selector_raw_scan_case raw-time-shell-regex-parent-stale fail \
  "time bash -lc \"go test ./internal/parser -run 'CapturesImplLifetimes' -count=1\""
documented_selector_raw_scan_case raw-time-shell-regex-child-current pass \
  "time bash -lc \"go test ./internal/parser/rust -run 'CapturesImplLifetimes' -count=1\""
documented_selector_raw_scan_case raw-bash-shell-substring-parent-stale fail \
  "bash -lc \"cd go && go test ./internal/parser -run 'CapturesImplLifetimes' -count=1\""
documented_selector_raw_scan_case raw-bash-shell-substring-child-current pass \
  "bash -lc \"cd go && go test ./internal/parser/rust -run 'CapturesImplLifetimes' -count=1\""
documented_selector_raw_scan_case raw-path-shell-substring-parent-stale fail \
  "/bin/sh -c \"cd go && go test ./internal/parser -run 'CapturesImplLifetimes' -count=1\""
documented_selector_raw_scan_case raw-path-shell-substring-child-current pass \
  "/bin/sh -c \"cd go && go test ./internal/parser/rust -run 'CapturesImplLifetimes' -count=1\""
documented_selector_raw_scan_case raw-quoted-path-shell-parent-stale fail \
  "\"/bin/bash\" -lc \"cd go && go test ./internal/parser -run 'CapturesImplLifetimes' -count=1\""
documented_selector_raw_scan_case raw-quoted-path-shell-child-current pass \
  "\"/bin/bash\" -lc \"cd go && go test ./internal/parser/rust -run 'CapturesImplLifetimes' -count=1\""
documented_selector_raw_scan_case raw-quoted-bare-shell-parent-stale fail \
  "'bash' -lc \"cd go && go test ./internal/parser -run 'CapturesImplLifetimes' -count=1\""
documented_selector_raw_scan_case raw-quoted-bare-shell-child-current pass \
  "'bash' -lc \"cd go && go test ./internal/parser/rust -run 'CapturesImplLifetimes' -count=1\""
documented_selector_raw_scan_case raw-assignment-shell-substring-parent-stale fail \
  "GOTOOLCHAIN=go1.26.6 bash -lc \"cd go && go test ./internal/parser -run 'CapturesImplLifetimes' -count=1\""
documented_selector_raw_scan_case raw-assignment-shell-substring-child-current pass \
  "GOTOOLCHAIN=go1.26.6 bash -lc \"cd go && go test ./internal/parser/rust -run 'CapturesImplLifetimes' -count=1\""
documented_selector_raw_scan_case raw-prompt-shell-substring-parent-stale fail \
  "$ bash -lc \"cd go && go test ./internal/parser -run 'CapturesImplLifetimes' -count=1\""
documented_selector_raw_scan_case raw-prompt-shell-substring-child-current pass \
  "$ bash -lc \"cd go && go test ./internal/parser/rust -run 'CapturesImplLifetimes' -count=1\""
documented_selector_raw_scan_case raw-goflags-shell-parent-stale fail \
  "GOFLAGS='-run=CapturesImplLifetimes' time /bin/bash -lc \"cd go && go test ./internal/parser -count=1\""
documented_selector_raw_scan_case raw-goflags-shell-child-current pass \
  "GOFLAGS='-run=CapturesImplLifetimes' time /bin/bash -lc \"cd go && go test ./internal/parser/rust -count=1\""
documented_selector_raw_scan_case raw-prompt-goflags-env-shell-parent-stale fail \
  "$ GOFLAGS='-run=CapturesImplLifetimes' env -C go/internal/parser bash -lc 'go test . -count=1'"
documented_selector_raw_scan_case raw-prompt-goflags-env-shell-child-current pass \
  "$ GOFLAGS='-run=CapturesImplLifetimes' env -C go/internal/parser/rust bash -lc 'go test . -count=1'"
documented_selector_fenced_scan_case fenced-time-shell-substring-parent-stale fail \
  "time bash -lc \"go test ./internal/parser -run 'CapturesImplLifetimes' -count=1\""
documented_selector_fenced_scan_case fenced-time-shell-substring-child-current pass \
  "time bash -lc \"go test ./internal/parser/rust -run 'CapturesImplLifetimes' -count=1\""
