#!/usr/bin/env bash

# Sourced by the Cargo selector fixture after classifier helpers are defined.
# These paired cases keep Markdown discovery and record decoding connected to
# each command-classification family exercised directly above.
# shellcheck disable=SC2154 # Parent defines fixture helpers.

documented_selector_scan_case() {
  local name="$1" expected="$2" command="$3" fixture
  fixture="$(init_repo "$name")"
  printf '\n`%s`\n' "$command" \
    >>"${fixture}/docs/public/contributing-language-support.md"
  git -C "$fixture" add .
  git -C "$fixture" commit -q -m "$name"
  case "$expected" in
    fail) expect_fail "$fixture" ;;
    pass) expect_pass "$fixture" ;;
    *) printf 'invalid selector scan expectation: %s\n' "$expected" >&2; exit 1 ;;
  esac
}

documented_selector_fenced_scan_case() {
  local name="$1" expected="$2" command="$3" fixture
  fixture="$(init_repo "$name")"
  printf '\n```bash\n%s\n```\n' "$command" \
    >>"${fixture}/docs/public/contributing-language-support.md"
  git -C "$fixture" add .
  git -C "$fixture" commit -q -m "$name"
  case "$expected" in
    fail) expect_fail "$fixture" ;;
    pass) expect_pass "$fixture" ;;
    *) printf 'invalid fenced selector scan expectation: %s\n' "$expected" >&2; exit 1 ;;
  esac
}

documented_selector_delimited_scan_case() {
  local name="$1" expected="$2" delimiter="$3" command="$4" fixture
  fixture="$(init_repo "$name")"
  printf '\n%s%s%s\n' "$delimiter" "$command" "$delimiter" \
    >>"${fixture}/docs/public/contributing-language-support.md"
  git -C "$fixture" add .
  git -C "$fixture" commit -q -m "$name"
  case "$expected" in
    fail) expect_fail "$fixture" ;;
    pass) expect_pass "$fixture" ;;
    *) printf 'invalid delimited selector scan expectation: %s\n' "$expected" >&2; exit 1 ;;
  esac
}

documented_selector_custom_fence_scan_case() {
  local name="$1" expected="$2" fence="$3" command="$4" closing="${5:-$3}" fixture
  fixture="$(init_repo "$name")"
  printf '\n%sbash\n%s\n%s\n' "$fence" "$command" "$closing" \
    >>"${fixture}/docs/public/contributing-language-support.md"
  git -C "$fixture" add .
  git -C "$fixture" commit -q -m "$name"
  case "$expected" in
    fail) expect_fail "$fixture" ;;
    pass) expect_pass "$fixture" ;;
    *) printf 'invalid custom-fence selector expectation: %s\n' "$expected" >&2; exit 1 ;;
  esac
}

documented_selector_scan_case separator-stale-parent fail \
  'go test ./internal/parser -run Cargo -count=1; echo ./internal/parser/rust'
documented_selector_scan_case separator-current-mixed pass \
  'go test ./internal/parser ./internal/parser/rust -run Cargo -count=1; echo done'
documented_selector_scan_case normalized-path-stale-parent fail \
  'go test ./internal/parser/../parser/ -run Cargo -count=1'
documented_selector_scan_case normalized-path-current-mixed pass \
  'go test ./internal/parser/. ./internal/parser/rust -run Cargo -count=1'
documented_selector_scan_case shell-cd-parent-dot-stale fail \
  'cd go/internal/parser && go test . -run '\''TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' -count=1'
documented_selector_scan_case shell-cd-child-dot-current pass \
  'cd go/internal/parser/rust && go test . -run '\''TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' -count=1'
documented_selector_scan_case shell-cd-parent-relative-child-current pass \
  'cd go/internal/parser && go test ./rust -run '\''TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' -count=1'
documented_selector_scan_case shell-cd-child-env-prefix-current pass \
  'cd go/internal/parser/rust && GOTOOLCHAIN=go1.26.6 go test . -run '\''TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' -count=1'
documented_selector_scan_case shell-cd-child-continued-env-prefix-current pass \
  $'cd go/internal/parser/rust && \\\nGOTOOLCHAIN=go1.26.6 go test . -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case shell-cd-child-multiple-env-prefixes-current pass \
  'cd go/internal/parser/rust && GOTOOLCHAIN=go1.26.6 GOCACHE=/tmp/eshu-cache go test . -run '\''TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' -count=1'
documented_selector_scan_case goflags-relocated-parent-stale fail \
  'GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$" go test ./internal/parser -count=1'
documented_selector_scan_case goflags-unquoted-parent-stale fail \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser -count=1'
documented_selector_scan_case goflags-multiple-flags-parent-stale fail \
  'GOFLAGS="-count=1 --run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$" go test ./internal/parser'
documented_selector_scan_case goflags-env-parent-stale fail \
  'env GOFLAGS='\''-test.run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' go test ./internal/parser -count=1'
documented_selector_scan_case goflags-assignment-before-env-parent-stale fail \
  'GOFLAGS='\''-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' env GOTOOLCHAIN=go1.26.6 go test ./internal/parser -count=1'
documented_selector_scan_case goflags-export-newline-parent-stale fail \
  $'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"\ngo test ./internal/parser -count=1'
documented_selector_scan_case goflags-export-semicolon-parent-stale fail \
  'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; go test ./internal/parser -count=1'
documented_selector_scan_case goflags-export-and-parent-stale fail \
  'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$" && go test ./internal/parser -count=1'
documented_selector_scan_case goflags-export-newline-child-current pass \
  $'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"\ngo test ./internal/parser/rust -count=1'
documented_selector_scan_case goflags-export-newline-unrelated-current pass \
  $'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"\ngo test ./internal/query -count=1'
documented_selector_scan_case goflags-export-unset-current pass \
  $'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"\nunset GOFLAGS\ngo test ./internal/parser -count=1'
documented_selector_scan_case goflags-export-cli-override-current pass \
  $'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"\ngo test ./internal/parser -run TestDefaultEngineParsePathRuby -count=1'
documented_selector_scan_case goflags-export-command-assignment-override-current pass \
  $'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"\nGOFLAGS=-run=^TestDefaultEngineParsePathRuby$ go test ./internal/parser -count=1'
documented_selector_scan_case goflags-export-cli-relocated-override-stale fail \
  $'export GOFLAGS="-run=^TestDefaultEngineParsePathRuby$"\ngo test ./internal/parser -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case goflags-export-dynamic-parent-stale fail \
  $'export GOFLAGS="$PARSER_TEST_FLAGS"\ngo test ./internal/parser -count=1'
documented_selector_scan_case goflags-export-or-current pass \
  'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$" || go test ./internal/parser -count=1'
documented_selector_scan_case goflags-export-pipeline-current pass \
  'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$" | go test ./internal/parser -count=1'
documented_selector_scan_case goflags-export-background-current pass \
  'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$" & go test ./internal/parser -count=1'
documented_selector_scan_case goflags-export-n-dialect-stale fail \
  $'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"\nexport -n GOFLAGS\ngo test ./internal/parser -count=1'
documented_selector_scan_case goflags-assign-then-export-newline-parent-stale fail \
  $'GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"\nexport GOFLAGS\ngo test ./internal/parser -count=1'
documented_selector_scan_case goflags-assign-then-export-semicolon-parent-stale fail \
  'GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; export GOFLAGS; go test ./internal/parser -count=1'
documented_selector_scan_case goflags-assign-then-export-first-of-many-parent-stale fail \
  'GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; export GOFLAGS OTHER; go test ./internal/parser -count=1'
documented_selector_scan_case goflags-assign-then-export-later-of-many-parent-stale fail \
  $'GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"\nexport OTHER GOFLAGS\ngo test ./internal/parser -count=1'
documented_selector_scan_case goflags-first-assignment-then-export-parent-stale fail \
  'GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$" OTHER=value; export GOFLAGS; go test ./internal/parser -count=1'
documented_selector_scan_case goflags-later-assignment-then-export-parent-stale fail \
  'OTHER=value GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; export GOFLAGS; go test ./internal/parser -count=1'
documented_selector_scan_case goflags-escaped-space-assignment-then-export-parent-stale fail \
  'GOFLAGS=-count=1\ -run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$; export GOFLAGS; go test ./internal/parser'
documented_selector_scan_case goflags-ansi-c-assignment-then-export-parent-stale fail \
  'GOFLAGS=$'"'"'-count=1 -run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'"'"'; export GOFLAGS; go test ./internal/parser'
documented_selector_scan_case goflags-assignment-comment-then-export-parent-stale fail \
  $'GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$" # Rust selector\nexport GOFLAGS\ngo test ./internal/parser -count=1'
documented_selector_scan_case goflags-assignment-blank-line-then-export-parent-stale fail \
  $'GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"\n\nexport GOFLAGS\ngo test ./internal/parser -count=1'
documented_selector_scan_case goflags-export-after-command-separator-parent-stale fail \
  'true; export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; go test ./internal/parser -count=1'
documented_selector_scan_case goflags-export-after-or-parent-stale fail \
  'false || export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; go test ./internal/parser -count=1'
documented_selector_scan_case goflags-export-skipped-after-true-or-current pass \
  'true || export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; go test ./internal/parser -count=1'
documented_selector_scan_case goflags-export-skipped-after-false-and-current pass \
  'false && export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; go test ./internal/parser -count=1'
documented_selector_scan_case goflags-unset-after-false-or-current pass \
  'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; false || unset GOFLAGS; go test ./internal/parser -count=1'
documented_selector_scan_case goflags-unset-after-true-and-current pass \
  'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; true && unset GOFLAGS; go test ./internal/parser -count=1'
documented_selector_scan_case goflags-wrapper-after-prefix-parent-stale fail \
  'GOFLAGS='"'"'-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'"'"' sh -c '"'"'printf ignored; go test ./internal/parser -count=1'"'"''
documented_selector_scan_case goflags-env-wrapper-after-prefix-parent-stale fail \
  'env GOFLAGS='"'"'-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'"'"' sh -c '"'"'printf ignored; go test ./internal/parser -count=1'"'"''
documented_selector_scan_case goflags-after-cd-and-command-parent-stale fail \
  'cd go/internal/parser && printf ignored && export GOFLAGS='"'"'-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'"'"' && go test . -count=1'
documented_selector_scan_case goflags-brace-export-then-parent-stale fail \
  '{ export GOFLAGS='"'"'-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'"'"'; }; go test ./internal/parser -count=1'
documented_selector_scan_case goflags-nested-brace-group-parent-stale fail \
  '{ { export GOFLAGS='"'"'-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'"'"'; }; go test ./internal/parser -count=1; }'
documented_selector_scan_case goflags-export-before-assignment-parent-stale fail \
  $'export GOFLAGS\nGOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"\ngo test ./internal/parser -count=1'
documented_selector_scan_case goflags-intervening-assignment-parent-stale fail \
  'GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; OTHER=value; export GOFLAGS; go test ./internal/parser -count=1'
documented_selector_scan_case goflags-export-double-dash-parent-stale fail \
  'GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; export -- GOFLAGS; go test ./internal/parser -count=1'
documented_selector_scan_case goflags-subshell-parent-stale fail \
  '(export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; go test ./internal/parser -count=1)'
documented_selector_scan_case goflags-export-before-subshell-parent-stale fail \
  'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; (go test ./internal/parser -count=1)'
documented_selector_scan_case goflags-continued-go-test-parent-stale fail \
  $'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"\ngo \\\n test ./internal/parser -count=1'
documented_selector_scan_case goflags-command-wrapper-parent-stale fail \
  'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; command go test ./internal/parser -count=1'
documented_selector_scan_case goflags-exec-wrapper-parent-stale fail \
  'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; exec go test ./internal/parser -count=1'
documented_selector_scan_case goflags-shell-wrapper-parent-stale fail \
  'sh -c '\''export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; go test ./internal/parser -count=1'\'''
documented_selector_scan_case goflags-assignment-shell-wrapper-parent-stale fail \
  'GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$" sh -c '\''go test ./internal/parser -count=1'\'''
documented_selector_scan_case goflags-env-shell-wrapper-parent-stale fail \
  'env GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$" sh -c '\''go test ./internal/parser -count=1'\'''
documented_selector_scan_case goflags-absolute-go-parent-stale fail \
  'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; /usr/local/go/bin/go test ./internal/parser -count=1'
documented_selector_scan_case goflags-append-export-parent-stale fail \
  'export GOFLAGS+="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; go test ./internal/parser -count=1'
documented_selector_scan_case goflags-brace-group-parent-stale fail \
  '{ export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; go test ./internal/parser -count=1; }'
documented_selector_scan_case goflags-export-before-brace-group-parent-stale fail \
  'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; { go test ./internal/parser -count=1; }'
documented_selector_scan_case goflags-command-p-wrapper-parent-stale fail \
  'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; command -p go test ./internal/parser -count=1'
documented_selector_scan_case goflags-subshell-state-does-not-escape-current pass \
  '(export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"); go test ./internal/parser -count=1'
documented_selector_scan_case goflags-quoted-inert-command-current pass \
  'printf '\''%s\n'\'' '\''GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$; go test ./internal/parser'\'''
documented_selector_scan_case goflags-comment-only-command-current pass \
  '# GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$; go test ./internal/parser'
documented_selector_fenced_scan_case goflags-fenced-export-parent-stale fail \
  $'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"\ngo test ./internal/parser -count=1'
documented_selector_fenced_scan_case goflags-fenced-escaped-assignment-parent-stale fail \
  'GOFLAGS=-count=1\ -run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$; export GOFLAGS; go test ./internal/parser'
documented_selector_fenced_scan_case goflags-fenced-quoted-inert-current pass \
  'printf '\''%s\n'\'' '\''GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$; go test ./internal/parser'\'''
documented_selector_fenced_scan_case run-fenced-quoted-inert-current pass \
  'printf '\''%s\n'\'' '\''env -C go/internal/parser go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'\'''
documented_selector_scan_case run-inline-quoted-inert-current pass \
  'printf '\''%s\n'\'' '\''env -C go/internal/parser go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'\'''
documented_selector_fenced_scan_case goflags-fenced-heredoc-inert-current pass \
  $'cat <<'"'"'EOF'"'"'\nGOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"; export GOFLAGS; go test ./internal/parser\nEOF'
documented_selector_fenced_scan_case goflags-fenced-wrapper-continuation-parent-stale fail \
  "GOFLAGS='-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$' sh -c 'printf ignored; go \\"$'\n'" test ./internal/parser -count=1'"
documented_selector_delimited_scan_case goflags-double-backtick-parent-stale fail '``' \
  'GOFLAGS=-count=1\ -run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$; export GOFLAGS; go test ./internal/parser'
documented_selector_custom_fence_scan_case goflags-four-backtick-parent-stale fail '````' \
  'GOFLAGS=-count=1\ -run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$; export GOFLAGS; go test ./internal/parser'
documented_selector_custom_fence_scan_case goflags-tilde-fence-parent-stale fail '~~~' \
  'GOFLAGS=-count=1\ -run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$; export GOFLAGS; go test ./internal/parser'
documented_selector_custom_fence_scan_case goflags-longer-closing-fence-parent-stale fail '```' \
  'GOFLAGS=-count=1\ -run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$; export GOFLAGS; go test ./internal/parser' '````'
documented_selector_scan_case goflags-standalone-assignment-not-exported-current pass \
  $'GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"\ngo test ./internal/parser -count=1'
documented_selector_scan_case goflags-assign-export-unset-current pass \
  $'GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"\nexport GOFLAGS\nunset GOFLAGS\ngo test ./internal/parser -count=1'
documented_selector_scan_case goflags-export-then-standalone-reassignment-current pass \
  $'export GOFLAGS="-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$"\nGOFLAGS=-run=^TestDefaultEngineParsePathRuby$\ngo test ./internal/parser -count=1'
documented_selector_scan_case goflags-env-unset-option-parent-stale fail \
  'env -u ESHU_REVIEW_UNUSED GOFLAGS='\''-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' GOTOOLCHAIN=go1.26.6 go test ./internal/parser -count=1'
documented_selector_scan_case goflags-env-path-option-parent-stale fail \
  'env -P /opt/homebrew/bin GOFLAGS='\''-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' GOTOOLCHAIN=go1.26.6 go test ./internal/parser -count=1'
documented_selector_scan_case goflags-env-attached-path-option-parent-stale fail \
  'env -P/opt/homebrew/bin GOFLAGS='\''-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' go test ./internal/parser -count=1'
documented_selector_scan_case goflags-env-path-option-child-current pass \
  'env -P /opt/homebrew/bin GOFLAGS='\''-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' go test ./internal/parser/rust -count=1'
documented_selector_scan_case goflags-env-path-option-unrelated-current pass \
  'env -P /opt/homebrew/bin GOFLAGS='\''-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' go test ./internal/query -count=1'
documented_selector_scan_case goflags-env-verbose-option-parent-stale fail \
  'env -v GOFLAGS='\''-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' go test ./internal/parser -count=1'
documented_selector_scan_case goflags-env-repeated-verbose-option-parent-stale fail \
  'env -vv GOFLAGS='\''-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' go test ./internal/parser -count=1'
documented_selector_scan_case goflags-env-verbose-option-child-current pass \
  'env -v GOFLAGS='\''-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' go test ./internal/parser/rust -count=1'
documented_selector_scan_case goflags-env-clustered-ignore-verbose-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ env -iv GOTOOLCHAIN=local go test ./internal/parser -count=1'
documented_selector_scan_case goflags-env-clustered-verbose-ignore-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ env -vi GOTOOLCHAIN=local go test ./internal/parser -count=1'
documented_selector_scan_case goflags-env-chdir-option-parent-stale fail \
  'env -C go GOFLAGS='\''-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' go test ./internal/parser -count=1'
documented_selector_scan_case env-chdir-implicit-parent-stale fail \
  'env -C go/internal/parser go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case env-chdir-implicit-child-current pass \
  'env -C go/internal/parser/rust go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case assignment-env-chdir-implicit-parent-stale fail \
  'GOTOOLCHAIN=local env -C go/internal/parser go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case assignment-env-chdir-implicit-child-current pass \
  'GOTOOLCHAIN=local env -C go/internal/parser/rust go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case env-chdir-terminator-implicit-parent-stale fail \
  'env -C go/internal/parser -- go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case env-chdir-terminator-implicit-child-current pass \
  'env -C go/internal/parser/rust -- go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case env-chdir-absolute-go-implicit-parent-stale fail \
  'env -C go/internal/parser /usr/local/go/bin/go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case env-chdir-absolute-go-implicit-child-current pass \
  'env -C go/internal/parser/rust /usr/local/go/bin/go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case env-chdir-escaped-go-path-implicit-parent-stale fail \
  'env -C go/internal/parser /Applications/Go\ Tools/bin/go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case env-chdir-escaped-go-path-implicit-child-current pass \
  'env -C go/internal/parser/rust /Applications/Go\ Tools/bin/go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case env-chdir-quoted-go-path-implicit-parent-stale fail \
  'env -C go/internal/parser "/Applications/Go Tools/bin/go" test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case env-chdir-quoted-go-path-implicit-child-current pass \
  'env -C go/internal/parser/rust "/Applications/Go Tools/bin/go" test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case env-chdir-command-implicit-parent-stale fail \
  'env -C go/internal/parser command go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case env-chdir-command-implicit-child-current pass \
  'env -C go/internal/parser/rust command go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case env-chdir-exec-implicit-parent-stale fail \
  'env -C go/internal/parser exec go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case env-chdir-exec-implicit-child-current pass \
  'env -C go/internal/parser/rust exec go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case env-chdir-command-absolute-go-parent-stale fail \
  'env -C go/internal/parser command /usr/local/go/bin/go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case env-chdir-command-absolute-go-child-current pass \
  'env -C go/internal/parser/rust command /usr/local/go/bin/go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_fenced_scan_case multiline-cd-absolute-go-parent-stale fail \
  $'cd go/internal/parser\n/usr/local/go/bin/go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_fenced_scan_case multiline-cd-absolute-go-child-current pass \
  $'cd go/internal/parser/rust\n/usr/local/go/bin/go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case shell-prompt-env-chdir-parent-stale fail \
  '$ env -C go/internal/parser go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case shell-prompt-env-chdir-child-current pass \
  '$ env -C go/internal/parser/rust go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_fenced_scan_case quoted-go-chdir-parent-stale fail \
  '"/usr/local/go/bin/go" -C go/internal/parser test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_fenced_scan_case quoted-go-chdir-child-current pass \
  '"/usr/local/go/bin/go" -C go/internal/parser/rust test -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case goflags-env-attached-chdir-option-child-current pass \
  'env -Cgo/internal/parser/rust GOFLAGS='\''-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' go test . -count=1'
documented_selector_scan_case goflags-env-chdir-option-unrelated-current pass \
  'env -C go/internal/query GOFLAGS='\''-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' go test . -count=1'
documented_selector_scan_case goflags-env-dynamic-chdir-option-stale fail \
  'env -C "$PARSER_DIR" GOFLAGS='\''-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' go test . -count=1'
documented_selector_scan_case goflags-shell-and-env-chdir-parent-stale fail \
  'cd go && /usr/bin/env -C internal GOFLAGS='\''-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' go test ./parser -count=1'
documented_selector_scan_case goflags-env-split-string-parent-stale fail \
  'GOFLAGS='\''-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' /usr/bin/env -S '\''GOTOOLCHAIN=go1.26.6 go test ./internal/parser -count=1'\'''
documented_selector_scan_case goflags-env-split-string-child-current pass \
  'GOFLAGS='\''-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' env -S '\''go test ./internal/parser/rust -count=1'\'''
documented_selector_scan_case goflags-env-split-string-unrelated-current pass \
  'GOFLAGS='\''-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' env -S '\''go test ./internal/query -count=1'\'''
documented_selector_scan_case goflags-inside-env-split-chdir-child-current pass \
  'env -S '\''-C go/internal/parser/rust GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test . -count=1'\'''
documented_selector_scan_case goflags-inside-env-split-chdir-unrelated-current pass \
  'env -S '\''-C go/internal/query GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test . -count=1'\'''
documented_selector_scan_case goflags-inside-env-split-string-parent-stale fail \
  'env -S '\''GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser -count=1'\'''
documented_selector_scan_case goflags-inside-nested-env-split-string-parent-stale fail \
  'env -S '\''env GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ GOTOOLCHAIN=go1.26.6 go test ./internal/parser -count=1'\'''
documented_selector_scan_case goflags-inside-shell-wrapper-split-string-parent-stale fail \
  'env -S '\''sh -c "GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ GOTOOLCHAIN=go1.26.6 go test ./internal/parser -count=1"'\'''
documented_selector_scan_case goflags-inside-shell-wrapper-split-string-nonmatching-current pass \
  'env -S '\''sh -c "GOFLAGS=-run=^TestDefaultEngineParsePathRuby$ go test ./internal/parser -count=1"'\'''
documented_selector_scan_case goflags-shell-wrapper-local-overrides-inherited-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ env -S '\''sh -c "GOFLAGS=-run=^TestDefaultEngineParsePathRuby$ go test ./internal/parser -count=1"'\'''
documented_selector_scan_case goflags-shell-wrapper-last-local-assignment-current pass \
  'env -S '\''sh -c "GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ GOFLAGS=-run=^TestDefaultEngineParsePathRuby$ go test ./internal/parser -count=1"'\'''
documented_selector_scan_case goflags-shell-wrapper-last-local-assignment-stale fail \
  'env -S '\''sh -c "GOFLAGS=-run=^TestDefaultEngineParsePathRuby$ GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser -count=1"'\'''
documented_selector_scan_case goflags-inside-inert-split-string-data-current pass \
  'env -S '\''printf "GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser\\n"'\'''
documented_selector_scan_case goflags-before-inert-split-string-data-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ env -S '\''printf "go test ./internal/parser\\n"'\'''
documented_selector_scan_case goflags-inside-login-shell-wrapper-split-string-parent-stale fail \
  'env -S '\''/bin/zsh -lc "GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser -count=1"'\'''
documented_selector_scan_case goflags-shell-wrapper-inherits-child-cwd-current pass \
  'cd go/internal/parser/rust && env -S '\''sh -c "GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ GOTOOLCHAIN=go1.26.6 go test . -count=1"'\'''
documented_selector_scan_case goflags-shell-wrapper-composes-inner-child-cwd-current pass \
  'cd go && env -S '\''sh -c "cd internal/parser/rust && GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test . -count=1"'\'''
documented_selector_scan_case goflags-shell-wrapper-semicolon-inner-parent-cwd-stale fail \
  'env -S '\''sh -c "cd go/internal/parser; GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test . -count=1"'\'''
documented_selector_scan_case goflags-shell-wrapper-semicolon-inner-child-cwd-current pass \
  'env -S '\''sh -c "cd go/internal/parser/rust; GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test . -count=1"'\'''
documented_selector_scan_case goflags-shell-wrapper-inherits-env-child-cwd-current pass \
  'env -C go/internal/parser/rust -S '\''sh -c "GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test . -count=1"'\'''
documented_selector_scan_case goflags-shell-wrapper-inner-go-chdir-parent-stale fail \
  'cd go/internal/parser/rust && env -S '\''sh -c "GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go -C .. test . -count=1"'\'''
documented_selector_scan_case goflags-shell-wrapper-inner-go-chdir-child-current pass \
  'cd go/internal/parser && env -S '\''sh -c "GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go -C rust test . -count=1"'\'''
documented_selector_scan_case goflags-shell-wrapper-preamble-parent-stale fail \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ env -S '\''sh -c "unset ESHU_REVIEW_UNUSED; GOTOOLCHAIN=go1.26.6 go test ./internal/parser -count=1"'\'''
documented_selector_scan_case goflags-shell-wrapper-and-preamble-parent-stale fail \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ env -S '\''sh -c "unset ESHU_REVIEW_UNUSED && GOTOOLCHAIN=go1.26.6 go test ./internal/parser -count=1"'\'''
documented_selector_scan_case goflags-shell-wrapper-newline-preamble-parent-stale fail \
  $'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ env -S \'sh -c "unset ESHU_REVIEW_UNUSED\nGOTOOLCHAIN=go1.26.6 go test ./internal/parser -count=1"\''
documented_selector_scan_case goflags-shell-wrapper-comment-newline-parent-stale fail \
  $'env -S \'sh -c "unset ESHU_REVIEW_UNUSED # comment\nGOFLAGS=-run=TestDefaultEngineParsePathRustCapturesImplLifetimes go test ./internal/parser -count=1"\''
documented_selector_scan_case goflags-shell-wrapper-inert-comment-newline-parent-stale fail \
  $'env -S \'sh -c "printf ignored # comment\nGOFLAGS=-run=TestDefaultEngineParsePathRustCapturesImplLifetimes go test ./internal/parser -count=1"\''
documented_selector_scan_case goflags-shell-wrapper-exit-comment-newline-current pass \
  $'env -S \'sh -c "exit 0 # comment\nGOFLAGS=-run=TestDefaultEngineParsePathRustCapturesImplLifetimes go test ./internal/parser -count=1"\''
documented_selector_scan_case goflags-shell-wrapper-comment-only-parent-stale fail \
  $'env -S \'sh -c "# setup comment\nGOFLAGS=-run=TestDefaultEngineParsePathRustCapturesImplLifetimes go test ./internal/parser -count=1"\''
documented_selector_scan_case goflags-shell-wrapper-comment-only-child-current pass \
  $'env -S \'sh -c "# setup comment\nGOFLAGS=-run=TestDefaultEngineParsePathRustCapturesImplLifetimes go test ./internal/parser/rust -count=1"\''
documented_selector_scan_case goflags-shell-wrapper-comment-only-unrelated-current pass \
  $'env -S \'sh -c "# setup comment\nGOFLAGS=-run=TestDefaultEngineParsePathRustCapturesImplLifetimes go test ./internal/query -count=1"\''
documented_selector_scan_case goflags-shell-wrapper-terminating-preamble-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ env -S '\''sh -c "exit 0; GOTOOLCHAIN=go1.26.6 go test ./internal/parser -count=1"'\'''
documented_selector_scan_case goflags-shell-wrapper-exit-pipeline-parent-stale fail \
  'env -S '\''sh -c "exit 0 | GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser -count=1"'\'''
documented_selector_scan_case goflags-shell-wrapper-exit-background-parent-stale fail \
  'env -S '\''sh -c "exit 0 & GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser -count=1"'\'''
documented_selector_scan_case goflags-shell-wrapper-or-list-parent-stale fail \
  'env -S '\''sh -c "false || GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser -count=1"'\'''
documented_selector_scan_case goflags-shell-wrapper-pipeline-parent-stale fail \
  'env -S '\''sh -c "printf ignored | GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser -count=1"'\'''
documented_selector_scan_case goflags-shell-wrapper-background-parent-stale fail \
  'env -S '\''sh -c "sleep 0 & GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser -count=1"'\'''
documented_selector_scan_case goflags-shell-wrapper-redirection-child-current pass \
  'env -S '\''sh -c "GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser/rust -count=1 2>&1"'\'''
documented_selector_scan_case goflags-shell-wrapper-redirection-parent-stale fail \
  'env -S '\''sh -c "GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser -count=1 2>&1"'\'''
documented_selector_scan_case goflags-shell-wrapper-quoted-operators-inert-current pass \
  'env -S '\''sh -c "printf \"GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser | ignored && ignored\""'\''
documented_selector_scan_case goflags-env-option-before-split-string-parent-stale fail \
  'env -v -S '\''GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser -count=1'\'''
documented_selector_scan_case goflags-env-path-before-split-string-parent-stale fail \
  'env -P /opt/homebrew/bin -S '\''GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser -count=1'\'''
documented_selector_scan_case goflags-shell-cd-env-split-string-parent-stale fail \
  'cd go && /usr/bin/env -S '\''GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser -count=1'\'''
documented_selector_scan_case goflags-env-attached-split-string-parent-stale fail \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ env -S'\''go test ./internal/parser -count=1'\'''
documented_selector_scan_case goflags-inside-env-attached-split-string-parent-stale fail \
  'env -S'\''GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser -count=1'\'''
documented_selector_scan_case goflags-env-split-string-utility-argument-parent-stale fail \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ /usr/bin/env -S '\''GOTOOLCHAIN=go1.26.6 go test ./internal/parser -count=1 -args GOFLAGS=-run=TestDefaultEngineParsePathRuby'\'''
documented_selector_scan_case goflags-env-split-string-utility-argument-nonmatching-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRuby$ env -S '\''go test ./internal/parser -count=1 -args GOFLAGS=-run=TestDefaultEngineParsePathRustCapturesImplLifetimes'\'''
documented_selector_scan_case goflags-env-split-string-ignore-environment-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ env -S '\''-i go test ./internal/parser -count=1'\'''
documented_selector_scan_case goflags-env-split-string-dash-ignore-environment-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ env -S '\''- GOTOOLCHAIN=local go test ./internal/parser -count=1'\'''
documented_selector_scan_case goflags-env-split-string-long-ignore-environment-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ env -S '\''--ignore-environment GOTOOLCHAIN=local go test ./internal/parser -count=1'\'''
documented_selector_scan_case goflags-env-split-string-unset-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ env -S '\''-u GOFLAGS go test ./internal/parser -count=1'\'''
documented_selector_scan_case goflags-env-split-string-nonmatching-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRuby$ env -S '\''go test ./internal/parser -count=1'\'''
documented_selector_scan_case goflags-inside-env-split-string-nonmatching-current pass \
  'env -S '\''GOFLAGS=-run=^TestDefaultEngineParsePathRuby$ go test ./internal/parser -count=1'\'''
documented_selector_scan_case goflags-unset-clears-leading-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ env -u GOFLAGS go test ./internal/parser -count=1'
documented_selector_scan_case goflags-ignore-environment-clears-leading-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ env -i go test ./internal/parser -count=1'
documented_selector_scan_case goflags-dash-clears-leading-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ env - go test ./internal/parser -count=1'
documented_selector_scan_case goflags-reassigned-after-unset-parent-stale fail \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRuby$ env -u GOFLAGS GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser -count=1'
documented_selector_scan_case goflags-shell-cd-parent-stale fail \
  'cd go && GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser -count=1'
documented_selector_scan_case goflags-child-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser/rust -count=1'
documented_selector_scan_case goflags-mixed-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser ./internal/parser/rust -count=1'
documented_selector_scan_case goflags-unrelated-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/query -count=1'
documented_selector_scan_case goflags-without-selector-current pass \
  'GOFLAGS=-count=1 go test ./internal/parser'
documented_selector_scan_case goflags-nonmatching-selector-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRuby$ go test ./internal/parser -count=1'
documented_selector_scan_case goflags-empty-current pass \
  'GOFLAGS= go test ./internal/parser -count=1'
documented_selector_scan_case goflags-cli-selector-overrides-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser -run '^\''TestDefaultEngineParsePathRuby$'\'' -count=1'
documented_selector_scan_case goflags-cli-selector-overrides-stale fail \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRuby$ go test ./internal/parser -run '^\''TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' -count=1'
documented_selector_scan_case goflags-last-assignment-current pass \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ GOFLAGS=-run=^TestDefaultEngineParsePathRuby$ go test ./internal/parser -count=1'
documented_selector_scan_case goflags-last-assignment-stale fail \
  'GOFLAGS=-run=^TestDefaultEngineParsePathRuby$ GOFLAGS=-run=^TestDefaultEngineParsePathRustCapturesImplLifetimes$ go test ./internal/parser -count=1'
documented_selector_scan_case goflags-dynamic-parent-stale fail \
  'GOFLAGS="$GOFLAGS" go test ./internal/parser -count=1'
documented_selector_scan_case goflags-dynamic-child-stale fail \
  'GOFLAGS="$GOFLAGS" go test ./internal/parser/rust -count=1'
documented_selector_scan_case goflags-dynamic-unrelated-current pass \
  'GOFLAGS="$GOFLAGS" go test ./internal/query -count=1'
documented_selector_scan_case goflags-split-value-parent-stale fail \
  'GOFLAGS="-run TestDefaultEngineParsePathRustCapturesImplLifetimes" go test ./internal/parser -count=1'
documented_selector_scan_case goflags-split-value-unrelated-current pass \
  'GOFLAGS="-run TestDefaultEngineParsePathRustCapturesImplLifetimes" go test ./internal/query -count=1'
documented_selector_scan_case shell-cd-child-dynamic-env-prefix-stale fail \
  'cd go/internal/parser/rust && GOTOOLCHAIN="$TOOLCHAIN" go test . -run '\''TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' -count=1'
documented_selector_scan_case shell-cd-child-command-env-prefix-stale fail \
  'cd go/internal/parser/rust && GOCACHE="$(pwd)/cache" go test . -run '\''TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' -count=1'
documented_selector_scan_case shell-cd-child-malformed-env-prefix-stale fail \
  'cd go/internal/parser/rust && GOTOOLCHAIN="go1.26.6 go test . -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case shell-cd-unrelated-dynamic-env-prefix-current pass \
  'cd go/internal/query && GOTOOLCHAIN="$TOOLCHAIN" go test . -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case shell-cd-unrelated-env-command-prefix-current pass \
  'cd go/internal/query && env GOTOOLCHAIN=go1.26.6 go test . -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case shell-cd-unrelated-malformed-env-prefix-current pass \
  'cd go/internal/query && GOTOOLCHAIN="go1.26.6 go test . -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case shell-dynamic-cd-parent-relative-stale fail \
  'cd "$(git rev-parse --show-toplevel)/go/internal" && go test ./parser -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case shell-multiline-cd-parent-relative-stale fail \
  $'cd go/internal\ngo test ./parser -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case shell-multiline-cd-child-dot-current pass \
  $'cd go/internal/parser/rust\ngo test . -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case shell-multiline-cd-env-prefix-child-current pass \
  $'cd go/internal/parser/rust\nGOTOOLCHAIN=go1.26.6 \\\nGOCACHE=/tmp/eshu-cache \\\ngo test . -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case shell-multiline-later-cd-does-not-rebind-prefix pass \
  $'cd go\ngo test -c -o /tmp/query.test ./internal/query\ncd internal/query && \\\n/tmp/query.test -test.run Cargo -test.count=1'
documented_selector_scan_case quoted-cd-data-does-not-change-directory fail \
  'cd go/internal/parser && printf '\''%s\\n'\'' '\''cd go/internal/parser/rust'\'' && go test . -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_scan_case slash-selector-stale-parent fail \
  'go test ./internal/parser -run '\''TestDefaultEngineParsePathCargoTomlEmitsDependencyEvidence/foo'\'' -count=1'
documented_selector_scan_case slash-selector-nonmatching-parent pass \
  'go test ./internal/parser -run '\''TestDefaultEngineParsePathCargoTomlEmitsDependencyEvidence[/]foo'\'' -count=1'
documented_selector_scan_case flag-operand-stale-parent fail \
  'go test ./internal/parser -coverpkg ./internal/parser/rust -run Cargo -count=1'
documented_selector_scan_case boolean-flag-current-mixed pass \
  'go test ./internal/parser -cover -v ./internal/parser/rust -run Cargo -count=1'
documented_selector_scan_case escaped-dollar-unquoted-current pass \
  'go test ./internal/parser -run \$Cargo -count=1'
documented_selector_scan_case escaped-dollar-double-quoted-current pass \
  'go test ./internal/parser -run "\$Cargo" -count=1'
documented_selector_scan_case double-quoted-escaped-slash-current pass \
  'go test ./internal/parser -run "TestDefaultEngineParsePathCargoTomlEmitsDependencyEvidence\/foo" -count=1'
documented_selector_scan_case double-quoted-escaped-bracket-current pass \
  'go test ./internal/parser -run "TestDefaultEngineParsePathCargo\[" -count=1'

# shellcheck source=scripts/lib/test-verify-parser-relationship-kit-record-discovery-cases.sh
. "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-record-discovery-cases.sh"
