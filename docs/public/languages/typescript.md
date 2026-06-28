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
| Framework and package roots | JavaScript-family Node package, React, Next.js, Express, Koa, Fastify, NestJS, Hapi, AWS SDK, and GCP SDK packs. |
| Query surfacing | `code/language-query`, `code/search`, entity resolve/context, relationships, complexity, and dead-code responses preserve TypeScript metadata when graph or content rows carry it. |

## Capability Claim Ledger

| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Source entities | `source-entities` | supported | parser buckets | `name, line_number` where applicable | `execute_language_query` | `go/internal/parser/engine_typescript_advanced_semantics_test.go` | Compose-backed fixture verification | Tree-sitter-backed TypeScript entity extraction. |
| Type metadata | `type-metadata` | supported | parser metadata buckets | type/decorator metadata where applicable | `execute_language_query` | `go/internal/parser/engine_typescript_advanced_semantics_test.go`, `go/internal/query/typescript_graph_metadata_test.go` | Compose-backed fixture verification | Deterministic TypeScript metadata, no provider key. |
| JavaScript-family roots | `javascript-family-roots` | supported | `dead_code_root_kinds`, package metadata, framework metadata | source-proven root kind and location | `find_dead_code` | `go/internal/query/code_dead_code_node_typescript_matrix_test.go`, `go/internal/query/code_dead_code_typescript_semantics_test.go` | Compose-backed fixture verification | Derived root evidence applies the JavaScript-family root model to TypeScript. |
| Express/Hapi route truth | `express-hapi-route-truth` | supported | `framework_semantics.route_entries` | `method, path`; `handler` only for exact named handlers | `HANDLES_ROUTE` when reducer can resolve the exact handler | `go/internal/parser/engine_javascript_ast_conversion_test.go::TestDefaultEngineParsePathExpressESMRoutesFromAST`, `go/internal/reducer/handles_route_intents_test.go` | Shared reducer route projection proof | TypeScript uses the JavaScript parser path for exact Express/Hapi route entries. |
| Next.js route-handler truth | `nextjs-route-handler-truth` | partial | - | - | - | `go/internal/parser/engine_javascript_ast_conversion_test.go::TestDefaultEngineParsePathNextJSRouteSurfaceFromAST` | Explicit root-vs-route wording on this page | Next.js route/app exports are root evidence today, not `route_entries` or `HANDLES_ROUTE` truth; tracked by #4095. |
| Koa/Fastify/NestJS route truth | `koa-fastify-nestjs-route-truth` | partial | - | - | - | `go/internal/query/code_dead_code_javascript_roots_test.go` | Explicit root-vs-route wording on this page | Koa, Fastify, and NestJS are modeled as framework roots today, not exact route entries or handler bindings; tracked by #4094. |
| Query surfacing | `query-surfacing` | supported | graph/content query rows | TypeScript metadata when present | `code/language-query`, `get_code_relationship_story`, `find_dead_code` | `go/internal/query/typescript_graph_metadata_test.go` | Compose-backed fixture verification | Query paths preserve TypeScript metadata when graph or content rows carry it. |
| Outbound contracts | `outbound-contracts` | partial | - | - | - | `go/internal/parser/engine_javascript_semantics_test.go` | Explicit unsupported-contract wording on this page | SDK/client evidence does not create deterministic cross-repo outbound contract edges today. |
| Decorator/container behavior | `decorator-container-behavior` | partial | - | - | - | Support-maturity guardrails | Explicit decorator/container wording on this page | Decorators are metadata today, not whole-framework runtime reachability truth. |
| Generated clients | `generated-clients` | partial | - | - | - | Support-maturity guardrails | Explicit generated-client wording on this page | Generated clients and runtime route/client manifests are not parser-owned route or contract truth. |
| Dead-code roots | `dead-code-roots` | derived | `dead_code_root_kinds` | modeled root kind and source location | `find_dead_code` | `go/internal/query/code_dead_code_node_typescript_matrix_test.go` | Compose-backed fixture verification | Derived liveness roots are not cleanup-safe exact truth. |

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
- Express and Hapi route registrations emit `route_entries`; `handler` is
  recorded only for exact named handlers so the reducer can project exact
  `HANDLES_ROUTE` edges without guessing.
- Next.js routes, Koa, Fastify, and NestJS are modeled as root evidence, but do
  not emit exact route entries or handler bindings today; follow-up work is
  tracked in #4095 and #4094.
- Supported roots include package entrypoints, package exports, scripts,
  migrations, and seeds.
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
