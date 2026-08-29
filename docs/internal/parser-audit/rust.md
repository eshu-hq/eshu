# Rust Parser Audit

## Overview
The Rust parser (`go/internal/parser/rust/`) is a mature tree-sitter adapter that extracts functions, structs/enums/unions, traits, impl blocks, modules, variables (const/static), type aliases, macros, imports, calls, nested annotations, lifetime evidence, generic metadata, where-clause predicates, attribute/derive/conditional-derive metadata, exactness blockers, and Cargo dependency evidence from `Cargo.toml` and `Cargo.lock`. Its directory owns package-level tests and external `rust_test` suites that exercise Rust source through the parent engine. Cross-language engine coverage remains in the parent parser directory.

## Claimed Constructs
List every construct the parser claims to extract, with source references.

1. **Functions** — `parser.go:47-48,177-251` (`function_item`, `function_signature_item`)
2. **Classes** (struct, enum, union) — `parser.go:49-50,103-124` (`struct_item`, `enum_item`, `union_item`)
3. **Traits** — `parser.go:51-52,126-147` (`trait_item`)
4. **Impl blocks** — `parser.go:45-46,149-175` (`impl_item`), with kind (`inherent_impl`/`trait_impl`)
5. **Modules** — `parser.go:62-63,325-357` (`mod_item`), with `declared_path_candidates`, `module_path_source`
6. **Variables** (const, static) — `parser.go:43-44,280-304` (`const_item`, `static_item`)
7. **Type aliases** — `parser.go:64-65,253-278` (`type_item`)
8. **Macro definitions** — `parser.go:57-58,306-323` (`macro_definition`)
9. **Imports** — `parser.go:53-54,359-387` (`use_declaration`), with macro-invocation imports
10. **Function calls** — `parser.go:55-56,93-125` (`call_expression`, `macro_invocation`)
11. **Annotations** (nested field/enum-variant attributes) — `parser.go:66-71`, `nested_attributes.go:13-103`
12. **Dead-code root kinds** (`helpers.go:172-189`, `parser.go:215-225`, `metadata.go:84-89`):
    - `rust.main_function` (in `src/main.rs`, `src/bin/`, `examples/`, `build.rs`, or with `#[tokio::main]`)
    - `rust.test_function` (`#[test]`)
    - `rust.tokio_main` (`#[tokio::main]`)
    - `rust.tokio_test` (`#[tokio::test]`)
    - `rust.public_api_item` (exact `pub` visibility)
    - `rust.benchmark_function` (`#[bench]`, `#[divan::bench]`, `criterion_group!`)
    - `rust.trait_impl_method` (methods inside `impl Trait for Type`)
13. **Lifetime evidence** — `lifetimes.go:19-139` (`lifetime_parameters`, `signature_lifetimes`, `return_lifetime`)
14. **Generic metadata** — `metadata.go:102-113` (`type_parameters`, `const_parameters`, `lifetime_parameters`)
15. **Where clause predicates** — `where.go:8-31` (`where_predicates`, `associated_type_constraints`, `higher_ranked_trait_bounds`)
16. **Attribute metadata** — `metadata.go:20-52` (`attribute_paths`, `derives`, `conditional_derives`, `decorators`)
17. **Exactness blockers** — `metadata.go:56-67` (`cfg_unresolved`, `macro_expansion_unavailable`)
18. **Visibility** — `helpers.go:158-170` (pub, pub(crate), scoped)
19. **Async/unsafe flags** — `parser.go:204-209`
20. **Module resolution** — `module_resolution.go:24-62` (`ResolveModuleRowFileCandidates`)
21. **Cargo cfg manifest** — `cargo_cfg.go:26-77` (package name, workspace members, features)
22. **Cargo.toml dependencies** — `cargo_dependencies.go:65-139` (runtime/dev/build/target scope, workspace inheritance)
23. **Cargo.lock dependencies** — `cargo_lock_dependencies.go:22-55` (exact versions, dependency chains)
24. **Call receiver type inference** — `receiver_types.go:13-25`
25. **Cyclomatic complexity** — `complexity.go:30-31`
26. **PreScan** — `parser.go:95-101`

## Verified-by-Test Constructs
List constructs verified by tests, with file:function references.

1. **Public API root marking** — `rust/root_metadata_test.go:12-41` (`TestParseMarksExactPubRustItemsAsPublicAPIRoots`): functions, structs, traits, type_aliases
2. **Benchmark function roots** — `rust/root_metadata_test.go:44-79` (`TestParseMarksRustBenchmarkRootsConservatively`): criterion_group!, bench attr, divan::bench attr, helper exclusion
3. **Module declaration path candidates** — `rust/root_metadata_test.go:82-102` (`TestParseCapturesRustModuleDeclarationPathCandidates`): inline vs declaration modules
4. **Import visibility preservation** — `rust/root_metadata_test.go:104-123` (`TestParsePreservesRustPubUseImportVisibility`): pub use vs bare use
5. **Multiline attributes** — `rust/metadata_test.go:12-34` (`TestParseCapturesMultilineRustAttributes`): multiline cfg_attr/derive
6. **Attribute deduplication** — `rust/metadata_test.go:36-53` (`TestParseDoesNotDuplicateRustItemAttributes`)
7. **Trait impl blocks with generics** — `rust/metadata_test.go:54-76` (`TestParseTrimsMultilineImplWhereClauseFromTarget`): `impl AsyncRead for Compat<T>` and the trimmed receiver target
8. **Conditional derives and nested attributes** — `rust/remaining_maturity_test.go:12-61` (`TestParseCapturesRustConditionalDerivesAndNestedAttributes`): field and enum_variant annotations
9. **Where clause semantics** — `rust/remaining_maturity_test.go:64-104` (`TestParseCapturesRustWhereClauseSemantics`): predicates, associated_type_constraints, higher_ranked_trait_bounds
10. **Lifetime AST parity** (7 subtests) — `rust/lifetime_ast_test.go:17-111` (`TestRustLifetimeASTParity`): `lifetime_parameters`, `signature_lifetimes`, `return_lifetime`
11. **Impl block lifetime fields** — `rust/lifetime_ast_test.go:115-145` (`TestRustImplLifetimeASTParity`)
12. **Cyclomatic complexity** — `engine_cyclomatic_complexity_test.go:118-131` (2 Rust test cases)
13. **Rust module resolution** — `rust/engine_rust_module_resolution_test.go` (external `rust_test`, black-box parent Engine)
14. **Rust lifetimes** — `rust/engine_rust_lifetimes_test.go` (external `rust_test`, black-box parent Engine)
15. **Async/unsafe flags and Tokio roots** — `rust/parser_test.go:155-204` (`TestParseCapturesRustDogfoodMaturityPatterns`): `async`, `unsafe`, `rust.tokio_main`, `rust.tokio_test`, and ordinary test roots
16. **Const generic metadata and call receiver inference** — `rust/parser_test.go:297-367` (`TestParseCapturesRustAttributesAndGenericParameters`): `const_parameters`, multiple derives, and `inferred_obj_type`
17. **Path-attribute and macro-origin module/import metadata** — `rust/remaining_maturity_test.go:106-149` (`TestParseCapturesRustPathAttributeModuleAndMacroDeclarations`): `module_path_source`, `module_origin`, `import_origin`, aliases, candidates, and exactness blockers
18. **Cargo.toml dependency evidence** — `rust/rust_cargo_dependency_test.go:16-78` (`TestDefaultEngineParsePathCargoTomlEmitsDependencyEvidence`): runtime/dev/build/target scopes, workspace inheritance, renamed packages, versions, and target cfg
19. **Cargo.lock versions and proven chains** — `rust/rust_cargo_dependency_test.go:80-142` (`TestDefaultEngineParsePathCargoLockEmitsExactVersionsAndProvenChains`): direct/transitive depth and unreachable packages without invented paths
20. **Cargo.lock source-qualified identity** — `rust/rust_cargo_dependency_test.go:144-207` (`TestDefaultEngineParsePathCargoLockUsesSourceQualifiedDependencies`): same-name packages from distinct sources do not cross-contaminate reachability
21. **Malformed Cargo input and coverage registry** — `rust/rust_cargo_dependency_test.go:209-267` (`TestDefaultEngineParsePathCargoRejectsMalformedDependencyFiles`, `TestCargoDependencyCoverageMatrixMarksCargoFilesCovered`)

## Unverified / Claimed-but-Untested Constructs
List constructs claimed but not covered by any test.

1. **`function_signature_item` (extern blocks)** (`parser.go:47`): no test for `extern "C" { fn foo(); }`.
2. **`union_item`** (`parser.go:49`): no Rust union fixture asserts the emitted class row.
3. **`rust.main_function` with `build.rs` or `examples/` paths** (`helpers.go:300-305`): current root tests prove `src/main.rs`, not these two Cargo entrypoint locations.

## Edge Cases Considered
List edge cases the tests actually cover with test references.

- **Multiline attributes** (multi-line `#[...]`) — `rust/metadata_test.go:15-30`
- **Attribute deduplication** (same attr on inline and leading pass) — `rust/metadata_test.go:38-52`
- **Trait impl with generic type parameter** — `rust/metadata_test.go:57-76`
- **Conditional derives (`cfg_attr` derive)** — `rust/remaining_maturity_test.go:15-38`
- **Nested field attributes (`#[serde(skip)]`)** — `rust/remaining_maturity_test.go:41-44`
- **Nested enum variant attributes** — `rust/remaining_maturity_test.go:52-55`
- **Where clause with associated_type_constraints** — `rust/remaining_maturity_test.go:73-86`
- **Higher-ranked trait bounds** — `rust/remaining_maturity_test.go:92-105`
- **Lifetime parameters from `type_parameters` field** — `rust/lifetime_ast_test.go:17-55` (7 subtests)
- **Return lifetime from `return_type` field** — `rust/lifetime_ast_test.go:54-75`
- **Body lifetime exclusion** (char literal `'a'` in body) — `rust/lifetime_ast_test.go:77-91`
- **Impl block lifetimes** — `rust/lifetime_ast_test.go:117-144`
- **Scoped visibility (`pub(crate)`) is not `pub`** — `rust/root_metadata_test.go:34-41`
- **Inline vs declaration module kinds** — `rust/root_metadata_test.go:85-100`
- **Criterion group targets vs expression-based** — `rust/root_metadata_test.go:56-68`
- **`decorators` field present on functions** — tested by attribute tests, always `[]string{}` minimum
- **Async/unsafe flags and Tokio test roots** — `rust/parser_test.go:155-204`
- **Call receiver type inference from a typed parameter** — `rust/parser_test.go:320-363`
- **Path attributes and macro-origin module/import rows** — `rust/remaining_maturity_test.go:106-149`
- **Cargo manifest scopes, workspace inheritance, renamed packages, and inline-table dependencies** — `rust/rust_cargo_dependency_test.go:16-78`
- **Cargo lock direct/transitive chains, source-qualified identity, and packages without dependency lists** — `rust/rust_cargo_dependency_test.go:80-207`
- **Malformed Cargo.toml and Cargo.lock input** — `rust/rust_cargo_dependency_test.go:209-250`

## Edge Cases NOT Considered
List edge cases not tested.

- **Rust 2024 edition syntax** (e.g., `async` trait methods, RPIT)
- **`extern "C"` block function declarations** (`function_signature_item`)
- **`macro_rules!` with no name** (ERROR node in grammar)
- **Very deeply nested use tree expansion** (`use a::{b::{c::{d, e}}}`)
- **`where` clause at end of signature** (before `;` but after `}`)
- **Cargo.lock version 4 format** (post-2024 Cargo)
- **`criterion_group!` with `targets =` named form** — tested implicitly but not isolated
- **Path-based module with non-`.rs` extension in `#[path = "..."]`**
- **Struct with both `field_declaration_list` and `enum_variant_list` as children**

## Verdict
deep

The Rust parser has child-package proof for its syntax metadata and black-box parent-Engine proof for lifetimes, module resolution, routes, and Cargo dependency files. The Cargo suite covers manifest scopes and aliases, exact lockfile versions, reachability chains, source-qualified identity, malformed input, and the dependency coverage registry. Shared engine behavior and cyclomatic-complexity cases remain at the parent parser root. The remaining construct gaps are extern-block function signatures, Rust unions, and the `build.rs` / `examples/` variants of main-function root detection.

## Recommended Actions
1. Add an external-function test for `extern "C" { fn foo(); }`.
2. Add a Rust `union` fixture and assert its emitted class row.
3. Add a main-root path matrix for `build.rs` and `examples/*.rs`.
4. Add a Cargo.lock version 4 compatibility fixture.
