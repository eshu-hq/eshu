# JavaScript Parser

This page describes the current Go parser and query contract for JavaScript.
Detailed parser mechanics live in `go/internal/parser/README.md` and
`go/internal/parser/javascript_language.go`.

## Parser Contract

| Field | Value |
| --- | --- |
| Language | `javascript` |
| Family | `language` |
| Parser | `DefaultEngine (javascript)` |
| Entrypoint | `go/internal/parser/javascript_language.go` |
| Fixture repo | `tests/fixtures/ecosystems/javascript_comprehensive/` |
| Main parser tests | `go/internal/parser/engine_javascript_semantics_test.go` |
| Runtime validation | Compose-backed fixture verification; see [Local Testing](../reference/local-testing.md) |

## Supported Surfaces

| Surface | Current contract |
| --- | --- |
| Source entities | Function declarations and expressions, named arrow functions, methods, classes, imports, calls, member calls, variables, generator functions, JSDoc, and method-kind metadata. |
| Framework and package roots | Node package entrypoints, CommonJS exports, Next.js routes/app exports, Express, Koa, Fastify, NestJS, Hapi, AMQP consumers, migration exports, and seed-style execute functions. |
| Graph-backed queries | `code/language-query`, `code/search`, `entities/resolve`, entity-context, `code/call-chain`, `code/relationships`, `code/complexity`, and dead-code responses preserve JavaScript semantic metadata when graph or content rows carry it. |
| Semantic metadata | JSDoc, generator signal, getter/setter/async method kind, semantic summaries, `semantic_profile`, and `javascript_semantics`. |

Primary proof:

- `go/internal/parser/engine_test.go::TestDefaultEngineParsePathJavaScript`
- `go/internal/parser/engine_javascript_semantics_test.go`
- `go/internal/query/javascript_semantics_test.go`
- `go/internal/query/language_query_graph_first_test.go`
- `go/internal/query/code_dead_code_javascript_roots_test.go`

## Dead-Code Support

JavaScript dead-code support is `derived`. Eshu models source-proven package,
module, framework, and reference evidence. It does not guess through
runtime-built property names or dynamic `require()` targets.

Modeled roots and evidence include:

- Node package entrypoints, `bin` targets, scripts, package exports, and
  CommonJS default or mixin-style exports.
- Next.js app/route exports and Express, Koa, Fastify, NestJS, and Hapi handler
  or plugin roots.
- Hapi AMQP consumers, route-array handlers, proxy callbacks, migration
  exports, and seed-style execute functions.
- Returned function values, static relative reexports, Hapi handler references,
  CommonJS property require aliases, and bounded constructor receiver evidence.

Focused coverage lives in
`go/internal/parser/javascript_dead_code_node_roots_test.go`,
`go/internal/parser/javascript_dead_code_hapi_alias_test.go`,
`go/internal/parser/javascript_dead_code_commonjs_class_test.go`,
`go/internal/query/code_dead_code_javascript_roots_test.go`, and
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

- Runtime-dependent computed expressions, dynamic `require()` targets,
  runtime plugin loading, package export breadth, and declaration/API precision
  remain outside the exactness boundary.
