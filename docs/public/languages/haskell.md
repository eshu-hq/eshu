# Haskell Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `haskell`
- Family: `language`
- Parser: `DefaultEngine (haskell)`
- Entrypoint: `go/internal/parser/perl_haskell_language.go`
- Fixture repo: `tests/fixtures/ecosystems/haskell_comprehensive/`
- Unit test suite: `go/internal/parser/engine_long_tail_test.go`
- Integration validation: compose-backed fixture verification (see `../reference/local-testing.md`)

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Function declarations | `function-declarations` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/haskell/parser_test.go::TestParseCapturesHaskellBuckets` | Compose-backed fixture verification | Multi-equation definitions fingerprint over every equation ([#6865](https://github.com/eshu-hq/eshu/issues/6865)) |
| Initializer declarations | `initializer-declarations` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathHaskellFixtures` | Compose-backed fixture verification | - |
| Type classes | `type-classes` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/haskell/parser_test.go::TestParseCapturesHaskellDeadCodeRootsAndCalls` | Compose-backed fixture verification | - |
| Data types (struct-like) | `data-types-struct-like` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/haskell/parser_test.go::TestParseCapturesHaskellDeadCodeRootsAndCalls` | Compose-backed fixture verification | - |
| Enumerations | `enumerations` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathHaskellFixtures` | Compose-backed fixture verification | - |
| Protocols/typeclasses | `protocols-typeclasses` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/haskell/parser_test.go::TestParseCapturesHaskellDeadCodeRootsAndCalls` | Compose-backed fixture verification | - |
| Import declarations | `import-declarations` | supported | `imports` | `name, line_number` | `relationship:IMPORTS` | `go/internal/parser/haskell/parser_test.go::TestParseCapturesHaskellDeadCodeRootsAndCalls` | Compose-backed fixture verification | - |
| Function call expressions | `function-call-expressions` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/haskell/parser_test.go::TestParseCapturesHaskellDeadCodeRootsAndCalls` | Compose-backed fixture verification | Bounded lexical calls from function right-hand sides |
| Property/binding declarations | `property-binding-declarations` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathHaskellFixtures` | Compose-backed fixture verification | - |
| Declaration spans | `declaration-spans` | supported | `classes, functions, variables` | `name, line_number, end_line` | `node:Class, node:Function, node:Variable` | `go/internal/parser/haskell/ast_extract_test.go::TestParseRecordsFullDeclarationSpans`, `go/internal/parser/haskell/ast_extract_test.go::TestParseRecordsFullWhereLocalSpan` | Compose-backed fixture verification | Data/newtype/type declarations, typeclasses, typeclass methods, and where-block locals record their full `end_line` span. |

## Dead-code Support

Haskell dead-code support is `derived`. Parser metadata marks `main`, explicit
module-exported functions and types, typeclass method declarations, and instance
methods as root kinds consumed by the dead-code query surface. Function-call
evidence is lexical and bounded to definition right-hand sides, so it supports
triage but does not claim compiler-equivalent Haskell reachability.

Exact cleanup remains blocked by Template Haskell expansion, CPP conditional
compilation, Cabal component resolution, implicit module export surfaces,
typeclass dispatch, module re-export resolution, and foreign-function interface
callbacks.

## Framework And Library Support

Supported today:

- This parser does not claim framework-level support.
- Derived root evidence includes `main`, explicit module exports, typeclass
  method declarations, and instance methods.

Not claimed today:

- Template Haskell, Cabal component selection, implicit exports, typeclass
  dispatch, module reexports, and FFI callback reachability remain exactness
  blockers.

## Runtime Version Declarations

The git collector also emits `runtime_declaration.version` facts for
runtime `haskell` from two manifests: a Stack `stack.yaml` (the root
`resolver` string scalar, `source_kind` `stack_yaml_resolver`) and a Cabal
`*.cabal` file (the column-zero `tested-with` literal specs, `source_kind`
`cabal_tested_with`, read through the content-only path with no snapshot
digest binding). The resolver is the only version declaration the
`stack.yaml` side owns; every other `stack.yaml` key stays out of runtime
truth.

| Manifest | Declared key | Emitted declaration |
| --- | --- | --- |
| `stack.yaml` (exact basename) | root `resolver` plain string scalar | `stack_yaml_resolver` (`haskell`), first effective line |
| `*.cabal` (extension match) | column-zero `tested-with` literal specs | `cabal_tested_with` (`haskell`), per-entry line |

Only the exact `stack.yaml` name matches, so `stack.yaml.lock`,
`cabal.project`, and freeze files never match; only `*.cabal` files match
the cabal side. Only a plain string scalar
declares a resolver: a custom-snapshot mapping or any other shape is a
counted rejection, never a version. A file without a `resolver` key is
absent, distinct from a malformed file, which stays a decode error. A
CloudFormation template carrying the `stack.yaml` name falls through to the
generic YAML handling and emits no Stack evidence. Values the credential
guard rejects are counted drops, never facts. Parser-private evidence is
stripped from the parsed payload before content storage and bound to the
exact source bytes by digest, so a digest mismatch retries instead of
emitting stale truth.

Evidence pointers: `go/internal/parser/yaml/runtime_evidence.go`
(`stackEvidenceFromDecoded`, `ExtractStackRuntimeEvidence`),
`go/internal/parser/cabal/cabal.go`,
`go/internal/collector/repo/runtime/declaration/haskell.go`
(`ExtractHaskellEvidence`, `ExtractHaskellCabalEvidence`),
`go/internal/collector/repo/git/haskell_runtime_handoff.go`,
`go/internal/collector/repo/git/parser_fact_export.go`
(`noteHaskellCandidate`). Pinned by
`go/internal/parser/yaml/runtime_evidence_test.go::TestStackResolverEvidence`,
`go/internal/parser/yaml/runtime_evidence_test.go::TestStackYamlCloudFormationFallsThrough`,
`go/internal/collector/repo/git/haskell_runtime_handoff_test.go::TestHaskellRuntimeNativeHandoff`,
`go/internal/collector/repo/git/haskell_runtime_handoff_test.go::TestStackYamlCloudFormationCandidateFallsThrough`,
`go/internal/collector/repo/runtime/declaration/haskell_test.go::TestStackManifestSpecAndFailClosedCases`,
`go/internal/collector/repo/runtime/declaration/haskell_test.go::TestStackDeclarationLineAndKind`,
`go/internal/collector/repo/runtime/declaration/haskell_test.go::TestStackLockfilesNotAdmitted`,
`go/internal/collector/repo/runtime/declaration/haskell_test.go::TestHaskellCabalManifestContentOnlyDispatch`,
`go/internal/collector/repo/runtime/declaration/haskell_test.go::TestHaskellCabalManifestSkips`,
and
`go/internal/parser/registry_haskell_cabal_test.go::TestRegistryAdmitsHaskellCabalMetadataOnly`.

## Known Limitations
- Type class instances are not modeled as inheritance relationships
- Modules without explicit export lists do not automatically root every
  top-level declaration as public API
- Where-clauses and let-bindings define local names that are not separately
  graphed
- Point-free style definitions may result in functions with no parameter information
