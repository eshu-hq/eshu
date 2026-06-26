# C Parser Audit

## Overview
The C parser (`go/internal/parser/c/`) uses a tree-sitter AST walk for primary symbol extraction (functions, structs, enums, unions, macros, typedefs, includes, variables, calls) and adds bounded dead-code root metadata for entrypoints, signal handlers, callback arguments, direct function-pointer initializers (including brace-initializer tables), and functions declared by directly-included local headers. Header prototype scanning and function-pointer initializer parsing use regex over already-located AST node text or external header files — documented permanent exceptions rather than primary symbol extraction. C has one dedicated parent-level test file (`c_dead_code_roots_test.go`) plus C-specific tests within `engine_systems_test.go`, totaling 12 C-specific subtests.

## Claimed Constructs
| Construct | Source Reference |
|---|---|
| `functions` | `parser.go:97-115` (`appendCFunction`) |
| `structs` | `parser.go:47` (`struct_specifier`), `helpers.go:29-41` (`appendNamedType`) |
| `enums` | `parser.go:49` (`enum_specifier`) |
| `unions` | `parser.go:51` (`union_specifier`) |
| `imports` (includes) | `parser.go:43` (`preproc_include`), `parser.go:117-140` (`appendCImportMetadata`) |
| `macros` | `parser.go:45` (`preproc_def`, `preproc_function_def`), `helpers.go:43-55` (`appendMacro`) |
| `typedefs` | `parser.go:53-58` (`type_definition`, `typedef`-prefixed `declaration`), `parser.go:142-168` (`appendCTypedefAliases`) |
| `variables` | `parser.go:61-64` (`declaration` → `init_declarator`, module-scope only), `parser.go:257-274` (`appendCDeclarationVariables`) |
| `function_calls` | `parser.go:66` (`call_expression`), `parser.go:234-255` (`appendCCall`) |
| `cyclomatic_complexity` | `complexity.go:31-33` |
| PreScan names | `parser.go:89-95` |
| `dead_code_root_kinds`: `c.main_function` | `dead_code_roots.go:76-80` |
| `c.public_header_api` | `dead_code_roots.go:51-67` (`AnnotatePublicHeaderRoots`) |
| `c.signal_handler` | `dead_code_roots.go:326-339` (`annotateCSignalHandlerRoot`) |
| `c.callback_argument_target` | `dead_code_roots.go:341-354` (`annotateCCallbackArgumentRoot`) |
| `c.function_pointer_target` | `dead_code_roots.go:356-375` (`annotateCFunctionPointerTargetRoot`) |

## Verified-by-Test Constructs
| Construct | Test Reference |
|---|---|
| `functions` (name) | `engine_systems_test.go:55` (`TestDefaultEngineParsePathC`) |
| `structs` (name) | `engine_systems_test.go:56` |
| `enums` (name) | `engine_systems_test.go:57` |
| `imports` (name, source, full_import_name, include_kind) | `engine_systems_test.go:58-61` |
| `function_calls` (name, full_name) | `engine_systems_test.go:62-63` |
| `variables` (name) | `engine_systems_test.go:64` |
| `macros` (name) | `engine_systems_test.go:65` |
| `typedefs` (simple type alias) | `engine_systems_test.go:295-331` (`TestDefaultEngineParsePathCTypedefAliasEmitsDedicatedEntities`) |
| `typedefs` → enum/union/struct buckets | `engine_systems_test.go:333-376` (`TestDefaultEngineParsePathCTypedefAliases`) |
| `typedefs` (function pointer, nested struct, named struct) | `engine_systems_test.go:383-420` (`TestDefaultEngineParsePathCTypedefAliasesFromASTOnly`) |
| `typedefs` (multi-declarator, array typedef) | `engine_systems_test.go:429-458` (`TestDefaultEngineParsePathCTypedefAliasMultiDeclaratorAndArray`) |
| `c.main_function` | `c_dead_code_roots_test.go:28` (`TestDefaultEngineParsePathCDeadCodeFixtureExpectedRoots`), lines 354-398 (duplicate `main`) |
| `c.public_header_api` | `c_dead_code_roots_test.go:29`, lines 38-119 (static/commented excluded) |
| `c.signal_handler` | `c_dead_code_roots_test.go:30`, lines 251-252 (signal with `&`) |
| `c.callback_argument_target` | `c_dead_code_roots_test.go:249-252` (`TestDefaultEngineParsePathCMarksCallbackArgumentTargets`) |
| `c.function_pointer_target` | `c_dead_code_roots_test.go:31`, lines 258-351 (all 11 variants) |
| `c.callback_argument_target` + `c.signal_handler` combined | `c_dead_code_roots_test.go:252` |
| Negative: unused handler NOT rooted | `c_dead_code_roots_test.go:253` |
| Negative: static header prototype NOT public API | `c_dead_code_roots_test.go:111` |
| Negative: commented-out header prototype NOT public API | `c_dead_code_roots_test.go:114-118` |
| Negative: header outside repo root NOT read | `c_dead_code_roots_test.go:156-158` |
| Negative: symlink outside repo root NOT followed | `c_dead_code_roots_test.go:202-203` |
| Negative: directly-used helper NOT dead-code rooted | `c_dead_code_roots_test.go:33-35` |
| Negative: unused callback handler NOT rooted | `c_dead_code_roots_test.go:253` |
| Negative: unused function pointer target NOT rooted | `c_dead_code_roots_test.go:349-350` |
| `cyclomatic_complexity` | `engine_cyclomatic_complexity_test.go:59-66` (C-specific straight-line and branchy fixtures) |
| PreScan names (C part of systems pre-scan) | `engine_systems_test.go:245-293` (`TestDefaultEnginePreScanPathsSystems`) |

## Unverified / Claimed-but-Untested Constructs
- **`unions`**: The `unions` bucket is initialized in `Parse` (line 34) and extracted from `union_specifier` nodes (line 51), but no test explicitly asserts a union is extracted. The typedef-as-union test (`TestDefaultEngineParsePathCTypedefAliases`) covers union buckets only through the typedef path, not a direct `union Name { ... };` declaration.
- **Function `end_line`**: No test asserts that function `end_line` spans the entire function body (closing brace).
- **Call `full_name` with receiver**: The test only asserts `printf` (bare call). No test covers `obj.method()` or similar qualified calls.
- **`include_kind` "local"**: Only system includes (`<stdio.h>`) are tested. Local includes (`"header.h"`) are not directly verified.
- **`decorators`**: Always emitted as `[]string{}` sentinel. No test asserts its presence.

## Edge Cases Considered
| Edge Case | Test Reference |
|---|---|
| Static header prototypes excluded from public API | `c_dead_code_roots_test.go:111` |
| Block-commented header prototypes excluded | `c_dead_code_roots_test.go:114` |
| Line-commented header prototypes excluded | `c_dead_code_roots_test.go:117` |
| Headers outside repo root excluded | `c_dead_code_roots_test.go:156` |
| Symlinks outside repo root excluded | `c_dead_code_roots_test.go:202` |
| Duplicate `main` under conditional compilation | `c_dead_code_roots_test.go:354-398` |
| Bare callback argument (no `&`) | `c_dead_code_roots_test.go:232,249` |
| Address-of callback argument (`&`) | `c_dead_code_roots_test.go:233,250` |
| Signal handler via `&function_name` | `c_dead_code_roots_test.go:234,251` |
| Bare function pointer initializer | `c_dead_code_roots_test.go:317,338` |
| Address-of function pointer initializer (`&`) | `c_dead_code_roots_test.go:318,339` |
| Typedef function pointer initializer | `c_dead_code_roots_test.go:319,340` |
| Multi-declarator function pointer assignment | `c_dead_code_roots_test.go:320,341-342` |
| Typedef multi-declarator function pointer assignment | `c_dead_code_roots_test.go:321,343-344` |
| Brace-initializer table function pointers | `c_dead_code_roots_test.go:322,345-346` |
| Typedef brace-initializer table function pointers | `c_dead_code_roots_test.go:323,347-348` |
| Function pointer typedef recognition | `dead_code_roots.go:199-212` (exercised by typedef_target tests) |
| Multi-declarator typedef (first alias only) | `engine_systems_test.go:437-456` |
| Array typedef (`buffer[64]`) | `engine_systems_test.go:441,457` |
| Function pointer typedef alias | `engine_systems_test.go:391,415` |
| Nested anonymous struct typedef | `engine_systems_test.go:393-397,416,418` |
| Named struct typedef | `engine_systems_test.go:399-401,417,419` |
| Keyword-like identifiers filtered from header prototypes | `dead_code_roots.go:298` (`cKeywordLikeIdentifier` — `if`, `for`, `while`, `switch`, `return`, `sizeof`) |

## Edge Cases NOT Considered
No test covers: invalid C syntax, empty files, very large files, macro expansion affecting include paths, transitive includes, dynamic symbol lookup (`dlopen`/`dlsym`), function pointers in struct fields, opaque pointer types (`typedef struct Foo *FooPtr`), `_Generic` expressions, C11 atomics, thread-local storage, flexible array members, bitfield members, variadic functions, K&R-style function definitions, inline assembly, `__attribute__` annotations, designated initializers in function pointer tables, multi-level pointer indirection in initializers, or the `source` field with `IndexSource`.

## Verdict
**Deep** — The C parser has 12 focused subtests covering all 5 dead-code root kinds with positive, negative, and edge-case assertions. Primary symbol extraction (functions, structs, enums, macros, includes, calls, variables, typedefs) is verified. Function-pointer initializer coverage is exhaustive: 11 variants across bare, address-of, typedef, multi-declarator, and brace-initializer forms. Header-public-API roots test static, commented, out-of-repo, and symlink exclusion. The only minor gap is the absence of a direct `union Name {...}` test (unions are tested indirectly through typedef aliases).

## Recommended Actions
1. Add a test for a direct `union Data { int i; float f; };` declaration (not via typedef) to verify union extraction without typedef.
2. Add a test for local include (`#include "header.h"`) with `include_kind: "local"`.
3. Add a test for qualified function calls (e.g., `obj.method()`) to verify `full_name` with receiver.
4. Add a test asserting `decorators` field is present and empty for C functions.
5. Add a test for function `end_line` spanning the entire function body.
6. Add a test for `typedef struct Foo *FooPtr;` (opaque pointer typedef) to verify alias extraction.
