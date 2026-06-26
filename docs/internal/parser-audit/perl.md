# Perl Parser Audit

## Overview
The Perl parser (`go/internal/parser/perl/`) is a tree-sitter-backed adapter that extracts packages (as classes), `use` imports, subroutine declarations, variables, function calls, and bounded dead-code root metadata. It operates on a single-pass AST walk via `tree_sitter_syntax.go`, with a shared McCabe complexity walker. The package has one subdirectory test file with 5 test functions, plus 2 parent-level cyclomatic complexity test cases and 2 long-tail fixture references.

## Claimed Constructs
List every construct the parser claims to extract, with source references.

1. **Classes** (package statements) — `tree_sitter_syntax.go:59-73` (`package_statement`)
2. **Imports** (use statements) — `tree_sitter_syntax.go:74-81` (`use_statement`)
3. **Functions** (subroutine declarations) — `tree_sitter_syntax.go:100-116` (`subroutine_declaration_statement`)
4. **Phaser blocks** (BEGIN/UNITCHECK/CHECK/INIT/END) — `tree_sitter_syntax.go:84-99` (`phaser_statement`)
5. **Variables** — `tree_sitter_syntax.go:117-118` (`variable_declaration`)
6. **Function calls** — `tree_sitter_syntax.go:119-122` (`function_call_expression`, `ambiguous_function_call_expression`, `func0op_call_expression`, `func1op_call_expression`, `method_call_expression`)
7. **Dead-code root kinds** (`parser.go:109-129`, `tree_sitter_syntax.go:114`):
   - `perl.package_namespace` — public packages (`parser.go:154-159`, `tree_sitter_syntax.go:69-71`)
   - `perl.script_entrypoint` — `sub main` in `.pl`/`.t` files (`parser.go:111-113`)
   - `perl.constructor` — `sub new` in package context (`parser.go:119-121`)
   - `perl.exported_subroutine` — subs named in `@EXPORT`/`@EXPORT_OK` (`parser.go:92-99`, `tree_sitter_syntax.go:132-151`)
   - `perl.special_block` — phaser blocks (`tree_sitter_syntax.go:92`)
   - `perl.autoload_subroutine` — `sub AUTOLOAD` (`parser.go:123-124`)
   - `perl.destroy_subroutine` — `sub DESTROY` (`parser.go:125-126`)
8. **Cyclomatic complexity** — `complexity.go:38-39`
9. **PreScan** — `parser.go:181-198`
10. **IndexSource** — `parser.go:47-49`

## Verified-by-Test Constructs
List constructs verified by tests, with file:function references.

1. **Classes** — `perl/parser_test.go:30` (`TestParseCapturesPerlBuckets`)
2. **Imports** — `perl/parser_test.go:31` (`TestParseCapturesPerlBuckets`), also `:107-108`
3. **Functions (subroutines)** — `perl/parser_test.go:32-34` (`TestParseCapturesPerlBuckets`)
4. **Function source span (IndexSource)** — `perl/parser_test.go:33`
5. **Variables** — `perl/parser_test.go:36` (`TestParseCapturesPerlBuckets`)
6. **Function calls** — `perl/parser_test.go:37-38` (`TestParseCapturesPerlBuckets`)
7. **Line/end_line metadata** — `perl/parser_test.go:57-60` (`TestParseCapturesPerlSubroutineFromTreeSitterSpan`)
8. **`perl.package_namespace`** — `perl/parser_test.go:109` (`TestParseMarksPerlDeadCodeRoots`)
9. **`perl.constructor`** — `perl/parser_test.go:110`
10. **`perl.script_entrypoint`** — `perl/parser_test.go:111`
11. **`perl.exported_subroutine`** — `perl/parser_test.go:112-114`
12. **`perl.special_block`** — `perl/parser_test.go:115`
13. **`perl.autoload_subroutine`** — `perl/parser_test.go:116`
14. **`perl.destroy_subroutine`** — `perl/parser_test.go:117`
15. **Package-scoped exporter roots** — `perl/parser_test.go:124-152` (`TestParseKeepsExporterRootsPackageScoped`)
16. **PreScan includes full names** — `perl/parser_test.go:154-171` (`TestPreScanIncludesFullPerlPackageNames`)
17. **Cyclomatic complexity** — `engine_cyclomatic_complexity_test.go:199-211` (`straight_line` and `branches_and_boolean`)
18. **Long-tail comprehensive fixture parsing** — `engine_long_tail_test.go:276-301` (2 tests referencing `perl_comprehensive`)

## Unverified / Claimed-but-Untested Constructs
List constructs claimed but not covered by any test.

1. **Dollar-sigil variable filtering** (`$`, `@`, `%`, `&` stripping in `parser.go:83`): no test verifies that `$name` becomes `name`.
2. **`func0op_call_expression` and `func1op_call_expression` call AST nodes** (`tree_sitter_syntax.go:119`): tests only exercise `function_call_expression` and `ambiguous_function_call_expression`. No file call op variants tested.
3. **Class `full_name` and `end_line` fields**: tested only for functions, not classes.
4. **Phaser blocks inside non-package scope** (module-level phasers): all tests place phasers inside a package.
5. **Non-`.pl`/`.t` path `script_entrypoint`** (`parser.go:148-151`): never tested with `.pm` path and `sub main`.
6. **Variable deduplication by name** (`tree_sitter_syntax.go:162-163`): never tested with duplicate variable names.
7. **Call deduplication by name** (`tree_sitter_syntax.go:179-181`): never tested with duplicate calls.
8. **`PerlFunctionKey` with empty package** (`parser.go:102-105`): no test for subroutines at file scope (no package).
9. **`perl.exported_subroutine` with `@EXPORT`** (not `@EXPORT_OK`): tested indirectly via `default_action`, but not isolated.
10. **Edge case: PUBLIC package check** (`parser.go:154-159`): not tested with underscore-prefix package name.

## Edge Cases Considered
List edge cases the tests actually cover with test references.

- **Multiline subroutine declaration** (name on line after `sub`) — `perl/parser_test.go:41-67` (`TestParseCapturesPerlSubroutineFromTreeSitterSpan`)
- **Multi-package file scope** (shared function name across packages) — `perl/parser_test.go:124-152` (`TestParseKeepsExporterRootsPackageScoped`)
- **Private helper not marked as dead-code root** — `perl/parser_test.go:118-121` (`TestParseMarksPerlDeadCodeRoots`)
- **IndexSource preserves tree-sitter span** — `perl/parser_test.go:33` and `:63-64`
- **PreScan includes both short name and fully qualified name** — `perl/parser_test.go:154-171` (`TestPreScanIncludesFullPerlPackageNames`)

## Edge Cases NOT Considered
List edge cases not tested.

- **Empty source file**
- **Non-ASCII subroutine names**
- **Nested packages**
- **Exporter with empty @EXPORT**
- **Multiple phaser blocks in one package**
- **A subroutine that is BOTH `new` and exported** (root kind merging)
- **Calls spanning multiple expression kinds simultaneously**
- **Phaser names beyond the 5 known (BEGIN/CHECK/INIT/UNITCHECK/END)**
- **`method_call_expression` with dotted method name**

## Verdict
moderate

The core payload buckets and all 7 dead-code root kinds are verified by focused tests. The single test file provides end-to-end coverage for the primary AST walk and PreScan. However, coverage is thin: only 5 test functions total, with no tests for the `func0op`/`func1op`/`method` call expression variants, variable deduplication, or call deduplication. The complexity walker is tested only at the parent level.

## Recommended Actions
1. Add a test for `func0op_call_expression` and `method_call_expression` call node parsing.
2. Add a test for dollar-sigil stripping in variable names.
3. Add a test for `PerlFunctionKey` at file scope (no package).
4. Add a test for variable and call deduplication.
5. Add a test for underscore-prefix package names not receiving `perl.package_namespace`.
