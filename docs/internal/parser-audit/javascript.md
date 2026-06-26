# JavaScript Parser Audit

## Overview
The JavaScript parser is the largest and most complex language adapter in Eshu,
covering JavaScript, TypeScript, and TSX. After the regex-to-AST migration
(#3539/#3563), all primary symbol, edge, and framework-metadata extraction is
tree-sitter AST node-walking. Only three within-string-content regexes are
retained as documented permanent exceptions (computed-property validation, AWS
service slug, GCP service slug). The parser handles import/require/re-export
rows, tsconfig.json alias resolution, package.json public surface modeling,
React component detection with hooks, Hapi/Express/Fastify/Next.js route
evidence, dead-code root modeling (20+ root kinds), embedded shell commands,
TypeScript type parameters and declaration merging, and opt-in value-flow
analysis. A parent-lookup optimization eliminated per-node cgo crossings
(#3586). The test suite is the largest of any parser: 33
parent-level tests plus 11 subdirectory tests and jsdataflow tests.

## Claimed Constructs
- **Functions**: from function_declaration, generator_function_declaration,
  method_definition with decorators, class_context, dead_code_root_kinds,
  parameter_count, cyclomatic_complexity (`javascript_language.go:80-90`)
- **Classes**: from class_declaration, abstract_class_declaration with
  decorators (TS), type_parameters (TS), implemented_interfaces (TS),
  dead_code_root_kinds (`javascript_language.go:91-113`)
- **Interfaces** (TS): from interface_declaration (`javascript_language.go:115-120`)
- **Type aliases** (TS): from type_alias_declaration
  (`javascript_language.go:121-132`)
- **Enums** (TS): from enum_declaration (`javascript_language.go:133-143`)
- **Components** (React): JSX-returning functions detected via AST
  (`javascript_language.go:83-84`, `javascript_semantics.go`)
- **Variables**: from variable_declaration, lexical_declaration with scope
  gating (`javascript_language.go:156-183`)
- **Imports**: from import_statement, import/require call expressions, with
  resolved_source from tsconfig (`javascript_imports.go`)
- **Re-exports**: from export_statement re-export clauses
  (`javascript_exports.go`)
- **Calls**: from call_expression, new_expression with full_name,
  argument_count, inference (`javascript_language.go:184-199`)
- **Dead-code roots (20+ kinds)**: package_entrypoint, commonjs_default_export,
  commonjs_export, hapi_handler, hapi_route, hapi_plugin_register,
  nextjs_route_export, fastify_route_handler, fastify_route_object_handler,
  framework_callback, typescript_public_api, typescript_interface_method,
  typescript_declaration_public_surface, module_contract_export,
  constructor_function_value, function_value_reference, bin_script,
  main_entrypoint, exports_field, browser_field, package_types_field,
  and more (`javascript_dead_code_roots.go`,
  `javascript_dead_code_commonjs.go`, `javascript_dead_code_framework_exports.go`,
  `javascript_dead_code_framework_routes.go`, `javascript_dead_code_hapi.go`,
  `javascript_dead_code_hapi_route.go`, `javascript_dead_code_hapi_proxy.go`,
  `javascript_dead_code_node_roots.go`, `javascript_dead_code_package.go`,
  `javascript_dead_code_typescript_surface.go`,
  `javascript_dead_code_typescript_import_exports.go`)
- **Hapi routes**: method, path, handler pairs from route config objects
  (`javascript_hapi_routes.go`)
- **Express routes**: server symbols extracted via
  `ExpressServerSymbols` (`javascript_dead_code_roots.go`)
- **Embedded shell commands**: `child_process` exec/spawn calls
  (`embedded_shell.go`)
- **AWS/GCP client services**: service slugs from `@aws-sdk/client-*` or
  `@google-cloud/*` imports (`javascript_semantics_ast.go`)
- **React hooks**: `useState`, `useEffect`, etc. via call_expression callee
  walk (`javascript_semantics_ast.go`)
- **tsconfig.json resolution**: path aliases, baseUrl, rootDir, composite
  project references (`tsconfig.go`)
- **package.json roots**: nearest package ownership, public source targets,
  bin/scripts/main/exports fields (`package_json.go`)
- **TypeScript type parameters**: on classes, interfaces, functions
  (`javascript_type_parameters.go`)
- **TypeScript declaration merging**: namespace + class/function/enum merging
  (`javascript_typescript_declaration_merging.go`)
- **TypeScript public surface re-exports**: static barrel-file walking with
  depth cap (`javascript_dead_code_typescript_surface.go`,
  `javascript_dead_code_typescript_import_exports.go`)
- **CommonJS module aliases**: `module.exports =` patterns
  (`javascript_language.go:73`)
- **Parent lookup optimization**: single-pass child-to-parent map
  (`parent_lookup.go`)
- **Sibling parser**: shared ParserFactory for dead-code file analysis
  (`javascript_sibling_parser.go`)
- **Value-flow (opt-in)**: dataflow_functions, taint_findings,
  interproc_findings via JS CFG lowering (`cfg_emit.go`, `jsdataflow/*`)
- **JSX component detection and method kinds** (getter/setter/async/generator):
  AST-based (`javascript_semantics_ast.go`)
- **PreScan**: declaration names (`javascript_language.go`)

## Verified-by-Test Constructs
The test suite is organized by feature area with approximately 35 parent-level
engine tests and 20+ subdirectory tests. Key coverage categories:

**Core parsing (engine_test.go, engine_managed_oo_test.go)**:
- Basic JS/TS/TSX payload construction: `TestDefaultEngineParsePathJavaScript`
- Annotation metadata and usage kinds

**Imports and re-exports (engine_javascript_*.go)**:
- Static relative re-exports:
  `engine_javascript_reexports_test.go:TestDefaultEngineParsePathJavaScriptStaticRelativeReExports`
- Require imports:
  `engine_javascript_require_test.go:TestDefaultEngineParsePathJavaScriptRequireImports`
- Require template literal interpolation skipped:
  `engine_javascript_require_test.go:TestDefaultEngineParsePathJavaScriptRequireTemplateLiteralInterpolationIsSkipped`

**Call metadata (engine_javascript_call_metadata_test.go)**:
- Chain preservation and JSX call kinds:
  `TestDefaultEngineParsePathJavaScriptCallMetadataPreservesChainsAndJSXKinds`
- Nested functions carry enclosing function:
  `TestDefaultEngineParsePathJavaScriptNestedFunctionsCarryEnclosingFunction`

**Computed properties (engine_javascript_computed_property_test.go)**:
- Static computed member names:
  `TestDefaultEngineParsePathJavaScriptComputedClassMemberNames`
- Concatenation in computed names:
  `TestDefaultEngineParsePathJavaScriptComputedClassMemberConcatenation`
- Runtime-dependent names skipped:
  `TestDefaultEngineParsePathJavaScriptComputedClassMemberRuntimeDependentNameIsSkipped`

**Handler/route detection (engine_javascript_handler_test.go,
engine_javascript_route_handler_test.go)**:
- Hapi binds named handler only:
  `TestDefaultEngineParsePathJavaScriptHapiBindsNamedHandlerOnly`
- Express captures named handler:
  `TestDefaultEngineParsePathJavaScriptExpressCapturesNamedHandler`
- Express duplicate route stays unbound:
  `TestDefaultEngineParsePathJavaScriptExpressDuplicateRouteStaysUnbound`

**Framework semantics (engine_javascript_semantics_test.go)**:
- Framework semantics (AWS/GCP/React):
  `TestDefaultEngineParsePathJavaScriptFrameworkSemantics`
- Hapi route entries preserve method/path pairs:
  `TestDefaultEngineParsePathJavaScriptHapiRouteEntriesPreserveMethodPathPairs`
- Docstrings and method kinds:
  `TestDefaultEngineParsePathJavaScriptDocstringsAndMethodKinds`
- Generator functions:
  `TestDefaultEngineParsePathJavaScriptGeneratorFunctions`

**AST conversion parity (engine_javascript_ast_conversion_test.go)**:
- React hook member call parity:
  `TestDefaultEngineParsePathReactHookMemberCallParity`
- AWS client symbol constructor only:
  `TestDefaultEngineParsePathAWSClientSymbolConstructorOnly`

**Dead-code roots (javascript_dead_code_*_test.go — 12+ files)**:
- Node package entrypoints:
  `javascript_dead_code_node_entrypoints_test.go` (2 tests)
- Package script roots:
  `javascript_dead_code_package_scripts_test.go`
- CommonJS default export class roots:
  `javascript_dead_code_commonjs_class_test.go` (2 tests)
- Framework route callbacks:
  `javascript_dead_code_framework_routes_test.go`
- Hapi handler/plugin/alias/proxy/Typescript roots:
  `javascript_dead_code_hapi_alias_test.go` (7 tests),
  `javascript_dead_code_hapi_typescript_test.go`
- Node package roots (bin, main, exports, browser, types):
  `javascript_dead_code_node_roots_test.go` (5 tests)
- Express server symbols:
  `javascript_dead_code_roots_test.go:TestJavaScriptExpressServerSymbols`
- TS public surface and re-exports:
  `javascript_dead_code_typescript_surface_test.go`,
  `javascript_dead_code_typescript_import_exports_test.go`,
  `javascript_dead_code_typescript_surface_reexport_test.go`

**TypeScript (engine_javascript_tsconfig_baseurl_test.go,
engine_javascript_type_parameters_test.go)**:
- tsconfig baseUrl resolution
- Type parameters

**Embedded shell**:
  `embedded_shell_test.go:TestDefaultEngineParsePathJavaScriptEmbeddedShellCommands`

**Value-flow (js_cfg_dataflow_test.go, jsdataflow/)**:
- JS CFG dataflow tests
- Lowering tests
- Precision tests
- Taintfacts tests
- Destructuring tests
- Interproc tests

**Subdirectory unit tests**:
- tsconfig JSON parsing, path alias resolution, candidate ordering:
  `tsconfig_test.go`
- package.json nearest ownership, public source mapping:
  `package_json_test.go`
- Parent lookup cgo regression:
  `parent_lookup_regression_test.go:TestJavaScriptParentLookupEliminatesCgoCrossings`
- Residual regex characterization (17 tests for 3 permanent regex exceptions):
  `javascript_residual_regex_characterization_test.go`
- TS import/export re-exports from root:
  `javascript_dead_code_typescript_import_exports_test.go` (4 tests)
- TS surface re-export:
  `javascript_dead_code_typescript_surface_reexport_test.go`

**Cyclomatic complexity**: tested in `engine_cyclomatic_complexity_test.go`

## Unverified / Claimed-but-Untested Constructs
- **PreScan standalone test**: no dedicated test for `javascript.PreScan`; it
  is implicitly exercised through engine-level PreScanPaths tests.
- **Error paths**: nil parser, nil tree, unreadable file — not directly
  tested in the javascript subpackage.
- **`JavaScriptCallMetadata` helper**: tested indirectly through engine tests
  but no subdirectory unit test.
- **CommonJS module alias extraction** (`javaScriptCommonJSModuleExportAliases`):
  used in production but no dedicated test verifying alias resolution.
- **Sibling parser cache behavior**: tested indirectly through dead-code root
  tests but no unit test for `javaScriptSiblingParser` caching logic.
- **Fastify registration bases**:
  `javaScriptFastifyRegistrationBases` — tested through engine dead-code
  route handler tests but no standalone unit test.
- **New expression variable types**:
  `javaScriptNewExpressionVariableTypes` — no standalone test.
- **Type alias resolution in declaration merging**: basic merging tested but
  complex multi-file type alias chains not covered.
- **Depth cap on TS public surface walking**: the cycle-safe depth cap is
  documented but no test verifies a cycle actually terminates.
- **Composite tsconfig project references**: not tested.
- **All framework callback patterns**: React, Express, Fastify, Hapi, Next.js,
  NestJS each have some coverage but the exhaustive callback-signature shapes
  are not all pinned.

## Edge Cases Considered
- **Require template literal interpolation skipped**: `engine_javascript_require_test.go` (line 53)
- **Computed property concatenation still detected**: `engine_javascript_computed_property_test.go` (line 41)
- **Runtime-dependent computed names rejected**: `engine_javascript_computed_property_test.go` (line 71)
- **Express duplicate route stays unbound**: `engine_javascript_route_handler_test.go` (line 54)
- **React hook member call parity with old regex**: `engine_javascript_ast_conversion_test.go`
- **AWS client symbol constructor only** (not import bindings): `engine_javascript_ast_conversion_test.go`
- **Nested package.json ownership** (workspace root doesn't claim nested): `javascript_dead_code_node_roots_test.go` (line 142)
- **Nested Hapi handler roots**: `javascript_dead_code_node_roots_test.go` (line 299)
- **Hapi plugin register roots via init pattern**: `javascript_dead_code_node_roots_test.go:TestDefaultEngineParsePathJavaScriptHapiPluginRegisterRoots` (line 392)
- **CommonJS mixin export roots method**: `javascript_dead_code_commonjs_class_test.go` (line 53)
- **All 3 residual regexes with + and - cases**: `javascript_residual_regex_characterization_test.go` (17 tests)
- **Parent lookup cgo elimination**: `parent_lookup_regression_test.go` (line 106)
- **AST narrowing intentionally drops regex false positives** (hooks in comments, client symbols in imports): documented and tested in `engine_javascript_ast_conversion_test.go`
- **Comprehensive golden fixtures**: js/ts/tsx comprehensive golden fixtures in `engine_long_tail_test.go`
- **TypeScript import/export re-exports from root (Fastify shape)**:
  `javascript_dead_code_typescript_import_exports_test.go:TestTypeScriptImportedExportClauseReexportsFromRootHandlesFastifyShape`
- **Block comments ignored in re-export clauses**:
  `javascript_dead_code_typescript_import_exports_test.go:TestTypeScriptImportedExportClauseReexportsFromRootIgnoresBlockComments`
- **Imported type references from public declarations**:
  `javascript_dead_code_typescript_import_exports_test.go:TestTypeScriptImportedTypeReferencesFromPublicDeclarations`

## Edge Cases NOT Considered
- **Empty JS/TS/TSX file**: no test for zero-byte source.
- **Syntax error files**: parser behavior on unparseable JavaScript.
- **Very large files (>100K lines)**: no size-boundary test.
- **Deeply nested JSX component trees**: basic JSX tested but deeply nested
  (>20 levels) not covered.
- **ESM dynamic import() with template literals**: static imports and require
  tested but dynamic `import(\`./${name}\`)` not tested.
- **Decorator chaining on TS class members** (e.g., `@Foo @Bar method()`):
  basic decorators tested but chained decorators not explicitly covered.
- **`export default function()` with HOF wrappers**: basic exports tested but
  higher-order-function-wrapped default exports not tested.
- **TypeScript `satisfies` operator** (TS 4.9+): not tested.
- **`as const` assertions**: not tested.
- **TS enums with computed values**: basic enum declarations tested but computed
  members not covered.
- **`export type * from` re-exports**: basic re-exports tested but wildcard
  type re-exports not.
- **Declaration maps and `.d.ts.map` files**: not supported/tested.
- **`package.json` without `name` field**: no test.
- **Paths with symlinks**: no test for symlinked tsconfig or package.json.
- **Sibling parser with nonexistent sibling file**: not tested.
- **Value-flow for async/await patterns**: basic taint tested but async/await
  interprocedural not specifically verified.
- **Concurrent calls to `Parse` with shared `ParserFactory`**: not tested.

## Verdict
**deep**

The JavaScript parser has Eshu's most comprehensive test coverage: approximately
35 parent engine tests covering imports, calls, routes, handlers, dead-code
roots, frameworks, semantics, tsconfig, embedded shell, computed properties, AST
conversion parity, and value-flow, plus 20+ subdirectory unit tests for tsconfig,
package.json, parent lookup, residual regexes, TypeScript surface re-exports,
and jsdataflow lowering. Edge-case coverage includes false-positive suppression
from AST narrowing, Hapi/Express/Next.js/Fastify route patterns, CommonJS class
method roots across nested exports, package.json workspace isolation, and cgo
crossing elimination. Remaining gaps are TS 4.9+ features, async value-flow,
and deep nesting/large-file bounds.

## Recommended Actions
1. Add a standalone test for `javascript.PreScan` verifying declaration name
   extraction matching the Parse output.
2. Add error-path tests: nil/absent ParserFactory, unreadable file, empty file,
   nil tree.
3. Add a test for ESM dynamic `import()` with template literal source
   specifiers.
4. Add a test for `satisfies` operator and `as const` assertions (document as
   intentionally unsupported if needed).
5. Add a sibling parser test for nonexistent sibling file paths.
6. Add a benchmark/regression test for deep JSX nesting (>20 levels).
7. Add value-flow tests for async/await patterns in taint analysis.
8. Add a test for chained decorators (`@Foo @Bar`) on TS class members.
9. Add a test for the TypeScript public surface depth cap — verify a cycle
   terminates without infinite recursion.
10. Add a test for `export type * from` wildcard type re-exports.
