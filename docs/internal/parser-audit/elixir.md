# Elixir Parser Audit

## Overview
The Elixir parser (`go/internal/parser/elixir/`) uses a single tree-sitter AST walk (`elixirExtractor.extract`) to emit modules, protocols, functions, imports, attributes, variables, calls, guard calls, dead-code root kinds, and exactness blockers. Hex dependency rows from `mix.exs`/`mix.lock` use bounded regex manifest parsing. It has 1 subdirectory test file (`hex_dependency_test.go`) with hex dependency proof, and 4 parent-level test files (`engine_elixir_ast_test.go`, `engine_elixir_review_test.go`, `engine_elixir_semantics_test.go`, `elixir_dead_code_roots_test.go`) covering AST extraction, dead-code roots, semantics, and review follow-up cases.

## Claimed Constructs
From `doc.go:7-24`, `README.md:7-18`, and function docstrings:

| Construct | Source reference |
|---|---|
| Modules (`defmodule`) | `ast_extract.go:106-147` (`handleModuleCall`), `doc.go:7` |
| Protocols (`defprotocol`) | `ast_extract.go:128-142`, `doc.go:7` |
| Protocol implementations (`defimpl`) | `ast_extract.go:129-134`, `doc.go:7` |
| Functions (`def`, `defp`, `defmacro`, `defmacrop`) | `ast_extract.go:151-238` (`handleFunctionCall`), `doc.go:7` |
| Guards (`defguard`, `defguardp`) | `ast_extract.go:232-234` (`appendGuardCalls`) |
| Delegates (`defdelegate`) | `ast_shared.go:161,187` |
| Imports (`use`, `import`, `alias`, `require`) | `ast_calls.go:15-43` (`handleImportCall`), `doc.go:7` |
| Module attributes (`@name`) | `ast_calls.go:48-84` (`handleAttribute`), `doc.go:7` |
| Variables (via attributes) | `ast_calls.go:65-83`, `doc.go:7` |
| Function calls | `ast_calls.go:88-114` (`handleExpressionCall`), `doc.go:8` |
| Guard calls | `ast_calls.go:161-188` (`appendGuardCalls`) |
| Cyclomatic complexity | `complexity.go:60` (`elixirCyclomaticComplexity`) |
| Dead-code roots: `elixir.application_start` | `dead_code_roots.go:88-89` |
| Dead-code roots: `elixir.public_macro` | `dead_code_roots.go:91` |
| Dead-code roots: `elixir.public_guard` | `dead_code_roots.go:94` |
| Dead-code roots: `elixir.protocol_function` | `dead_code_roots.go:98` |
| Dead-code roots: `elixir.protocol_implementation_function` | `dead_code_roots.go:100` |
| Dead-code roots: `elixir.behaviour_callback` | `dead_code_roots.go:103` |
| Dead-code roots: `elixir.genserver_callback` | `dead_code_roots.go:106` |
| Dead-code roots: `elixir.supervisor_callback` | `dead_code_roots.go:109` |
| Dead-code roots: `elixir.mix_task_run` | `dead_code_roots.go:112` |
| Dead-code roots: `elixir.phoenix_controller_action` | `dead_code_roots.go:115` |
| Dead-code roots: `elixir.phoenix_liveview_callback` | `dead_code_roots.go:118` |
| Exactness blockers (`dynamic_dispatch_unresolved`) | `ast_calls.go:192-207` (`markFunctionDynamicDispatch`), `doc.go:8` |
| Hex dependencies from `mix.exs` | `hex_dependencies.go:21-66` (`appendMixManifestDependencyRows`), `doc.go:16` |
| Hex dependencies from `mix.lock` | `hex_dependencies.go:68-103` (`appendMixLockDependencyRows`), `doc.go:16` |
| PreScan (functions, modules, protocols) | `language.go:59-67` |

## Verified-by-Test Constructs

Subdirectory test (`elixir/hex_dependency_test.go`):
| Construct | Test reference |
|---|---|
| Hex dependency from `mix.exs` (config_kind, package_manager, value, scope) | `hex_dependency_test.go:17-82` |
| Hex dependency with organization/namespace | `hex_dependency_test.go:53` |
| Hex dependency with app_name override | `hex_dependency_test.go:59-60` |
| Hex dependency with `only: :test` scope | `hex_dependency_test.go:66` |
| VCS dependency from `mix.exs` (github) | `hex_dependency_test.go:72` |
| VCS dependency from `mix.exs` (git) | `hex_dependency_test.go:78` |
| Hex dependency from `mix.lock` (lockfile, direct_dependency) | `hex_dependency_test.go:84-113` |
| Nested dependency from `mix.lock` (depth=2, direct_dependency=false) | `hex_dependency_test.go:112-113` |

Parent-level tests:
| Construct | Test file:function |
|---|---|
| Modules and protocols (module_kind) | `engine_elixir_semantics_test.go:12` (`TestDefaultEngineParsePathElixirModuleKindsAndFunctionKinds`) |
| Function metadata (visibility, semantic_kind, args) | `engine_elixir_semantics_test.go:57` (`TestDefaultEngineParsePathElixirFunctionMetadata`) |
| Import and call metadata (receiver, context) | `engine_elixir_semantics_test.go:126` (`TestDefaultEngineParsePathElixirImportAndCallMetadata`) |
| Alias brace expansion (`{Basic, Worker}`) | `engine_elixir_semantics_test.go:202` (`TestDefaultEngineParsePathElixirAliasBraceExpansionAndGuardCalls`) |
| Guard calls in defguard | `engine_elixir_semantics_test.go:202` |
| Module attributes | `engine_elixir_semantics_test.go:253` (`TestDefaultEngineParsePathElixirEmitsModuleAttributes`) |
| Multiline source spans | `engine_elixir_semantics_test.go:293` (`TestDefaultEngineParsePathElixirMultilineSourceSpans`) |
| End lines (no source index) | `engine_elixir_semantics_test.go:353` (`TestDefaultEngineParsePathElixirEndLinesDoNotRequireSourceIndex`) |
| Multiline callback signature | `engine_elixir_semantics_test.go:387` (`TestDefaultEngineParsePathElixirMultilineCallbackSignature`) |
| Module span covers nested ends | `engine_elixir_ast_test.go:16` (`TestDefaultEngineParsePathElixirModuleSpanCoversNestedEnds`) |
| Protocol impl functions keep own context | `engine_elixir_ast_test.go:63` (`TestDefaultEngineParsePathElixirProtocolImplFunctionsKeepOwnContext`) |
| One-line `def ..., do:` omits calls | `engine_elixir_ast_test.go:145` (`TestDefaultEngineParsePathElixirOneLineDoBodyOmitsCalls`) |
| Field access is not call | `engine_elixir_ast_test.go:181` (`TestDefaultEngineParsePathElixirFieldAccessIsNotCall`) |
| @impl carries across comments | `engine_elixir_ast_test.go:223` (`TestDefaultEngineParsePathElixirImplDecoratorCarriesAcrossComments`) |
| @impl vs @implementation distinction | `engine_elixir_review_test.go:11` (`TestDefaultEngineParsePathElixirImplPrefixAttributeIsNotCallback`) |
| Guarded multiline callback signature | `engine_elixir_review_test.go:42` (`TestDefaultEngineParsePathElixirGuardedMultilineCallbackSignature`) |
| `elixir.application_start` | `elixir_dead_code_roots_test.go:100` (`TestDefaultEngineParsePathElixirEmitsDeadCodeRootKinds`) |
| `elixir.behaviour_callback` | `elixir_dead_code_roots_test.go:101,103,159,161` |
| `elixir.genserver_callback` (init, handle_call, handle_cast, handle_info, terminate, code_change) | `elixir_dead_code_roots_test.go:102,104,160,162` |
| `elixir.supervisor_callback` | `elixir_dead_code_roots_test.go:105` |
| `elixir.mix_task_run` | `elixir_dead_code_roots_test.go:106` |
| `elixir.protocol_function` | `elixir_dead_code_roots_test.go:107` |
| `elixir.protocol_implementation_function` | `elixir_dead_code_roots_test.go:108` |
| `elixir.phoenix_liveview_callback` (mount, handle_event, render) | `elixir_dead_code_roots_test.go:109-111` |
| `elixir.public_macro` | `elixir_dead_code_roots_test.go:112` |
| `elixir.public_guard` | `elixir_dead_code_roots_test.go:113` |
| `elixir.phoenix_controller_action` | `elixir_dead_code_roots_test.go:99` |
| Exactness blockers for dynamic dispatch | `elixir_dead_code_roots_test.go:163` (`TestDefaultEngineParsePathElixirDeadCodeFixtureExpectedRoots`) |
| Functions without root kinds (negative assertions) | `elixir_dead_code_roots_test.go:165-166` |
| Cyclomatic complexity (straight-line, branches, boolean, inline keyword body) | `engine_cyclomatic_complexity_test.go:262-278` |
| Cyclomatic complexity (catch-all arm exclusion, boolean case arms) | `engine_cyclomatic_complexity_arms_test.go:178-190` |

## Unverified / Claimed-but-Untested Constructs
- **Elixir fixture corpus** — `engine_long_tail_test.go:219` (`TestDefaultEngineParsePathElixirFixtures`) exists but only checks that the parser does not error; no symbol-level assertions are made against the comprehensive corpus.
- **`defdelegate`** — claimed in `ast_shared.go:161,187` (`functionSemanticKind` returns `"delegate"`), no test verifies delegate extraction or its `semantic_kind`.
- **`defmacrop`** — claimed in `ast_shared.go:161` (function-like keyword), not tested distinctly from `defmacro`.
- **`defguardp`** — claimed in `ast_shared.go:161` as function-like keyword, not tested. Only `defguard` is exercised via the dead-code root kind `elixir.public_guard`.
- **Hex key `dependency_scope=optional`** from `mix.lock` — `mixLockDependencyScope` in `hex_dependencies.go:178` supports `optional`, but not tested.
- **`LiveComponent` use** detecting — `dead_code_roots.go:57` has `phoenix_live_component`, but `elixirIsLiveViewCallback` is only tested for `phoenix_live_view`, not `phoenix_live_component` (the conditions check both, but no fixture exercises LiveComponent callbacks).
- **Inline keyword body complexity** — `engine_cyclomatic_complexity_test.go:278` tests the case, but only for straight-line inline bodies; branching inline bodies (with `if`/`case` in `do:` body) are not separately tested.
- **`@doc` and `@moduledoc` suppression** — `ast_calls.go:62` claims these do not produce variable rows, but not tested.
- **Source indexing (`options.IndexSource`)** — `ast_extract.go:194-198` emits `source`/`docstring` only when `IndexSource` is set. No test exercises this path.

## Edge Cases Considered
- One-line `def ..., do:` omits calls: `engine_elixir_ast_test.go:145`
- Field access (`state.items`) is not a call: `engine_elixir_ast_test.go:181`
- @impl carries across comment lines: `engine_elixir_ast_test.go:223`
- @implementation prefix does not trigger @impl behavior: `engine_elixir_review_test.go:11`
- Guarded multiline callback signature: `engine_elixir_review_test.go:42`
- Protocol and protocol_impl root kind boundaries: `engine_elixir_ast_test.go:63`
- Module span covers nested ends: `engine_elixir_ast_test.go:16`
- Cyclomatic complexity catch-all arm exclusion (`_` wildcard, `true` for cond): `engine_cyclomatic_complexity_arms_test.go:178-190`
- Cyclomatic complexity inline keyword body: `engine_cyclomatic_complexity_test.go:278`
- Alias brace expansion: `engine_elixir_semantics_test.go:202`
- VCS dependencies vs Hex dependencies (git, github, non-registry source): `hex_dependency_test.go:72-82`
- Multiline source spans and end lines: `engine_elixir_semantics_test.go:293,353`
- Call requires parenthesized arguments: `ast_calls.go:97` checked but only indirectly through fixtures

## Edge Cases NOT Considered
- Empty Elixir source file
- File with only comments / whitespace
- Module with unquoted module name (e.g., `:atom`-style names)
- Unicode module names and function names
- Deeply nested module/function bodies (>100 levels)
- `defstruct` and `defexception` definitions
- `@type`/`@spec` type specifications
- `use` / `__using__` macro expansion
- Pipelines (`|>` operator) in complexity counting
- Nested `fn` anonymous functions containing calls
- Custom string sigils (`~r/.../`, `~s"""..."""`)
- Mixed `mix.exs` with both hex and path dependencies
- `in_umbrella: true` dependency detection

## Verdict
**deep**

The Elixir parser has excellent coverage across its 1 subdirectory + 4 parent-level test files. Every claimed dead-code root kind is asserted with positive and negative tests. Hex dependency extraction has both `mix.exs` and `mix.lock` coverage with namespace, app_name, scope, and VCS distinction. AST-level behaviors (one-line omission, field-access filtering, @impl comment crossing, protocol boundaries) each have dedicated tests. The few gaps are in edge cases (empty files, Unicode, nested fn calls), `defdelegate`/`defmacrop`/`defguardp` distinct testing, and the fixture corpus test lacking symbol-level assertions.

## Recommended Actions
1. Add a dedicated test for `defdelegate` with `semantic_kind=delegate` assertion.
2. Add distinct tests for `defmacrop` and `defguardp` (not just their public counterparts).
3. Add an empty/comment-only Elixir file parse test.
4. Add `dependency_scope=optional` test case for `mix.lock`.
5. Add a LiveComponent callback test (`handle_event` with `use LiveComponent`).
6. Add symbol-level assertions to `TestDefaultEngineParsePathElixirFixtures` in `engine_long_tail_test.go` instead of just checking no-error.
7. Add inline keyword body with branching (`if`/`case` in `do:`) complexity test.
8. Test `options.IndexSource` path for source/text emission.
