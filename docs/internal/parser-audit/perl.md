# Perl Parser Audit

## Overview

The Perl adapter in `go/internal/parser/perl/` uses tree-sitter to extract
packages as classes, `use` imports, subroutines, variables, calls, bounded
dead-code roots, cyclomatic complexity, and exact route semantics for narrow
Mojolicious::Lite and Dancer forms. Its direct coverage comprises 10
same-package tests and 4 external public Engine tests. Two shared parent tests
cover the comprehensive fixture and two shared complexity table cases cover
straight-line and branching subroutines.

## Claimed Constructs

1. **Classes** from package statements —
   `tree_sitter_syntax.go:(*perlSyntaxIndex).collect`, case
   `package_statement`.
2. **Imports** from `use` statements —
   `tree_sitter_syntax.go:(*perlSyntaxIndex).collect`, case `use_statement`.
3. **Functions** from subroutine declarations —
   `tree_sitter_syntax.go:(*perlSyntaxIndex).collect`, case
   `subroutine_declaration_statement`.
4. **Phaser blocks** for `BEGIN`, `UNITCHECK`, `CHECK`, `INIT`, and `END` —
   `tree_sitter_syntax.go:perlPhaserName`.
5. **Variables** from variable declarations —
   `tree_sitter_syntax.go:(*perlSyntaxIndex).appendVariables`.
6. **Function and method calls** —
   `tree_sitter_syntax.go:(*perlSyntaxIndex).collect` and `perlCallName`.
   Recognized node kinds are `function_call_expression`,
   `ambiguous_function_call_expression`, `func0op_call_expression`,
   `func1op_call_expression`, and `method_call_expression`.
7. **Dead-code root kinds** — `parser.go:perlFunctionRootKinds` and
   `tree_sitter_syntax.go:(*perlSyntaxIndex).collect`:
   - `perl.package_namespace` for public packages.
   - `perl.script_entrypoint` for `sub main` in `.pl` and `.t` files.
   - `perl.constructor` for package-scoped `sub new`.
   - `perl.exported_subroutine` for names in `@EXPORT` or `@EXPORT_OK`.
   - `perl.special_block` for recognized phaser blocks.
   - `perl.autoload_subroutine` for `sub AUTOLOAD`.
   - `perl.destroy_subroutine` for `sub DESTROY`.
8. **Cyclomatic complexity** — `complexity.go:perlCyclomaticComplexity`.
9. **Exact framework route semantics** for one active Mojolicious::Lite or
   Dancer/Dancer2 import family, literal paths, concrete HTTP verbs, and named
   code-reference handlers — `framework_routes.go:buildPerlFrameworkSemantics`
   and `perlExactRouteCall`.
10. **PreScan** short and fully qualified names — `parser.go:PreScan`.
11. **IndexSource** subroutine text — `parser.go:ParseWithParser`.

## Verified-by-Test Constructs

1. **Core package payload**: classes, imports, functions, source spans,
   variables, and calls —
   `perl/parser_test.go:TestParseCapturesPerlBuckets`.
2. **Multiline subroutine position and source** —
   `perl/parser_test.go:TestParseCapturesPerlSubroutineFromTreeSitterSpan`.
3. **All seven dead-code root kinds, including both `@EXPORT` and
   `@EXPORT_OK`** — `perl/parser_test.go:TestParseMarksPerlDeadCodeRoots`.
4. **Exporter roots stay package-scoped** —
   `perl/parser_test.go:TestParseKeepsExporterRootsPackageScoped`.
5. **Scalar, array, and hash variable names** —
   `perl/parser_test.go:TestParsePerlVariableExtraction`.
6. **Exporter root selection and an unrooted helper** —
   `perl/parser_test.go:TestParsePerlSubroutineWithDeadCodeRoots`.
7. **Method and ordinary function calls** —
   `perl/parser_test.go:TestParsePerlCallExpressionVariants`. The fixture's
   tree contains `method_call_expression` and `function_call_expression`
   nodes; it does not exercise the `func0op` or `func1op` node kinds.
8. **Multiple import forms** —
   `perl/parser_test.go:TestParsePerlUseImportExtraction`.
9. **Empty source returns empty entity buckets** —
   `perl/parser_test.go:TestParsePerlEmptyFile`.
10. **PreScan includes fully qualified package and subroutine names** —
    `perl/parser_test.go:TestPreScanIncludesFullPerlPackageNames`.
11. **Public Engine dispatch and core payload shape** —
    `perl/engine_perl_test.go:TestDefaultEngineParsePathPerlBasic`.
12. **Exact Mojolicious and Dancer routes**: double- and single-quoted literal
    paths, parenthesized calls, GET/POST/DELETE verbs, and route entries —
    `perl/engine_perl_route_semantics_test.go:TestDefaultEngineParsePathPerlExactFrameworkRouteEntries`.
13. **Qualified route handlers** —
    `perl/engine_perl_route_semantics_test.go:TestDefaultEngineParsePathPerlPreservesQualifiedRouteHandlers`.
14. **Unclaimed non-exact routes**: dynamic paths, inline subs, controller
    strings, `any`, wrapper calls, and a file importing both DSL families —
    `perl/engine_perl_route_semantics_test.go:TestDefaultEngineParsePathPerlSkipsNonExactFrameworkRoutes`.
15. **Comprehensive Engine fixture**: packages, functions, imports, calls, and
    variables — `engine_long_tail_test.go:TestDefaultEngineParsePathPerlFixtures`
    and `TestDefaultEngineParsePathPerlCallsAndVariables`.
16. **Cyclomatic complexity** for straight-line and branching subroutines —
    `engine_cyclomatic_complexity_test.go:TestCyclomaticComplexityPerLanguage`,
    cases `perl_straight_line` and `perl_branches_and_boolean`.

## Unverified / Claimed-but-Untested Constructs

1. **Sigil-prefixed Exporter entries**: `perlCollectExportNames` strips
   `$`, `@`, `%`, and `&`, but no test uses a value such as `qw(&handler)`.
2. **`func0op_call_expression` and `func1op_call_expression`**: the call
   variants test reaches method and ordinary function nodes only.
3. **Class `full_name` and `end_line` fields**: class names are asserted, but
   these fields are not checked directly.
4. **File-scope phaser context**: tested phasers follow a package declaration.
5. **`.pm` `sub main` is not a script entrypoint**: only positive `.pl`
   behavior is asserted.
6. **Variable and call deduplication** for repeated names.
7. **File-scope function keys**: complexity cases parse file-scope
   subroutines, but do not assert key-collision behavior.
8. **Private package namespace exclusion** for a package segment beginning
   with an underscore.
9. **Nil parser and unreadable-file error paths** in `ParseWithParser`.

## Edge Cases Considered

- Empty input.
- A multiline subroutine declaration and its source span.
- Multiple package declarations with the same subroutine name.
- A private helper that receives no dead-code root.
- Both `@EXPORT` and `@EXPORT_OK` declarations.
- Method calls and ordinary function calls.
- Multiple import forms.
- Qualified route handlers.
- Single- and double-quoted route paths and parenthesized route calls.
- Conservative rejection of ambiguous or dynamic route forms.

## Edge Cases NOT Considered

- Non-ASCII subroutine names.
- Nested or reopened packages beyond the covered two-package fixture.
- Empty Exporter lists and sigil-prefixed exported names.
- Multiple phaser blocks and file-scope phasers.
- A function receiving more than one dead-code root kind.
- Repeated variables or calls that should deduplicate.
- Nil parser and unreadable source errors.
- Private package namespace exclusion.

## Verdict

moderate

The primary payload, all seven dead-code root kinds, public Engine dispatch,
exact supported route forms, and conservative route rejection have focused
coverage. Coverage remains moderate because two recognized call node kinds,
deduplication, private-package exclusion, and parser error paths are not
directly exercised. Shared parent tests still own comprehensive fixture and
cross-language complexity coverage.

## Recommended Actions

1. Add fixtures that prove `func0op_call_expression` and
   `func1op_call_expression` extraction.
2. Add a sigil-prefixed Exporter fixture such as `qw(&handler)`.
3. Add duplicate variable and call cases.
4. Add a private package namespace exclusion test.
5. Add nil-parser and unreadable-file error tests for `ParseWithParser`.
