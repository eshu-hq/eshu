# Haskell Parser Audit

## Overview
The Haskell parser is a fully tree-sitter-AST-based adapter (migrated from
line-scan regex in issue #3588). A single `haskellExtractor` walk extracts
module declarations (with export lists), imports with common aliases,
data/newtype/type declarations (with `semantic_kind`), typeclass and instance
methods, top-level bindings, and where-block local variables. Function-call
evidence is bounded lexical extraction from binding right-hand-side text (the
documented permanent exception). Dead-code root metadata covers explicit module
exports, `main`, typeclass methods, and instance methods. Cyclomatic complexity
counts McCabe decision points plus pattern-match equation dispatch. The parser
has golden characterization fixtures for byte-parity regression detection.

## Claimed Constructs
- **Modules** from module header with dotted name reconstruction
  (`ast_nodes.go:40-55`, `ast_extract.go:98-116`)
- **Explicit export names** from module header export list
  (`ast_nodes.go:61-84`, `ast_extract.go:98-116`)
- **Imports** with dotted module name and optional `as` alias
  (`ast_nodes.go:88-99`, `ast_extract.go:120-139`)
- **Data/newtype/type declarations** as classes bucket rows with `semantic_kind`
  (data, newtype, type) (`ast_nodes.go:113-131`, `ast_extract.go:165-181`)
- **Typeclass declarations** as classes bucket rows with `semantic_kind: typeclass`
  (`ast_extract.go:185-206`)
- **Instance declarations** — methods recorded with instance context, no instance
  row itself (`ast_extract.go:211-217`)
- **Typeclass method signatures** as function rows with `class_context` and
  `haskell.typeclass_method` root (`ast_extract.go:222-246`)
- **Top-level function bindings** from `bind` and `function` nodes with class/
  instance context and dead-code roots (`ast_extract.go:251-269`)
- **Where-block local variables** in the variables bucket, never top-level
  functions (`ast_extract.go:335-360`)
- **Function-call evidence** from binding right-hand-sides via
  `haskellCallTokenPattern` regex (`helpers.go:18-128`)
- **Dead-code roots**: `haskell.module_export`, `haskell.main_function`,
  `haskell.typeclass_method`, `haskell.instance_method`, `haskell.exported_type`
  (`helpers.go:30-45`, `ast_extract.go`)
- **Cyclomatic complexity**: McCabe plus equation-dispatch points, with
  catch-all guard/alternative exclusion (`complexity.go:31-61`)
- **Function `source` field**: excludes trailing where-block (`ast_extract.go:276-294`)
- **PreScan** names from functions, classes, modules buckets
  (`parser.go:63-71`)

## Verified-by-Test Constructs
- Modules with name and line numbers:
  `parser_test.go:TestParseCapturesHaskellBuckets` (line 30),
  `parser_test.go:TestParseCapturesHaskellDeadCodeRootsAndCalls` (line 72)
- Imports with alias (`qualified ... as T`):
  `parser_test.go:TestParseCapturesHaskellDeadCodeRootsAndCalls` (line 73)
- Classes with `semantic_kind` (data/newtype/type):
  `ast_extract_test.go:TestParseCapturesDataNewtypeTypeKinds` (line 76)
- Typeclass with `semantic_kind: typeclass`:
  `parser_test.go:TestParseCapturesHaskellDeadCodeRootsAndCalls` (line 78)
- Typeclass method with `class_context` and `haskell.typeclass_method` root:
  `parser_test.go:TestParseCapturesHaskellDeadCodeRootsAndCalls` (line 83),
  `ast_extract_test.go:TestParseCapturesMultiLineTypeSignatureClassMethod` (line 23)
- Instance method with combined context and `haskell.instance_method` root:
  `parser_test.go:TestParseCapturesHaskellDeadCodeRootsAndCalls` (line 89)
- Top-level functions with dead-code roots:
  `parser_test.go:TestParseCapturesHaskellDeadCodeRootsAndCalls` (line 79-80)
- Exported type dead-code root (`haskell.exported_type`):
  `ast_extract_test.go:TestParseCapturesTypeExportInModuleHeader` (line 68),
  `parser_test.go:TestParseCapturesHaskellDeadCodeRootsAndCalls` (line 77)
- Module export root (`haskell.module_export`):
  `ast_extract_test.go:TestParseCapturesTypeExportInModuleHeader` (line 70)
- Main function root (`haskell.main_function`):
  `parser_test.go:TestParseCapturesHaskellDeadCodeRootsAndCalls` (line 79)
- Where-block local variables in variables bucket, excluded from functions:
  `parser_test.go:TestParseKeepsHaskellLocalBindingsOutOfFunctionBucket` (line 174),
  `parser_test.go:TestParseKeepsMultilineLocalBindingsOutOfFunctionBucket` (line 208)
- Function `source` field excludes where block:
  `parser_test.go:TestParseCapturesHaskellBuckets` (line 34)
- Guarded/multi-clause function bindings with correct line spans:
  `parser_test.go:TestParseCapturesHaskellGuardedFunctionBinding` (line 141)
- Continuation-style call extraction from do-blocks:
  `parser_test.go:TestParseCapturesHaskellContinuationCalls` (line 101)
- Parameter suppression in function calls:
  `parser_test.go:TestParseSuppressesHaskellTreeFunctionParameterCalls` (line 244)
- Pattern wrapper parameters (e.g., `(Just value)`):
  `ast_extract_test.go:TestHaskellTreeFunctionParametersReadPatternWrappers` (line 103)
- Multi-line typeclass signature (name and `::` on different lines):
  `ast_extract_test.go:TestParseCapturesMultiLineTypeSignatureClassMethod` (line 23)
- Golden characterization fixtures for byte-parity regression:
  `characterization_test.go:TestHaskellPayloadCharacterization` (line 122)
- Comprehensive corpus via Engine:
  `engine_long_tail_test.go:TestDefaultEngineParsePathHaskellFixtures` (line 319)
- Cyclomatic complexity:
  `engine_cyclomatic_complexity_test.go:TestCyclomaticComplexityPerLanguage` (Haskell cases at line 289)
- Grammar loading via Runtime.Parser:
  `runtime_test.go:TestRuntimeParserLoadsHaskellGrammar` (line 61)

## Unverified / Claimed-but-Untested Constructs
- **PreScanWithParser** (`parser.go:74-82`): no dedicated test; only PreScan is
  tested.
- **Cyclomatic complexity on Haskell functions** directly: the
  `applyComplexity` method and equation-counting logic are exercised only through
  characterization goldens. No subdirectory test asserts a specific complexity
  value (e.g., complexity=3 for a function with 2 equations + 1 guard).
- **Error path: nil parser** (`parser.go:37-39`, `ParseWithParser`): no
  direct test provides a nil parser.
- **Error path: nil tree** (`parser.go:40-44`): no test causes the parser to
  return a nil tree.
- **`haskellIsKeyword`** complete list: the 20 Haskell keywords are used in
  filtering, but no test verifies each keyword is suppressed from
  functions/variables/calls buckets.
- **`haskellStripStringsAndLineComment`** directly: tested indirectly through
  call extraction but no unit test for the string-stripping logic itself.
- **`haskellIdentifierByte`** helper: tested indirectly.
- **`haskellCallTokenPattern` regex** independently: no characterization test
  that pins which token forms the regex matches and rejects without involving
  full Parse.

## Edge Cases Considered
- Multi-line type signature where name and `::` are on separate lines (AST
  walks the `signature` node directly):
  `ast_extract_test.go:TestParseCapturesMultiLineTypeSignatureClassMethod`
- Pattern wrapper parameters like `(Just value)` — descends to inner bound
  variable: `ast_extract_test.go:TestHaskellTreeFunctionParametersReadPatternWrappers`
- Let-bound local variables are excluded from function bucket:
  `parser_test.go:TestParseKeepsHaskellLocalBindingsOutOfFunctionBucket`
- Multiline let/where locals excluded:
  `parser_test.go:TestParseKeepsMultilineLocalBindingsOutOfFunctionBucket`
- Function parameters are not reported as call targets:
  `parser_test.go:TestParseSuppressesHaskellTreeFunctionParameterCalls`
- Guard clauses with `otherwise` and case alternatives with bare `_` wildcard
  are excluded from McCabe counts:
  `complexity.go:65-88` (code logic, indirectly tested via characterization goldens)
- Multi-equation function bindings — end_line spans the full set of clauses:
  `parser_test.go:TestParseCapturesHaskellGuardedFunctionBinding`
- Continuation-style do-block calls:
  `parser_test.go:TestParseCapturesHaskellContinuationCalls`
- Byte-parity golden fixtures prevent regression across a representative corpus:
  `characterization_test.go`
- Comprehensive corpus tests at engine level:
  `engine_long_tail_test.go`

## Edge Cases NOT Considered
- **Empty Haskell file**: no test for a zero-byte `.hs` file
- **Haskell file with syntax errors**: parser behavior on unparseable Haskell
- **Operator-defined functions** (e.g., `(+++)`, `(.@)`): no test for
  operator-named bindings
- **Type families or GADTs**: only data/newtype/type handled; more advanced
  type declarations not tested
- **Deriving clauses**: not tested
- **Qualified module names with re-exports**: `module Foo (module Bar)` style
  not tested
- **Nested where blocks**: only single-level where blocks tested
- **Comments inside function bodies**: no test confirming comments don't produce
  spurious calls
- **`{-# LANGUAGE` pragmas**: no test for pragma handling
- **All 20 Haskell keywords**: no exhaustive suppression test
- **Instance with multi-parameter typeclass** (e.g., `instance Show Worker`):
  tested with single-param `instance Runner Worker where` but multi-param
  context formatting not verified

## Verdict
**moderate**

The Haskell parser has thorough coverage for its core AST extraction boundaries
(modules, imports, type declarations, class/instance methods, top-level
bindings, where-block variables, dead-code roots) with 10 behavior tests plus
golden characterization fixtures. However, operator-named functions, CPS/RHS
call extraction, advanced type features, error paths, and keyword suppression
are untested, and the complexity calculation has no direct assertion beyond
golden characterization.

## Recommended Actions
1. Add a direct cyclomatic complexity test asserting specific values for a
   known fixture (e.g., a function with 2 pattern-match equations and 1 guard
   should have complexity=3).
2. Add a test for `PreScanWithParser` matching the existing `PreScan` test
   contract.
3. Add error-path tests: nil parser, nil tree, unreadable file, empty file.
4. Add a characterization test for the `haskellCallTokenPattern` regex pinned
   to specific inputs showing match/no-match behavior.
5. Add a test for operator-named function bindings (`(+++)`).
6. Add an exhaustive keyword suppression test for all 20 entries in
   `haskellIsKeyword`.
7. Add a golden fixture exercising multiline RHS continuations with `$`,
   `where`, let-in, and case-of expressions.
