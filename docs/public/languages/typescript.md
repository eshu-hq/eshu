# TypeScript Parser

This page describes the current Go parser and query contract for TypeScript.

## Parser Contract

| Field | Value |
| --- | --- |
| Language | `typescript` |
| Parser | `DefaultEngine (typescript)` |
| Entrypoint | `go/internal/parser/javascript_language.go` |
| Fixture repo | `tests/fixtures/ecosystems/typescript_comprehensive/` |
| Main parser tests | `go/internal/parser/engine_javascript_semantics_test.go`, `go/internal/parser/engine_typescript_advanced_semantics_test.go` |
| Runtime validation | Compose-backed fixture verification; see [Local Testing](../reference/local-testing.md) |

## Supported Surfaces

| Surface | Current contract |
| --- | --- |
| Source entities | Functions, classes, interfaces, imports, variables, enums, modules, namespaces, type aliases, and declaration-merge groups. |
| Type metadata | Type parameters, mapped and conditional type aliases, decorators, type references, and declaration-merge metadata. |
| Framework and package roots | JavaScript-family Node package, React, Next.js, Express, Hapi, AWS SDK, and GCP SDK packs. |
| Query surfacing | `code/language-query`, `code/search`, entity resolve/context, relationships, complexity, and dead-code responses preserve TypeScript metadata when graph or content rows carry it. |

## Dead-Code Support

TypeScript dead-code support is `derived`. Modeled roots include Node package
entrypoints, `bin` targets, scripts, exports, declaration barrels, one-hop
static reexports, module-contract exports, public methods with `implements`
evidence, Next.js routes, and supported server framework handlers.

It is not cleanup-safe exact truth. Runtime-built imports, property dispatch,
decorator/container behavior, plugin loading, declaration-surface precision, and
broad package export surfaces remain blockers.

TSX uses the same TypeScript-family query path but has separate React wrapper
coverage.

## Framework And Library Support

Supported today:

- JavaScript-family framework roots apply to TypeScript when the pattern is
  represented in parseable source or package metadata.
- Supported roots include Next.js routes, Express, Koa, Fastify, NestJS, Hapi,
  package entrypoints, package exports, scripts, migrations, and seeds.
- TypeScript adds interface implementations, module-contract exports, public
  API exports and reexports, and public API type references.

Not claimed today:

- Decorator/container behavior is not modeled as whole-framework reachability.
- Dynamic imports, plugin loading, runtime property dispatch, and broad package
  declaration surfaces remain exactness blockers.

## Related Docs

- [TypeScript JSX Parser](typescriptjsx.md)
- [Dead Code Language Maturity](../reference/dead-code-language-maturity.md)
- [Parser Support Matrix](support-maturity.md)
