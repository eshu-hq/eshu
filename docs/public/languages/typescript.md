# TypeScript Parser

This page describes the current Go parser and query contract for TypeScript.
Detailed parser mechanics live in `go/internal/parser/README.md` and
`go/internal/parser/javascript_language.go`.

## Parser Contract

| Field | Value |
| --- | --- |
| Language | `typescript` |
| Family | `language` |
| Parser | `DefaultEngine (typescript)` |
| Entrypoint | `go/internal/parser/javascript_language.go` |
| Fixture repo | `tests/fixtures/ecosystems/typescript_comprehensive/` |
| Main parser tests | `go/internal/parser/engine_javascript_semantics_test.go`, `go/internal/parser/engine_typescript_advanced_semantics_test.go` |
| Runtime validation | Compose-backed fixture verification; see [Local Testing](../reference/local-testing.md) |

## Supported Surfaces

The TypeScript path supports the JavaScript-family declaration and framework
surface plus TypeScript-specific type metadata.

| Surface | Current contract |
| --- | --- |
| Source entities | Functions, classes, interfaces, imports, variables, enums, modules, namespaces, type aliases, and declaration-merge groups. |
| Type metadata | Type parameters, mapped and conditional type aliases, decorators, type references, and declaration-merge metadata. |
| Graph-backed queries | `code/language-query`, `code/search`, `entities/resolve`, entity-context, `code/relationships`, `code/complexity`, and dead-code responses preserve TypeScript semantic metadata when graph or content rows carry it. |
| Framework and package roots | Shares the JavaScript-family Node package, React, Next.js, Express, Hapi, AWS SDK, and GCP SDK framework packs. |

Primary proof:

- `go/internal/parser/engine_typescript_advanced_semantics_test.go`
- `go/internal/query/typescript_graph_metadata_test.go`
- `go/internal/query/language_query_graph_first_test.go`
- `go/internal/reducer/semantic_entity_materialization_typescript_test.go`
- `go/internal/storage/cypher/semantic_entity_test.go`

## Dead-Code Support

TypeScript dead-code support is `derived`. Eshu models source-proven roots and
reference evidence, then keeps cleanup truth conservative where runtime loading
or broad public API surfaces can keep symbols live.

Modeled roots and evidence include:

- Node package entrypoints, `bin` targets, scripts, exports, and declaration
  or public-surface files.
- Declaration-only barrels, one-hop static reexports, module-contract exports,
  exported static registry members, and public methods on classes with
  `implements` evidence.
- Next.js app/route exports plus Express, Koa, Fastify, NestJS, and Hapi
  handler or plugin surfaces.
- `typescript.type_reference` rows, returned function values, static relative
  reexports, CommonJS/Hapi handler references, and JSONC `tsconfig.json`
  `baseUrl`/`paths` parsing.

Focused coverage lives in
`go/internal/parser/javascript_dead_code_node_typescript_fixture_test.go`,
`go/internal/parser/javascript_dead_code_typescript_surface_test.go`,
`go/internal/query/code_dead_code_typescript_semantics_test.go`, and
`go/internal/query/code_dead_code_node_typescript_matrix_test.go`.

## Support Maturity

| Dimension | Status |
| --- | --- |
| Grammar routing | `supported` |
| Normalization | `supported` |
| Framework packs | `react-base`, `nextjs-app-router-base`, `express-base`, `hapi-base`, `aws-sdk-base`, `gcp-sdk-base` |
| Query surfacing | `supported` |
| Real-repo validation | `supported` |
| End-to-end indexing | `supported` |
| Dead-code exactness | `derived`, not cleanup-safe exact truth |

## Known Limitations

- Runtime-built imports, property dispatch, decorators with container-specific
  behavior, framework plugin loading, declaration-surface precision, and broad
  package export surfaces remain outside the exactness boundary.
- TSX uses the same TypeScript-family query path but has separate React wrapper
  coverage; see the parser support matrix for TSX status.
