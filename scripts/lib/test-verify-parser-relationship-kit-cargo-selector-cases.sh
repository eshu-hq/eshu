#!/usr/bin/env bash

# Sourced by test-verify-parser-relationship-kit.sh after its fixture helpers
# are defined. Keep the Cargo command cases outside the parent test driver so
# that driver stays below the repository's 500-line cap.
# shellcheck disable=SC2154 # Parent defines fixture helpers and repo paths.

# A documented Cargo selector must follow the tests into the Rust child
# package. The parent package command exits zero while selecting no Cargo
# tests, so this is a false-green rather than a harmless stale path.
documented_selector_case() {
  local name="$1" expected="$2" command="$3" actual=pass
  local error_file="${tmp_root}/documented-selector.err"
  if documented_parser_command_is_stale "$command" 2>"$error_file"; then
    actual=fail
  fi
  if rg -qF 'could not build documented -run matcher' "$error_file" ||
    { [ "$name" != invalid-cargo-selector ] &&
      rg -qF 'could not evaluate documented -run' "$error_file"; }; then
    printf 'documented selector matcher failed in %s\n' "$name" >&2
    sed -n '1,40p' "$error_file" >&2
    exit 1
  fi
  if [ "$actual" != "$expected" ]; then
    printf 'documented selector %s: expected %s, got %s\n' \
      "$name" "$expected" "$actual" >&2
    exit 1
  fi
}

documented_selector_path_case() {
  local name="$1" expected="$2" path="$3" command="$4" fixture
  fixture="$(init_repo "${name}")"
  mkdir -p "$(dirname "${fixture}/${path}")"
  printf '\n`%s`\n' "${command}" >>"${fixture}/${path}"
  git -C "${fixture}" add .
  git -C "${fixture}" commit -q -m "${name}"
  case "${expected}" in
    fail) expect_fail "${fixture}" ;;
    pass) expect_pass "${fixture}" ;;
    *) printf 'invalid documented selector expectation: %s\n' "${expected}" >&2; exit 1 ;;
  esac
}

# A single command may list both packages. The parent contributes no matching
# Cargo tests, but the Rust child in the same invocation executes the suite, so
# these are current commands rather than stale parent-only selectors.
documented_selector_case mixed-default-parent-child pass \
  'go test github.com/eshu-hq/eshu/go/internal/parser github.com/eshu-hq/eshu/go/internal/parser/rust -run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case mixed-default-child-parent-wrapped pass \
  $'go test -run=TestDefaultEngineParsePathCargo \\\n  ./internal/parser/rust ./internal/parser -count=1'
documented_selector_case mixed-coverage-parent-child-flags-first pass \
  'go test -run TestCargoDependencyCoverageMatrixMarksCargoFilesCovered ./internal/parser ./internal/parser/rust -count=1'
documented_selector_case mixed-coverage-child-parent-wrapped pass \
  $'go test ./internal/parser/rust \\\n  ./internal/parser -run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'
documented_selector_case mixed-test-run-parent-child pass \
  'go test -test.run TestDefaultEngineParsePathCargo ./internal/parser ./internal/parser/rust -count=1'
documented_selector_case mixed-test-run-child-parent-wrapped pass \
  $'go test ./internal/parser/rust \\\n  ./internal/parser -test.run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'

# A Rust path is proof only when it is a package argument. Shell comments and
# test-binary arguments do not make the Rust package part of the invocation.
documented_selector_case child-mention-in-comment fail \
  'go test ./internal/parser -run TestDefaultEngineParsePathCargo -count=1 # ./internal/parser/rust'
documented_selector_case child-mention-after-args fail \
  'go test ./internal/parser -run TestDefaultEngineParsePathCargo -count=1 -args ./internal/parser/rust'
documented_selector_case child-mention-after-double-dash fail \
  'go test ./internal/parser -run TestDefaultEngineParsePathCargo -count=1 -- ./internal/parser/rust'
documented_selector_case selector-and-child-after-args fail \
  'go test ./internal/parser -count=1 -args -test.run TestDefaultEngineParsePathCargo ./internal/parser/rust'
documented_selector_case selector-and-child-after-double-dash fail \
  'go test ./internal/parser -count=1 -- -test.run TestDefaultEngineParsePathCargo ./internal/parser/rust'

# A later shell command is not part of the `go test` package list. Exercise
# every command separator the scanner recognizes, including a literal newline.
documented_selector_case child-mention-after-semicolon fail \
  'go test ./internal/parser -run TestDefaultEngineParsePathCargo -count=1; echo ./internal/parser/rust'
documented_selector_case child-mention-after-and fail \
  'go test ./internal/parser -run TestDefaultEngineParsePathCargo -count=1 && echo ./internal/parser/rust'
documented_selector_case child-mention-after-or fail \
  'go test ./internal/parser -run TestDefaultEngineParsePathCargo -count=1 || echo ./internal/parser/rust'
documented_selector_case child-mention-after-pipe fail \
  'go test ./internal/parser -run TestDefaultEngineParsePathCargo -count=1 | echo ./internal/parser/rust'
documented_selector_case child-mention-after-newline fail \
  $'go test ./internal/parser -run TestDefaultEngineParsePathCargo -count=1\necho ./internal/parser/rust'

# Quoted and commented separator text is data, not a command boundary. A real
# child exempts the parent only when the selector runs a relocated Cargo test.
documented_selector_case quoted-child-mention fail \
  'go test ./internal/parser -run TestDefaultEngineParsePathCargo "echo ./internal/parser/rust ; && || |" -count=1'
documented_selector_case mixed-child-before-separator pass \
  'go test ./internal/parser ./internal/parser/rust -run TestDefaultEngineParsePathCargo -count=1; echo done'
documented_selector_case mixed-legacy-selector-with-quoted-separators fail \
  'go test ./internal/parser ./internal/parser/rust -run "^TestDefaultEngineParsePathCargo$|^Other$" -count=1'
documented_selector_case mixed-child-with-commented-separators pass \
  'go test ./internal/parser ./internal/parser/rust -run TestDefaultEngineParsePathCargo -count=1 # ; && || |'

# Go normalizes each of these spellings to the parent parser package. They all
# exit zero after selecting no Cargo tests, so none may evade the stale guard.
documented_selector_case module-parent-path fail \
  'go test github.com/eshu-hq/eshu/go/internal/parser -run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case trailing-slash-parent-path fail \
  'go test ./internal/parser/ -run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case dot-normalized-parent-path fail \
  'go test ./internal/parser/. -run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case dotdot-normalized-parent-path fail \
  'go test ./internal/parser/../parser -run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case dotdot-trailing-normalized-parent-path fail \
  'go test ./internal/parser/../parser/ -run '\''^TestDefaultEngineParsePathCargo$'\'' -count=1 -v'
documented_selector_case adjacent-parser-name pass \
  'go test ./internal/parser-extra -run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case normalization-does-not-cross-root pass \
  'go test ./internal/parser/../../parser -run TestDefaultEngineParsePathCargo -count=1'

# `go test -run` accepts Go regular expressions, not only literal test names.
# Any selector matching a relocated Cargo test is stale in the parent package;
# malformed expressions fail closed instead of silently bypassing the guard.
documented_selector_case grouped-cargo-selector fail \
  'go test ./internal/parser -run '\''TestDefaultEngineParsePath(Cargo)'\'' -count=1'
documented_selector_case general-cargo-selector fail \
  'go test ./internal/parser -run Cargo -count=1'
documented_selector_case match-all-cargo-selector fail \
  'go test ./internal/parser -run '\''.'\'' -count=1'
documented_selector_case invalid-cargo-selector fail \
  'go test ./internal/parser -run '\''['\'' -count=1'
documented_selector_case adjacent-nonmatching-selector pass \
  'go test ./internal/parser -run TestDefaultEngineParsePathRuby -count=1'
documented_selector_case anchored-extra-selector pass \
  'go test ./internal/parser -run '\''^TestDefaultEngineParsePathCargoExtra$'\'' -count=1'
documented_selector_case cargo-parent-with-subtest-selector fail \
  'go test ./internal/parser -run '\''TestDefaultEngineParsePathCargoTomlEmitsDependencyEvidence/foo'\'' -count=1'
documented_selector_case escaped-slash-selector pass \
  'go test ./internal/parser -run '\''TestDefaultEngineParsePathCargoTomlEmitsDependencyEvidence\/foo'\'' -count=1'
documented_selector_case bracketed-slash-selector pass \
  'go test ./internal/parser -run '\''TestDefaultEngineParsePathCargoTomlEmitsDependencyEvidence[/]foo'\'' -count=1'
documented_selector_case parenthesized-slash-selector pass \
  'go test ./internal/parser -run '\''TestDefaultEngineParsePathCargoTomlEmitsDependencyEvidence(/)foo'\'' -count=1'
documented_selector_case alternated-subtest-selector fail \
  'go test ./internal/parser -run '\''Cargo/foo|Ruby'\'' -count=1'

# Redirections stay in the same shell command; only real unquoted command
# separators may terminate package and selector scanning.
documented_selector_case redirect-stderr-to-stdout-before-run fail \
  'go test ./internal/parser 2>&1 -run Cargo -count=1'
documented_selector_case redirect-stdout-to-stderr-before-run fail \
  'go test ./internal/parser >&2 -run Cargo -count=1'
documented_selector_case redirect-stdin-before-run fail \
  'go test ./internal/parser <&0 -run Cargo -count=1'
documented_selector_case redirect-both-before-run fail \
  'go test ./internal/parser &>cargo.out -run Cargo -count=1'
documented_selector_case quoted-redirection-before-run fail \
  'go test ./internal/parser "2>&1 &>cargo.out" -run Cargo -count=1'
documented_selector_case mixed-child-with-redirections pass \
  'go test ./internal/parser 2>&1 ./internal/parser/rust -run Cargo -count=1'

# Flag operands are not package arguments, whether separated or joined with
# equals. Boolean flags consume no following token, so a real child remains.
documented_selector_case child-only-in-flag-operands fail \
  'go test ./internal/parser -coverpkg ./internal/parser/rust -tags ./internal/parser/rust -gcflags ./internal/parser/rust -ldflags ./internal/parser/rust -run Cargo -count 1 -timeout 1m'
documented_selector_case child-only-in-equals-flag-operands fail \
  'go test ./internal/parser -coverpkg=./internal/parser/rust -tags=./internal/parser/rust -gcflags=./internal/parser/rust -ldflags=./internal/parser/rust -run=Cargo -count=1 -timeout=1m'
documented_selector_case mixed-child-after-value-flags pass \
  'go test ./internal/parser -tags integration -gcflags all=-N -ldflags=-s -count 1 -timeout 1m ./internal/parser/rust -run Cargo'
documented_selector_case mixed-child-after-boolean-flags pass \
  'go test ./internal/parser -cover -race -short -v ./internal/parser/rust -run Cargo -count=1'

# Shell-expanded selectors cannot be proven from documentation text. Reject
# them conservatively; an explicitly escaped dollar remains literal regex data.
documented_selector_case ansi-c-expanded-selector fail \
  'go test ./internal/parser -run $'\''Cargo'\'' -count=1'
documented_selector_case variable-expanded-selector fail \
  'go test ./internal/parser -run $CARGO_SELECTOR -count=1'
documented_selector_case command-substitution-selector fail \
  'go test ./internal/parser -run $(printf Cargo) -count=1'
documented_selector_case escaped-dollar-selector pass \
  'go test ./internal/parser -run '\''\$Cargo'\'' -count=1'

# A bare package token is not a module-relative package path. The valid Rust
# child must not hide that invalid parent token in a mixed package list.
documented_selector_case mixed-bare-default-parent-child fail \
  'go test internal/parser ./internal/parser/rust -run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case mixed-bare-default-child-parent fail \
  'go test ./internal/parser/rust internal/parser -run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case mixed-bare-coverage-parent-child fail \
  'go test internal/parser ./internal/parser/rust -run TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'
documented_selector_case mixed-bare-coverage-child-parent fail \
  'go test ./internal/parser/rust internal/parser -run TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'

# The test-binary flag spelling is valid through `go test` too. Cover both
# Cargo selectors, both flag/package orders, and both space/equals forms.
documented_selector_case test-run-default-flags-space fail \
  'go test -test.run TestDefaultEngineParsePathCargo ./internal/parser -count=1'
documented_selector_case test-run-default-flags-equals-wrapped fail \
  $'go test -test.run=TestDefaultEngineParsePathCargo \\\n  ./internal/parser -count=1'
documented_selector_case test-run-default-package-space-wrapped fail \
  $'go test ./internal/parser \\\n  -test.run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case test-run-default-package-equals fail \
  'go test ./internal/parser -test.run=TestDefaultEngineParsePathCargo -count=1'

documented_selector_case test-run-coverage-flags-space fail \
  'go test -test.run TestCargoDependencyCoverageMatrixMarksCargoFilesCovered ./internal/parser -count=1'
documented_selector_case test-run-coverage-flags-equals-wrapped fail \
  $'go test -test.run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered \\\n  ./internal/parser -count=1'
documented_selector_case test-run-coverage-package-space-wrapped fail \
  $'go test ./internal/parser \\\n  -test.run TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'
documented_selector_case test-run-coverage-package-equals fail \
  'go test ./internal/parser -test.run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'

documented_selector_case test-run-default-child-flags-space pass \
  'go test -test.run TestDefaultEngineParsePathCargo ./internal/parser/rust -count=1'
documented_selector_case test-run-default-child-flags-equals-wrapped pass \
  $'go test -test.run=TestDefaultEngineParsePathCargo \\\n  ./internal/parser/rust -count=1'
documented_selector_case test-run-default-child-package-space-wrapped pass \
  $'go test ./internal/parser/rust \\\n  -test.run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case test-run-default-child-package-equals pass \
  'go test ./internal/parser/rust -test.run=TestDefaultEngineParsePathCargo -count=1'

documented_selector_case test-run-coverage-child-flags-space pass \
  'go test -test.run TestCargoDependencyCoverageMatrixMarksCargoFilesCovered ./internal/parser/rust -count=1'
documented_selector_case test-run-coverage-child-flags-equals-wrapped pass \
  $'go test -test.run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered \\\n  ./internal/parser/rust -count=1'
documented_selector_case test-run-coverage-child-package-space-wrapped pass \
  $'go test ./internal/parser/rust \\\n  -test.run TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'
documented_selector_case test-run-coverage-child-package-equals pass \
  'go test ./internal/parser/rust -test.run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'

# Go accepts the optional test. prefix with either one or two leading dashes.
# Cover the double-dash spelling independently of the --run cases below.
documented_selector_case double-test-run-default-parent-flags-space fail \
  'go test --test.run TestDefaultEngineParsePathCargo ./internal/parser -count=1'
documented_selector_case double-test-run-default-parent-flags-equals fail \
  'go test --test.run=TestDefaultEngineParsePathCargo ./internal/parser -count=1'
documented_selector_case double-test-run-default-parent-package-space fail \
  'go test ./internal/parser --test.run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case double-test-run-default-parent-package-equals fail \
  'go test ./internal/parser --test.run=TestDefaultEngineParsePathCargo -count=1'
documented_selector_case double-test-run-coverage-parent-flags-space fail \
  'go test --test.run TestCargoDependencyCoverageMatrixMarksCargoFilesCovered ./internal/parser -count=1'
documented_selector_case double-test-run-coverage-parent-flags-equals fail \
  'go test --test.run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered ./internal/parser -count=1'
documented_selector_case double-test-run-coverage-parent-package-space fail \
  'go test ./internal/parser --test.run TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'
documented_selector_case double-test-run-coverage-parent-package-equals fail \
  'go test ./internal/parser --test.run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'

documented_selector_case double-test-run-default-child-flags-space pass \
  'go test --test.run TestDefaultEngineParsePathCargo ./internal/parser/rust -count=1'
documented_selector_case double-test-run-default-child-flags-equals pass \
  'go test --test.run=TestDefaultEngineParsePathCargo ./internal/parser/rust -count=1'
documented_selector_case double-test-run-default-child-package-space pass \
  'go test ./internal/parser/rust --test.run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case double-test-run-default-child-package-equals pass \
  'go test ./internal/parser/rust --test.run=TestDefaultEngineParsePathCargo -count=1'
documented_selector_case double-test-run-coverage-child-flags-space pass \
  'go test --test.run TestCargoDependencyCoverageMatrixMarksCargoFilesCovered ./internal/parser/rust -count=1'
documented_selector_case double-test-run-coverage-child-flags-equals pass \
  'go test --test.run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered ./internal/parser/rust -count=1'
documented_selector_case double-test-run-coverage-child-package-space pass \
  'go test ./internal/parser/rust --test.run TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'
documented_selector_case double-test-run-coverage-child-package-equals pass \
  'go test ./internal/parser/rust --test.run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'

# Package Markdown carries the same command contract as docs/. Exercise the
# Rust README directly so a scan-root regression cannot hide behind CI routing.
documented_selector_path_case rust-readme-stale-mixed-legacy fail \
  'go/internal/parser/rust/README.md' \
  'go test ./internal/parser ./internal/parser/rust -run "^TestDefaultEngineParsePathCargo$" -count=1'
documented_selector_path_case rust-readme-current-child pass \
  'go/internal/parser/rust/README.md' \
  'go test ./internal/parser/rust -run TestDefaultEngineParsePathCargo -count=1'

stale_cargo_selector_repo="$(init_repo stale-cargo-selector)"
printf '\n`go test ./internal/parser -run '\''TestDefaultEngineParsePathCargo'\'' -count=1`\n' \
  >>"${stale_cargo_selector_repo}/docs/public/contributing-language-support.md"
git -C "${stale_cargo_selector_repo}" add .
git -C "${stale_cargo_selector_repo}" commit -q -m 'stale Cargo test selector'
expect_fail "${stale_cargo_selector_repo}"

# Markdown commonly wraps the selector onto the following line. This shape
# must not evade the stale-parent-package guard.
wrapped_stale_cargo_selector_repo="$(init_repo wrapped-stale-cargo-selector)"
printf '%s\n' '' '`go test ./internal/parser -run' \
  '  '\''TestDefaultEngineParsePathCargo'\'' -count=1`' \
  >>"${wrapped_stale_cargo_selector_repo}/docs/public/contributing-language-support.md"
git -C "${wrapped_stale_cargo_selector_repo}" add .
git -C "${wrapped_stale_cargo_selector_repo}" commit -q -m 'wrapped stale Cargo test selector'
expect_fail "${wrapped_stale_cargo_selector_repo}"

# Go accepts build flags before package arguments. This valid spelling still
# targets the stale parent package and exits zero after selecting no tests.
flags_first_stale_cargo_selector_repo="$(init_repo flags-first-stale-cargo-selector)"
printf '\n`go test -run '\''TestDefaultEngineParsePathCargo'\'' ./internal/parser -count=1`\n' \
  >>"${flags_first_stale_cargo_selector_repo}/docs/public/contributing-language-support.md"
git -C "${flags_first_stale_cargo_selector_repo}" add .
git -C "${flags_first_stale_cargo_selector_repo}" commit -q -m 'flags-first stale Cargo test selector'
expect_fail "${flags_first_stale_cargo_selector_repo}"

wrapped_flags_first_stale_cargo_selector_repo="$(init_repo wrapped-flags-first-stale-cargo-selector)"
printf '%s\n' '' '`go test -run '\''TestDefaultEngineParsePathCargo'\'' \' \
  '  ./internal/parser -count=1`' \
  >>"${wrapped_flags_first_stale_cargo_selector_repo}/docs/public/contributing-language-support.md"
git -C "${wrapped_flags_first_stale_cargo_selector_repo}" add .
git -C "${wrapped_flags_first_stale_cargo_selector_repo}" commit -q -m 'wrapped flags-first stale Cargo test selector'
expect_fail "${wrapped_flags_first_stale_cargo_selector_repo}"

# Single-dash flags accept the equals spelling too. Exercise both selectors and
# both package/flag orders; alternate the wrapped shape so removing the equals
# branch cannot hide behind either command layout.
documented_selector_case single-run-default-flags-equals fail \
  'go test -run=TestDefaultEngineParsePathCargo ./internal/parser -count=1'
documented_selector_case single-run-default-package-equals-wrapped fail \
  $'go test ./internal/parser \\\n  -run=TestDefaultEngineParsePathCargo -count=1'
documented_selector_case single-run-default-bare-package fail \
  'go test internal/parser -run=TestDefaultEngineParsePathCargo -count=1'
documented_selector_case single-run-coverage-flags-equals-wrapped fail \
  $'go test -run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered \\\n  ./internal/parser -count=1'
documented_selector_case single-run-coverage-package-equals fail \
  'go test ./internal/parser -run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'

# Double-dash is also a valid Go flag spelling. Cover both Cargo selectors,
# both flag/package orders, and both the space and equals forms. Wrapped forms
# exercise the same Markdown layouts used by the existing single-dash cases.
documented_selector_case double-run-default-flags-space fail \
  'go test --run TestDefaultEngineParsePathCargo ./internal/parser -count=1'
documented_selector_case double-run-default-flags-equals-wrapped fail \
  $'go test --run=TestDefaultEngineParsePathCargo \\\n  ./internal/parser -count=1'
documented_selector_case double-run-default-package-space-wrapped fail \
  $'go test ./internal/parser \\\n  --run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case double-run-default-package-equals fail \
  'go test ./internal/parser --run=TestDefaultEngineParsePathCargo -count=1'

documented_selector_case double-run-coverage-flags-space fail \
  'go test --run TestCargoDependencyCoverageMatrixMarksCargoFilesCovered ./internal/parser -count=1'
documented_selector_case double-run-coverage-flags-equals-wrapped fail \
  $'go test --run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered \\\n  ./internal/parser -count=1'
documented_selector_case double-run-coverage-package-space-wrapped fail \
  $'go test ./internal/parser \\\n  --run TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'
documented_selector_case double-run-coverage-package-equals fail \
  'go test ./internal/parser --run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'

cargo_selector_repo="$(init_repo cargo-selector)"
printf '\n`go test ./internal/parser/rust -run '\''TestDefaultEngineParsePathCargo'\'' -count=1`\n' \
  >>"${cargo_selector_repo}/docs/public/contributing-language-support.md"
git -C "${cargo_selector_repo}" add .
git -C "${cargo_selector_repo}" commit -q -m 'current Cargo test selector'
expect_pass "${cargo_selector_repo}"

wrapped_cargo_selector_repo="$(init_repo wrapped-cargo-selector)"
printf '%s\n' '' '`go test ./internal/parser/rust -run' \
  '  '\''TestDefaultEngineParsePathCargo'\'' -count=1`' \
  >>"${wrapped_cargo_selector_repo}/docs/public/contributing-language-support.md"
git -C "${wrapped_cargo_selector_repo}" add .
git -C "${wrapped_cargo_selector_repo}" commit -q -m 'wrapped current Cargo test selector'
expect_pass "${wrapped_cargo_selector_repo}"

flags_first_cargo_selector_repo="$(init_repo flags-first-cargo-selector)"
printf '\n`go test -run '\''TestDefaultEngineParsePathCargo'\'' ./internal/parser/rust -count=1`\n' \
  >>"${flags_first_cargo_selector_repo}/docs/public/contributing-language-support.md"
git -C "${flags_first_cargo_selector_repo}" add .
git -C "${flags_first_cargo_selector_repo}" commit -q -m 'flags-first current Cargo test selector'
expect_pass "${flags_first_cargo_selector_repo}"

wrapped_flags_first_cargo_selector_repo="$(init_repo wrapped-flags-first-cargo-selector)"
printf '%s\n' '' '`go test -run '\''TestDefaultEngineParsePathCargo'\'' \' \
  '  ./internal/parser/rust -count=1`' \
  >>"${wrapped_flags_first_cargo_selector_repo}/docs/public/contributing-language-support.md"
git -C "${wrapped_flags_first_cargo_selector_repo}" add .
git -C "${wrapped_flags_first_cargo_selector_repo}" commit -q -m 'wrapped flags-first current Cargo test selector'
expect_pass "${wrapped_flags_first_cargo_selector_repo}"

documented_selector_case single-run-default-child-flags-equals pass \
  'go test -run=TestDefaultEngineParsePathCargo ./internal/parser/rust -count=1'
documented_selector_case single-run-default-child-package-equals-wrapped pass \
  $'go test ./internal/parser/rust \\\n  -run=TestDefaultEngineParsePathCargo -count=1'
documented_selector_case single-run-coverage-child-flags-equals-wrapped pass \
  $'go test -run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered \\\n  ./internal/parser/rust -count=1'
documented_selector_case single-run-coverage-child-package-equals pass \
  'go test ./internal/parser/rust -run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'

documented_selector_case double-run-default-child-flags-space pass \
  'go test --run TestDefaultEngineParsePathCargo ./internal/parser/rust -count=1'
documented_selector_case double-run-default-child-flags-equals-wrapped pass \
  $'go test --run=TestDefaultEngineParsePathCargo \\\n  ./internal/parser/rust -count=1'
documented_selector_case double-run-default-child-package-space-wrapped pass \
  $'go test ./internal/parser/rust \\\n  --run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case double-run-default-child-package-equals pass \
  'go test ./internal/parser/rust --run=TestDefaultEngineParsePathCargo -count=1'

documented_selector_case double-run-coverage-child-flags-space pass \
  'go test --run TestCargoDependencyCoverageMatrixMarksCargoFilesCovered ./internal/parser/rust -count=1'
documented_selector_case double-run-coverage-child-flags-equals-wrapped pass \
  $'go test --run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered \\\n  ./internal/parser/rust -count=1'
documented_selector_case double-run-coverage-child-package-space-wrapped pass \
  $'go test ./internal/parser/rust \\\n  --run TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'
documented_selector_case double-run-coverage-child-package-equals pass \
  'go test ./internal/parser/rust --run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'

# Separate code spans must not be joined into one synthetic stale command.
split_command_context_repo="$(init_repo split-command-context)"
printf '%s\n' '' \
  '`go test -run '\''TestDefaultEngineParsePathCargo'\'' ./internal/parser/rust -count=1`' \
  '`go test ./internal/parser -count=1`' \
  >>"${split_command_context_repo}/docs/public/contributing-language-support.md"
git -C "${split_command_context_repo}" add .
git -C "${split_command_context_repo}" commit -q -m 'separate non-stale Cargo commands'
expect_pass "${split_command_context_repo}"

fenced_split_command_repo="$(init_repo fenced-split-command)"
printf '%s\n' '' '```bash' \
  'go test -run '\''TestDefaultEngineParsePathCargo'\'' ./internal/parser/rust -count=1' \
  'go test ./internal/parser -count=1' '```' \
  >>"${fenced_split_command_repo}/docs/public/contributing-language-support.md"
git -C "${fenced_split_command_repo}" add .
git -C "${fenced_split_command_repo}" commit -q -m 'separate fenced non-stale Cargo commands'
expect_pass "${fenced_split_command_repo}"

double_run_split_command_repo="$(init_repo double-run-split-command)"
printf '%s\n' '' \
  '`go test --run TestDefaultEngineParsePathCargo ./internal/parser/rust -count=1`' \
  '`go test ./internal/parser -count=1`' \
  >>"${double_run_split_command_repo}/docs/public/contributing-language-support.md"
git -C "${double_run_split_command_repo}" add .
git -C "${double_run_split_command_repo}" commit -q -m 'separate double-run Cargo commands'
expect_pass "${double_run_split_command_repo}"

double_run_fenced_split_repo="$(init_repo double-run-fenced-split)"
printf '%s\n' '' '```bash' \
  'go test --run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered ./internal/parser/rust -count=1' \
  'go test ./internal/parser -count=1' '```' \
  >>"${double_run_fenced_split_repo}/docs/public/contributing-language-support.md"
git -C "${double_run_fenced_split_repo}" add .
git -C "${double_run_fenced_split_repo}" commit -q -m 'separate double-run fenced commands'
expect_pass "${double_run_fenced_split_repo}"

# Force the helper's rg process to fail. A scan error must not become the same
# success result as "no stale command found", and the diagnostic must preserve
# both rg's error and the helper's exit-code context.
fake_rg_dir="${tmp_root}/fake-rg-bin"
fake_rg_error="${tmp_root}/fake-rg.err"
mkdir -p "${fake_rg_dir}"
printf '%s\n' '#!/usr/bin/env bash' \
  "printf '%s\\n' 'synthetic documented-command rg failure' >&2" \
  'exit 2' >"${fake_rg_dir}/rg"
chmod +x "${fake_rg_dir}/rg"
if PATH="${fake_rg_dir}:${PATH}" bash -c \
  '. "$1"; validate_documented_parser_test_commands "$2"' \
  _ "${repo_root}/scripts/lib/parser_documented_test_commands.sh" "${repo_root}" \
  >"${tmp_root}/fake-rg.out" 2>"${fake_rg_error}"; then
  printf 'expected documented Rust command scan to fail when rg exits 2\n' >&2
  exit 1
fi
rg -qF 'synthetic documented-command rg failure' "${fake_rg_error}" || {
  printf 'documented Rust command scan dropped the underlying rg diagnostic\n' >&2
  exit 1
}
rg -qF 'documented Rust command scan failed (rg exit 2)' "${fake_rg_error}" || {
  printf 'documented Rust command scan omitted the rg exit-code diagnostic\n' >&2
  exit 1
}

stale_cargo_coverage_selector_repo="$(init_repo stale-cargo-coverage-selector)"
printf '\n`go test ./internal/parser -run '\''TestCargoDependencyCoverageMatrixMarksCargoFilesCovered'\'' -count=1`\n' \
  >>"${stale_cargo_coverage_selector_repo}/docs/public/contributing-language-support.md"
git -C "${stale_cargo_coverage_selector_repo}" add .
git -C "${stale_cargo_coverage_selector_repo}" commit -q -m 'stale Cargo coverage selector'
expect_fail "${stale_cargo_coverage_selector_repo}"

wrapped_stale_cargo_coverage_selector_repo="$(init_repo wrapped-stale-cargo-coverage-selector)"
printf '%s\n' '' '`go test ./internal/parser -run' \
  '  '\''TestCargoDependencyCoverageMatrixMarksCargoFilesCovered'\'' -count=1`' \
  >>"${wrapped_stale_cargo_coverage_selector_repo}/docs/public/contributing-language-support.md"
git -C "${wrapped_stale_cargo_coverage_selector_repo}" add .
git -C "${wrapped_stale_cargo_coverage_selector_repo}" commit -q -m 'wrapped stale Cargo coverage selector'
expect_fail "${wrapped_stale_cargo_coverage_selector_repo}"

cargo_coverage_selector_repo="$(init_repo cargo-coverage-selector)"
printf '\n`go test ./internal/parser/rust -run '\''TestCargoDependencyCoverageMatrixMarksCargoFilesCovered'\'' -count=1`\n' \
  >>"${cargo_coverage_selector_repo}/docs/public/contributing-language-support.md"
git -C "${cargo_coverage_selector_repo}" add .
git -C "${cargo_coverage_selector_repo}" commit -q -m 'current Cargo coverage selector'
expect_pass "${cargo_coverage_selector_repo}"

wrapped_cargo_coverage_selector_repo="$(init_repo wrapped-cargo-coverage-selector)"
printf '%s\n' '' '`go test ./internal/parser/rust -run' \
  '  '\''TestCargoDependencyCoverageMatrixMarksCargoFilesCovered'\'' -count=1`' \
  >>"${wrapped_cargo_coverage_selector_repo}/docs/public/contributing-language-support.md"
git -C "${wrapped_cargo_coverage_selector_repo}" add .
git -C "${wrapped_cargo_coverage_selector_repo}" commit -q -m 'wrapped current Cargo coverage selector'
expect_pass "${wrapped_cargo_coverage_selector_repo}"

# shellcheck source=scripts/lib/test-verify-parser-relationship-kit-rust-selector-derived-cases.sh
. "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-rust-selector-derived-cases.sh"

# shellcheck source=scripts/lib/test-verify-parser-relationship-kit-cargo-selector-integration-cases.sh
. "${repo_root}/scripts/lib/test-verify-parser-relationship-kit-cargo-selector-integration-cases.sh"
cleanup_documented_selector_test_binary
