#!/usr/bin/env bash

# Sourced after the documented command classifier and fixture helpers.
# shellcheck disable=SC2154 # Parent defines tmp_root and repo_root.

# The go command applies -C before resolving package arguments. Exercise the
# command-local and global spellings so a dot package cannot hide a relocated
# Rust selector.
documented_selector_case change-dir-parent-space fail \
  'go test -C go/internal/parser . -run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case change-dir-parent-equals fail \
  'go test -C=go/internal/parser . -run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'
documented_selector_case change-dir-global-parent-space fail \
  'go -C go/internal/parser test . --test.run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case change-dir-global-parent-equals fail \
  'go --C=go/internal/parser test . -test.run=TestCargoDependencyCoverageMatrixMarksCargoFilesCovered -count=1'
documented_selector_case change-dir-module-parent fail \
  'go test -C go ./internal/parser -run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case change-dir-relative-parent fail \
  'go test -C go/internal ./parser -run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case change-dir-parent-from-child fail \
  'go test -C go/internal/parser/rust .. -run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case change-dir-child pass \
  'go test -C=go/internal/parser/rust . -run=TestDefaultEngineParsePathCargo -count=1'
documented_selector_case change-dir-implicit-parent fail \
  'go test -C go/internal/parser -run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case change-dir-global-implicit-parent fail \
  'go -C=go/internal/parser test -run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case change-dir-implicit-child pass \
  'go test -C go/internal/parser/rust -run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case change-dir-mixed-concrete pass \
  'go test -C go/internal/parser . ./rust -run TestDefaultEngineParsePathCargoTomlEmitsDependencyEvidence -count=1'
documented_selector_case change-dir-mixed-legacy fail \
  'go test -C=go/internal/parser . ./rust -run "^TestDefaultEngineParsePathCargo$|^Other$" -count=1'
documented_selector_case change-dir-unrelated pass \
  'go test -C go/internal/query . -run TestDefaultEngineParsePathCargo -count=1'
documented_selector_case change-dir-parent-recursive pass \
  'go test -C go/internal/parser ./... -run TestDefaultEngineParsePathCargoTomlEmitsDependencyEvidence -count=1'
documented_selector_case change-dir-mixed-recursive-child pass \
  'go test -C go/internal/parser . ./rust/... -run TestDefaultEngineParsePathCargoTomlEmitsDependencyEvidence -count=1'
documented_selector_case change-dir-dynamic-package-before-child fail \
  'go test -C go/internal/parser "$RUST_PACKAGE" ./rust -run TestDefaultEngineParsePathCargoTomlEmitsDependencyEvidence -count=1'
documented_selector_case change-dir-glob fail \
  'go test -C go/internal/par* . -run TestDefaultEngineParsePathCargoTomlEmitsDependencyEvidence -count=1'
documented_selector_case change-dir-tilde fail \
  'go test -C ~/personal-repos/eshu/go/internal/parser . -run TestDefaultEngineParsePathCargoTomlEmitsDependencyEvidence -count=1'
documented_selector_case change-dir-dynamic-can-inject-selector fail \
  'go test -C "$RUST_DIR" . -run "^Other$" -count=1'
documented_selector_path_case change-dir-readme-parent fail \
  go/internal/parser/rust/README.md \
  'go -C go/internal/parser test . --test.run=TestDefaultEngineParsePathCargo -count=1'
documented_selector_path_case change-dir-readme-child pass \
  go/internal/parser/rust/README.md \
  'go test -C=go/internal/parser/rust . -test.run TestDefaultEngineParsePathCargo -count=1'

# Every black-box Engine test moved into external rust_test ownership must be
# guarded, not only the Cargo subset.
documented_selector_case relocated-lifetime-parent fail \
  'go test ./internal/parser -run '\''^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' -count=1'
documented_selector_case relocated-lifetime-child pass \
  'go test ./internal/parser/rust -run '\''^TestDefaultEngineParsePathRustCapturesImplLifetimes$'\'' -count=1'
documented_selector_case relocated-module-parent fail \
  'go test ./internal/parser -run '\''^TestDefaultEngineParsePathRustAnnotatesResolvedModules$'\'' -count=1'
documented_selector_case relocated-route-parent fail \
  'go test ./internal/parser -run '\''^TestDefaultEngineParsePathRustEmitsExactFrameworkRouteEntries$'\'' -count=1'
documented_selector_case relocated-route-mixed pass \
  'go test ./internal/parser ./internal/parser/rust -run '\''^TestDefaultEngineParsePathRustSkipsNonExactFrameworkRoutes$'\'' -count=1'
documented_selector_case double-quoted-relocated-parent fail \
  'go test ./internal/parser -run "^TestDefaultEngineParsePathRustCapturesImplLifetimes$" -count=1'
documented_selector_case double-quoted-relocated-mixed pass \
  'go test ./internal/parser ./internal/parser/rust -run "^TestDefaultEngineParsePathCargoTomlEmitsDependencyEvidence$" -count=1'
documented_selector_case double-quoted-internal-rust-parent pass \
  'go test ./internal/parser -run "^TestParseCargoCfgManifest$" -count=1'
documented_selector_case double-quoted-variable-selector fail \
  'go test ./internal/parser -run "^${RUST_SELECTOR}$" -count=1'
documented_selector_case dynamic-parent-package fail \
  'go test "$RUST_PACKAGE" -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_case dynamic-package-before-literal-child fail \
  'go test "$RUST_PACKAGE" ./internal/parser/rust -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_case literal-child-before-dynamic-package fail \
  'go test -run TestDefaultEngineParsePathRustCapturesImplLifetimes ./internal/parser/rust "$RUST_PACKAGE" -count=1'
documented_selector_case dynamic-package-after-selector fail \
  'go test ./internal/parser ./internal/parser/rust -run TestDefaultEngineParsePathRustCapturesImplLifetimes "$RUST_PACKAGE" -count=1'
documented_selector_case dynamic-package-can-inject-selector fail \
  'go test ./internal/parser -run "^Other$" "$RUST_PACKAGE" -count=1'
documented_selector_case dynamic-test-args-can-inject-selector fail \
  'go test ./internal/parser -args -test.run Other $TEST_ARGS'
documented_selector_case dynamic-value-can-inject-selector fail \
  'go test ./internal/parser -run Other -count $COUNT_ARGS'
documented_selector_case dynamic-args-boundary-before-child fail \
  'go test ./internal/parser -run '\''^$'\'' "$RUST_PACKAGE" ./internal/parser/rust -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_case glob-parent-package fail \
  'go test ./internal/par* -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_case brace-parent-package fail \
  'go test ./internal/{parser,parser} -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_case quoted-glob-package pass \
  'go test "./internal/par*" -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_case glob-package-before-literal-child fail \
  'go test ./internal/par* ./internal/parser/rust -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_case parent-recursive-pattern pass \
  'go test ./internal/parser/... -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_case mixed-recursive-child-pattern pass \
  'go test ./internal/parser ./internal/parser/rust/... -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_case module-recursive-parent-pattern pass \
  'go test github.com/eshu-hq/eshu/go/internal/parser/... -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_case arbitrary-parent-pattern fail \
  'go test ./.../parser -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_case arbitrary-pattern-with-literal-child pass \
  'go test ./.../parser ./internal/parser/rust -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_case change-dir-arbitrary-parent-pattern fail \
  'go test -C go ./.../parser -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_case arbitrary-parent-pattern-dynamic-selector fail \
  'go test ./.../parser -run Other -count $COUNT_ARGS'
documented_selector_case arbitrary-pattern-dynamic-selector-with-child fail \
  'go test ./.../parser ./internal/parser/rust -run Other -count $COUNT_ARGS'
documented_selector_case buildvcs-bare-mixed pass \
  'go test -buildvcs ./internal/parser ./internal/parser/rust -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'
documented_selector_case buildvcs-equals-mixed pass \
  'go test -buildvcs=false ./internal/parser ./internal/parser/rust -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1'

# Point discovery at a synthetic Rust suite. New external black-box tests must
# be protected automatically, while same-package implementation tests remain
# outside this parent-Engine documentation guard.
rust_inventory_root="${tmp_root}/rust-inventory"
mkdir -p "${rust_inventory_root}/go/internal/parser/rust"
printf '%s\n' \
  'package rust_test' \
  'import "testing"' \
  'func Test(t *testing.T) {}' \
  'func Testfoo(t *testing.T) {}' \
  'func TestMain(m *testing.M) {}' \
  'type inventoryReceiver struct{}' \
  'func (inventoryReceiver) TestMethod(t *testing.T) {}' \
  'func TestIdentifierT(t *T) {}' \
  'func TestEmptyResults(t *testing.T) () {}' \
  'func TestDefaultEngineParsePathCargoExtra(t *testing.T) {}' \
  'func TestDefaultEngineParsePathRustFutureBehavior(t *testing.T) {}' \
  >"${rust_inventory_root}/go/internal/parser/rust/future_engine_test.go"
printf '%s\n' \
  'package rust' \
  'import "testing"' \
  'func TestUnrelatedRustBehavior(t *testing.T) {}' \
  >"${rust_inventory_root}/go/internal/parser/rust/future_internal_test.go"
printf '%s\n' \
  'package rust_test' \
  'import "testing"' \
  'func TestIgnoredDotFile(t *testing.T) {}' \
  >"${rust_inventory_root}/go/internal/parser/rust/._test.go"
printf '%s\n' \
  'package rust_test' \
  'import "testing"' \
  'func TestIgnoredUnderscoreFile(t *testing.T) {}' \
  >"${rust_inventory_root}/go/internal/parser/rust/_hidden_test.go"
printf '%s\n' \
  '//go:build plan9' \
  '' \
  'package rust_test' \
  'import "testing"' \
  'func TestCrossPlatformGuard(t *testing.T) {}' \
  >"${rust_inventory_root}/go/internal/parser/rust/future_plan9_test.go"
PARSER_RELOCATED_RUST_TEST_SOURCE_ROOT="${rust_inventory_root}"
PARSER_RELOCATED_RUST_TEST_NAMES=()
PARSER_SELECTOR_CACHE_KEYS=()
PARSER_SELECTOR_CACHE_RESULTS=()
documented_selector_case derived-future-cargo-parent fail \
  'go test ./internal/parser -run '\''^TestDefaultEngineParsePathCargoExtra$'\'' -count=1'
documented_selector_case derived-exact-test-name-parent fail \
  'go test ./internal/parser -run '\''^Test$'\'' -count=1'
documented_selector_case derived-identifier-t-parent fail \
  'go test ./internal/parser -run '\''^TestIdentifierT$'\'' -count=1'
documented_selector_case derived-empty-results-parent fail \
  'go test ./internal/parser -run '\''^TestEmptyResults$'\'' -count=1'
documented_selector_case derived-lowercase-test-name-parent pass \
  'go test ./internal/parser -run '\''^Testfoo$'\'' -count=1'
documented_selector_case derived-test-main-parent pass \
  'go test ./internal/parser -run '\''^TestMain$'\'' -count=1'
documented_selector_case derived-method-parent pass \
  'go test ./internal/parser -run '\''^TestMethod$'\'' -count=1'
documented_selector_case derived-ignored-dot-file-parent pass \
  'go test ./internal/parser -run '\''^TestIgnoredDotFile$'\'' -count=1'
documented_selector_case derived-ignored-underscore-file-parent pass \
  'go test ./internal/parser -run '\''^TestIgnoredUnderscoreFile$'\'' -count=1'
documented_selector_case derived-cross-platform-parent fail \
  'go test ./internal/parser -run '\''^TestCrossPlatformGuard$'\'' -count=1'
documented_selector_case derived-future-engine-parent fail \
  'go test ./internal/parser -run '\''^TestDefaultEngineParsePathRustFutureBehavior$'\'' -count=1'
documented_selector_case derived-future-engine-child pass \
  'go test ./internal/parser/rust -run '\''^TestDefaultEngineParsePathRustFutureBehavior$'\'' -count=1'
documented_selector_case derived-internal-rust-parent pass \
  'go test ./internal/parser -run '\''^TestUnrelatedRustBehavior$'\'' -count=1'

empty_inventory_root="${tmp_root}/empty-rust-inventory"
mkdir -p "${empty_inventory_root}/go/internal/parser/rust"
PARSER_RELOCATED_RUST_TEST_SOURCE_ROOT="${empty_inventory_root}"
PARSER_RELOCATED_RUST_TEST_NAMES=()
if load_documented_relocated_rust_test_names >/dev/null 2>&1; then
  printf 'empty relocated Rust test inventory unexpectedly passed\n' >&2
  exit 1
fi

malformed_inventory_root="${tmp_root}/malformed-rust-inventory"
mkdir -p "${malformed_inventory_root}/go/internal/parser/rust"
printf '%s\n' 'package rust_test' 'func TestBroken(' \
  >"${malformed_inventory_root}/go/internal/parser/rust/broken_test.go"
PARSER_RELOCATED_RUST_TEST_SOURCE_ROOT="${malformed_inventory_root}"
PARSER_RELOCATED_RUST_TEST_NAMES=()
if load_documented_relocated_rust_test_names >/dev/null 2>&1; then
  printf 'malformed relocated Rust test inventory unexpectedly passed\n' >&2
  exit 1
fi

generic_inventory_root="${tmp_root}/generic-rust-inventory"
mkdir -p "${generic_inventory_root}/go/internal/parser/rust"
printf '%s\n' \
  'package rust_test' \
  'import "testing"' \
  'func TestGeneric[T any](t *testing.T) {}' \
  >"${generic_inventory_root}/go/internal/parser/rust/generic_test.go"
PARSER_RELOCATED_RUST_TEST_SOURCE_ROOT="${generic_inventory_root}"
PARSER_RELOCATED_RUST_TEST_NAMES=()
if load_documented_relocated_rust_test_names >/dev/null 2>&1; then
  printf 'generic relocated Rust test inventory unexpectedly passed\n' >&2
  exit 1
fi

no_match_root="${tmp_root}/no-match-docs"
mkdir -p "${no_match_root}/docs" "${no_match_root}/go/internal/parser"
PARSER_RELOCATED_RUST_TEST_SOURCE_ROOT="${repo_root}"
PARSER_RELOCATED_RUST_TEST_NAMES=()
PARSER_SELECTOR_TEST_BINARY=''
PARSER_SELECTOR_MATCHER_OUTPUT="${tmp_root}/no-match-selector"
validate_documented_parser_test_commands "${no_match_root}"
if [ -e "$PARSER_SELECTOR_MATCHER_OUTPUT" ]; then
  printf 'documented selector matcher leaked after an empty scan\n' >&2
  exit 1
fi
PARSER_SELECTOR_MATCHER_OUTPUT=''

multi_match_root="${tmp_root}/multiple-command-match"
mkdir -p "${multi_match_root}/docs" "${multi_match_root}/go/internal/parser"
printf '%s\n' \
  '`go test ./internal/parser/rust -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1` then `go test ./internal/parser -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1`' \
  >"${multi_match_root}/docs/commands.md"
PARSER_RELOCATED_RUST_TEST_SOURCE_ROOT="${repo_root}"
PARSER_RELOCATED_RUST_TEST_NAMES=()
if validate_documented_parser_test_commands "${multi_match_root}" >/dev/null 2>&1; then
  printf 'second stale parser command on one line unexpectedly passed\n' >&2
  exit 1
fi
printf '%s\n' \
  '`go test ./internal/parser -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1` then `go test ./internal/parser/rust -run TestDefaultEngineParsePathRustCapturesImplLifetimes -count=1`' \
  >"${multi_match_root}/docs/commands.md"
PARSER_RELOCATED_RUST_TEST_NAMES=()
if validate_documented_parser_test_commands "${multi_match_root}" >/dev/null 2>&1; then
  printf 'first stale parser command on one line unexpectedly passed\n' >&2
  exit 1
fi

PARSER_RELOCATED_RUST_TEST_SOURCE_ROOT="${repo_root}"
PARSER_RELOCATED_RUST_TEST_NAMES=()
PARSER_SELECTOR_CACHE_KEYS=()
PARSER_SELECTOR_CACHE_RESULTS=()
load_documented_relocated_rust_test_names
for relocated_test_name in "${PARSER_RELOCATED_RUST_TEST_NAMES[@]}"; do
  documented_selector_case "inventory-${relocated_test_name}-parent" fail \
    "go test ./internal/parser -run '^${relocated_test_name}$' -count=1"
  documented_selector_case "inventory-${relocated_test_name}-child" pass \
    "go test ./internal/parser/rust -run '^${relocated_test_name}$' -count=1"
done
