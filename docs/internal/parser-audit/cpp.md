# C++ Parser Audit

## Overview
The C++ parser (`go/internal/parser/cpp/`) uses tree-sitter to extract functions, classes, structs, enums, unions, includes, macros, typedef aliases, calls, and 7 dead-code root kinds. It has been through multiple regex-to-AST migrations (issues #3540, #3574) and now uses AST field walks for primary symbol extraction. The remaining 9 `regexp.MustCompile` sites are documented fallbacks: 3 within-node text scans in `dead_code_roots.go`, 5 external-header text scans in `header_roots.go`, and 1 typedef alias fallback in `parser.go`.

## Claimed Constructs
From `doc.go:6-15`, `README.md:5-7`, and function docstrings:

| Construct | Source reference |
|---|---|
| Functions (free and member) | `parser.go:60` (`appendCPPFunction`), `doc.go:6-7` |
| Classes | `parser.go:50`, `helpers.go:29` (`appendNamedType`) |
| Structs | `parser.go:52`, `helpers.go:29` |
| Enums | `parser.go:54`, `helpers.go:29` |
| Unions | `parser.go:56`, `helpers.go:29` |
| Includes (`#include`) | `parser.go:46`, `dead_code_roots.go:156` (`appendCPPImportMetadata`) |
| Macros (`#define`, `#define`-function) | `parser.go:48`, `helpers.go:43` (`appendMacro`) |
| Typedef aliases | `parser.go:58-62`, `parser.go:119` (`appendCTypedefAliases`) |
| Calls | `parser.go:64`, `helpers.go:70` (`appendCall`) |
| Cyclomatic complexity | `complexity.go:34` (`cppComplexitySet`) |
| Dead-code roots: `cpp.main_function` | `dead_code_roots.go:16`, `README.md:34-35` |
| Dead-code roots: `cpp.public_header_api` | `dead_code_roots.go:17`, `header_roots.go:58-62` (`AnnotatePublicHeaderRoots`) |
| Dead-code roots: `cpp.virtual_method` | `dead_code_roots.go:18` |
| Dead-code roots: `cpp.override_method` | `dead_code_roots.go:19` |
| Dead-code roots: `cpp.callback_argument_target` | `dead_code_roots.go:20` |
| Dead-code roots: `cpp.function_pointer_target` | `dead_code_roots.go:21` |
| Dead-code roots: `cpp.node_addon_entrypoint` | `dead_code_roots.go:22` |
| PreScan (functions, classes, structs, enums, unions, macros) | `parser.go:86-92` |
| Qualified out-of-class method name extraction | `qualified_method.go:38` (`cppQualifiedFunctionNameAndClassFromNode`) |

## Verified-by-Test Constructs
Tests verify these constructs with specific references:

| Construct | Test file:function |
|---|---|
| Functions (name, class_context, line_number) | `cpp_dead_code_roots_test.go:11`: `TestDefaultEngineParsePathCPPMarksDeadCodeRootKinds`; `engine_systems_test.go:68`: `TestDefaultEngineParsePathCPP` |
| Classes and structs | `engine_systems_test.go:68`: `TestDefaultEngineParsePathCPP` |
| Includes (`#include`) | `engine_systems_test.go:68`: `TestDefaultEngineParsePathCPP` (imports bucket checked) |
| Macros | `engine_systems_test.go:68`: `TestDefaultEngineParsePathCPP` |
| `cpp.main_function` | `cpp_dead_code_roots_test.go:94`; `cpp/dead_code_roots_test.go:126` |
| `cpp.public_header_api` (free functions) | `cpp/dead_code_roots_test.go:216` (`TestDefaultEngineParsePathCPPMarksIncludedHeaderPublicAPI`); `cpp/dead_code_roots_test.go:127` |
| `cpp.public_header_api` (class methods) | `cpp/dead_code_roots_test.go:217`; `cpp/dead_code_roots_test.go:128` |
| `cpp.public_header_api` (namespace-qualified) | `cpp/dead_code_roots_test.go:278` (`TestDefaultEngineParsePathCPPMarksNamespaceQualifiedHeaderMethod`) |
| `cpp.virtual_method` | `cpp_dead_code_roots_test.go:103`; `cpp/dead_code_roots_test.go:129` |
| `cpp.override_method` | `cpp_dead_code_roots_test.go:104`; `cpp/dead_code_roots_test.go:130` |
| `cpp.callback_argument_target` | `cpp_dead_code_roots_test.go:95`; `cpp/dead_code_roots_test.go:131` |
| `cpp.function_pointer_target` | `cpp_dead_code_roots_test.go:97-99`; `cpp/dead_code_roots_test.go:132` |
| `cpp.node_addon_entrypoint` (NODE_MODULE_INIT/NAPI_MODULE_INIT) | `cpp_dead_code_roots_test.go:100-102`; `cpp/dead_code_roots_test.go:133` |
| `cpp.node_addon_entrypoint` (NODE_MODULE/NODE_MODULE_CONTEXT_AWARE/NAPI_MODULE) | `engine_systems_test.go:463` (`TestDefaultEngineParsePathCPPNodeModuleRegistrationFromAST`); `cpp/dead_code_roots_test.go:98-99` |
| Qualified method name extraction (AST) | `cpp/dead_code_roots_test.go:58` (`TestCPPQualifiedFunctionNameAndClassFromNode`) — 10 cases incl. operator, template, pointer/reference return |
| Qualified method source isolation | `cpp/dead_code_roots_test.go:107` (`TestCPPQualifiedFunctionNameUsesDeclaratorNotBody`) |
| Out-of-line qualified methods end-to-end | `cpp_qualified_method_ast_test.go:16` (`TestDefaultEngineParsePathCPPOutOfLineQualifiedMethodsViaAST`) |
| Cyclomatic complexity (straight-line, branches, boolean ops, catch) | `engine_cyclomatic_complexity_test.go:73-80`; `engine_cyclomatic_complexity_arms_test.go:31-38` |
| Empty file handling | `engine_systems_test.go:521` — parse of empty `main.cpp` |
| PreScan names | `engine_systems_test.go:281-289` |

## Unverified / Claimed-but-Untested Constructs
- **Enums and unions** — claimed in `doc.go` and emitted by `parser.go:54-56`, but no test asserts `enums` or `unions` bucket content. The `engine_systems_test.go:68` test parses `main.cpp` which lacks enums/unions; the dead-code root fixtures lack them too.
- **`cTypedefAliasPattern` fallback** — the regex at `parser.go:15-17` has no dedicated test; typedef extraction in `engine_systems_test.go:68` may exercise the AST path, but the `type_definition` regex fallback path in `cTypedefBucket` (line 181) has no specific test.
- **`cppDirectInitializerTargetPattern`** and **`cppBraceInitializerPattern`** — these within-node regexes at `dead_code_roots.go:44,53` support `cpp.function_pointer_target` but have no focused tests for edge cases (multi-target brace, empty braces, malformed initializers).
- **`cppFunctionPointerAliasPattern`** — at `dead_code_roots.go:34`, used for alias name collection. Tested only indirectly through `cpp.function_pointer_target` assertions.
- **Node addon call-expression registration via macros** — `annotateCPPNodeAddonRegistrationRoot` in `dead_code_roots.go:112` tested via `engine_systems_test.go:463` but only for `NODE_MODULE`; `NAPI_MODULE` and `NODE_MODULE_CONTEXT_AWARE` not explicitly tested with call-expression registration.

## Edge Cases Considered
- Empty file: `engine_systems_test.go:521`
- Virtual and override methods: `cpp_dead_code_roots_test.go:103-104`
- Namespace-qualified header methods (`api::Service::run`): `cpp_dead_code_roots_test.go:278`
- Operator overloads in qualified names: `cpp/dead_code_roots_test.go:77` (10 cases)
- Template-qualified methods (`Box<T>::get`): `cpp/dead_code_roots_test.go:76`
- Pointer/reference return declarator unwrapping: `qualified_method.go:63-68` tested via `cpp/dead_code_roots_test.go:77`
- Nested qualified identifiers (`Outer::Inner::method`): `qualified_method.go:104` tested via AST extractor
- Destructor names: `qualified_method.go:63`
- Include kinds (local vs system): `dead_code_roots.go:156`
- Static-storage function prototypes excluded from public header API: `header_roots.go:260`
- Comment stripping in header scanning: `header_roots.go:255`
- Cyclomatic complexity catch and default arms: `engine_cyclomatic_complexity_arms_test.go:31-38`
- Header path security (repo-root bounds, symlink resolution): `header_roots.go:115-153`

## Edge Cases NOT Considered
- Empty `#include` directives (no path node) — not tested
- Unicode identifiers in C++ source
- Deeply nested namespaces (>5 levels) for qualified methods
- `friend` function declarations in class bodies
- Template specializations recognized as dead-code roots
- Constructor/destructor methods with explicit `= default` / `= delete`
- Lambda expressions (not extracted as functions)
- `using` braces declaration aliases as separate function-pointer tracking
- Concurrent parse of the same cpp file in multiple goroutines
- Transitive include resolution (intentionally excluded per `header_roots.go:19-22`)

## Verdict
**moderate**

The CPP parser has good coverage of its core dead-code root kinds (7 root types with dedicated fixture tests), qualified method extraction, and complexity computation. However, enums and unions lack explicit test coverage, and the three within-node regex fallbacks (`cppFunctionPointerAliasPattern`, `cppDirectInitializerTargetPattern`, `cppBraceInitializerPattern`) are only tested indirectly through root-kind assertions, not through focused unit tests on their boundary behavior (malformed text, empty strings, multi-target brace initializers).

## Recommended Actions
1. Add explicit tests for `enums` and `unions` bucket emissions with a fixture containing both.
2. Add focused tests for `cppFunctionPointerAliasPattern`, `cppDirectInitializerTargetPattern`, and `cppBraceInitializerPattern` with malformed and edge-case inputs.
3. Add a test for `NAPI_MODULE` and `NODE_MODULE_CONTEXT_AWARE` via call-expression registration (not just the `*_MODULE_INIT` names).
4. Add tests for template specializations with destructors, `= default`, and `= delete`.
5. Add Unicode identifier tests.
6. Add a dead-code fixture test covering both `using` and `typedef` function-pointer alias forms with the same targets.
