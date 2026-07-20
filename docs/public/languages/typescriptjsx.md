# TypeScript JSX Parser

This page describes the current Go parser and query contract for TSX.

## Parser Contract

| Field | Value |
| --- | --- |
| Language | `typescriptjsx` |
| Parser | `DefaultEngine (tsx)` |
| Entrypoint | `go/internal/parser/javascript_language.go` |
| Fixture repo | `tests/fixtures/ecosystems/tsx_comprehensive/` (loaded and exercised by `go/internal/parser/prescan_derive_test.go::TestDerivePreScanNamesMatchesLegacyPreScan`) |
| Main parser tests | `go/internal/parser/engine_javascript_semantics_test.go`, `go/internal/parser/engine_tsx_advanced_semantics_test.go`, `go/internal/parser/engine_tsx_component_wrapper_test.go` |
| Runtime validation | Compose-backed fixture verification; see [Local Testing](../reference/local-testing.md) |

## Supported Surfaces

| Surface | Current contract |
| --- | --- |
| Source entities | Functions, classes, interfaces, imports, calls, variables, type aliases, and component entities. |
| React evidence | PascalCase JSX usage, component wrappers, `React.FC`, `React.FunctionComponent`, `lazy(...)`, fragment shorthand, and component type narrowing metadata. |
| Graph-backed query | TSX `Component`, `TypeAlias`, `Function`, and `Variable` rows can preserve semantic metadata on language-query, search, resolve, context, story, relationship, and dead-code surfaces. |
| Compatibility | Query code still normalizes older graph-backed JSX `CALLS` edges with `call_kind=jsx_component` where needed. |

## Dead-Code Support

TSX dead-code support is `derived`. It uses JavaScript-family package and
framework roots plus TSX component metadata. Runtime-built component names and
framework behavior that is not represented in source or package metadata remain
outside the exactness boundary.

## Framework And Library Support

Supported today:

- React component evidence includes PascalCase JSX usage, component wrappers,
  `React.FC`, `React.FunctionComponent`, `lazy(...)`, fragment shorthand, and
  component type narrowing metadata.
- Next.js app and route exports use the JavaScript-family root model.

Not claimed today:

- Runtime-built component names, framework plugin loading, generated route
  maps, and JSX indirection that is not represented in source remain outside
  the exactness boundary.
- A TSX file larger than 1 MiB has its tree-sitter parse skipped entirely in
  the normal parse stage (the shared javascript-family parser bounds
  JavaScript, TypeScript, and TSX identically); see
  [JavaScript Parser](javascript.md#known-limitations) for the bound, which
  also covers the repository pre-scan stage (#4766,
  [#4808](https://github.com/eshu-hq/eshu/issues/4808)).

## Related Docs

- [TypeScript Parser](typescript.md)
- [Dead Code Language Maturity](../reference/dead-code-language-maturity.md)
- [Parser Support Matrix](support-maturity.md)
