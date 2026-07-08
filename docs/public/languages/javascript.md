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
| Repository pre-scan | Function and class names, function-valued object members, static computed object keys, and CommonJS property or subscript export assignments such as `module.exports.name = ...`, `module.exports['name'] = ...`, and `exports.name = ...` feed the deterministic import map used by reducer code-call resolution. |

## Capability Claim Ledger

| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Source entities | `source-entities` | supported | parser buckets | `name, line_number` where applicable | `execute_language_query` | `go/internal/parser/engine_javascript_semantics_test.go` | Compose-backed fixture verification | Tree-sitter-backed JavaScript entity extraction. |
| Graph-backed queries | `graph-backed-queries` | supported | graph/content query rows | JavaScript entity metadata when graph rows carry it | `code/language-query`, `get_code_relationship_story`, `find_dead_code` | `go/internal/query/language_query_graph_first_test.go`, `go/internal/query/javascript_semantics_test.go` | Compose-backed fixture verification | Query paths preserve JavaScript metadata from graph and content rows. |
| Semantic metadata | `semantic-metadata` | supported | parser metadata buckets | JSDoc, method kind, semantic profile, and JavaScript semantics | `execute_language_query` | `go/internal/parser/engine_javascript_semantics_test.go`, `go/internal/query/javascript_semantics_test.go` | Compose-backed fixture verification | Deterministic JavaScript metadata is emitted without provider keys. |
| Framework/package roots | `framework-package-roots` | supported | `dead_code_root_kinds`, package metadata, framework metadata | source-proven root kind and location | `find_dead_code` | `go/internal/query/code_dead_code_javascript_roots_test.go` | Compose-backed fixture verification | Derived root evidence protects live framework/package surfaces without claiming cleanup-safe exactness. |
| Express/Hapi route truth | `express-hapi-route-truth` | supported | `framework_semantics.route_entries` | `method, path`; `handler` only for exact named handlers | `HANDLES_ROUTE` when reducer can resolve the exact handler | `go/internal/parser/engine_javascript_route_handler_test.go`, `go/internal/parser/engine_javascript_handler_test.go`, `go/internal/parser/engine_javascript_ast_conversion_test.go::TestDefaultEngineParsePathExpressESMRoutesFromAST`, `go/internal/parser/engine_javascript_ast_conversion_test.go::TestDefaultEngineParsePathHapiRoutesNestedConfigFromAST`, `go/internal/reducer/handles_route_intents_test.go` | Shared reducer route projection proof | Express/Hapi are the JavaScript route frameworks with exact `route_entries` and exact-only handler binding today. |
| Next.js route-handler truth | `nextjs-route-handler-truth` | supported | `framework_semantics.nextjs.route_entries` | `method, path, handler` for app-router exported HTTP method handlers; `ANY, path, handler` for named `pages/api` default exports | `HANDLES_ROUTE` when reducer can resolve the exact handler | `go/internal/parser/engine_javascript_nextjs_route_entries_test.go`, `go/internal/reducer/handles_route_intents_test.go` | Shared reducer route projection proof | Exact Next.js route handlers emit route entries; page/layout roots, anonymous defaults, rewrites, middleware matchers, generated manifests, and plugin conventions do not fabricate handler edges. |
| Koa/Fastify/NestJS route truth | `koa-fastify-nestjs-route-truth` | supported | `framework_semantics.{koa,fastify,nestjs}.route_entries` | `method, path`; `handler` only for exact named handlers or methods | `HANDLES_ROUTE` when reducer can resolve the exact handler | `go/internal/parser/engine_javascript_koa_fastify_nestjs_route_entries_test.go`, `go/internal/reducer/handles_route_javascript_frameworks_test.go`, `go/internal/query/content_reader_framework_routes_test.go` | Parser-to-reducer-to-query route-entry proof | Literal Koa router calls, Fastify verb/route-object registrations, and NestJS literal controller/method decorators emit exact route entries. Middleware chains, plugin loading, computed paths/methods, generated routes, and DI/container-only behavior stay unclaimed. |
| Outbound contracts | `outbound-contracts` | partial | - | - | - | `go/internal/parser/engine_javascript_semantics_test.go` | Explicit unsupported-contract wording on this page | SDK/client evidence does not create deterministic cross-repo outbound contract edges today. |
| Generated clients | `generated-clients` | partial | - | - | - | Support-maturity guardrails | Explicit generated-client wording on this page | Generated clients and runtime route/client manifests are not parser-owned route or contract truth. |
| Runtime dynamic routes | `runtime-dynamic-routes` | partial | - | - | - | Support-maturity guardrails | Explicit dynamic-route wording on this page | Dynamic imports, plugin loading, computed dispatch, and runtime route registration remain outside exact truth. |
| Dead-code roots | `dead-code-roots` | derived | `dead_code_root_kinds` | modeled root kind and source location | `find_dead_code` | `go/internal/query/code_dead_code_javascript_roots_test.go` | Compose-backed fixture verification | Derived liveness roots are not cleanup-safe exact truth. |

Primary proof:

- `go/internal/parser/engine_test.go::TestDefaultEngineParsePathJavaScript`
- `go/internal/parser/engine_javascript_semantics_test.go`
- `go/internal/parser/prescan_compat_test.go::TestPreScanMatchesParseDeclarationNames`
- `go/internal/parser/engine_javascript_koa_fastify_nestjs_route_entries_test.go`
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
| Framework and root evidence | React/TSX evidence, Next.js routes/app exports, Express, Koa, Fastify, NestJS, Hapi, AMQP consumers, package/bin/exports, migrations, seeds, AWS/GCP SDK evidence |
| Query surfacing | `supported` |
| Real-repo validation | `supported` |
| End-to-end indexing | `supported` |
| Dead-code exactness | `derived`, not cleanup-safe exact truth |

## Framework And Library Support

Supported today:

- Express and Hapi route registrations emit `route_entries`; `handler` is
  recorded only for exact named handlers so the reducer can project exact
  `HANDLES_ROUTE` edges without guessing.
- Next.js app-router `route.{js,jsx,ts,tsx}` modules emit one `route_entries`
  row per directly exported HTTP method handler, and `pages/api` modules emit an
  `ANY` route entry only when a named default handler is exact. Pages, layouts,
  anonymous defaults, rewrites, middleware matchers, generated route manifests,
  and plugin conventions remain root or unsupported evidence only.
- Koa router calls, Fastify verb calls and route-object registrations, and
  NestJS literal controller/method decorators emit exact `route_entries`.
  `handler` is recorded only for exact named handlers or methods. Middleware
  chains, plugin loading, computed paths/methods, generated route maps, and
  DI/container-only behavior remain root or unsupported evidence only.
- Node package entrypoints, `bin` targets, package exports, migrations, seeds,
  AMQP consumers, and bounded AWS/GCP SDK evidence are modeled as live roots.

Not claimed today:

- Runtime plugin loading, dynamic imports, computed property dispatch, and
  dynamic `require()` targets are not cleanup-safe reachability truth.
- Framework behavior that only exists in generated files, runtime config, or
  plugin conventions is not supported unless a parser test names that pattern.

## Known Limitations

- Runtime-dependent computed expressions, dynamic `require()` targets,
  runtime plugin loading, package export breadth, and declaration/API precision
  remain outside the exactness boundary.
- A JavaScript, TypeScript, or TSX file larger than 1 MiB has its tree-sitter
  parse skipped entirely in the normal parse stage (the shared
  javascript-family parser bounds TypeScript and TSX identically), to bound
  superlinear tree-sitter parse cost on very large generated files such as
  minified webpack bundles (#4766). No entities are extracted from a bounded
  file. The bound is recorded in `payload["js_parse_bounded"]` and logged,
  never silently dropped. The repository pre-scan stage is bounded by the
  same cap ([#4808](https://github.com/eshu-hq/eshu/issues/4808)): a bounded
  file contributes no pre-scan names and the bound is logged, since pre-scan
  has no payload map to carry a `js_parse_bounded` row.

## Parser Performance

The JavaScript/TypeScript parser folds independent per-file, full-tree
tree-sitter walks into shared passes: the embedded-shell import-alias and
enclosing-function scans run in one traversal, and the React-alias,
CommonJS-export, new-expression-type, and Fastify-base index builders run in
one shared dispatch walk gated per collector exactly as the originals were.
This lowers the always-on root-walk count in that path from 7 to 3 while
keeping parser output byte-identical, verified by a one-time old-vs-new `0/0`
symmetric-diff over the fixture corpus via the opt-in `JSTS_PARSE_DUMP` harness
(`equivalence_dump_test.go`, a manual differential — not a standing CI gate);
standing regression protection comes from the JS/TS parser package tests and
the B-12 golden snapshot (epic #4831, #4868). This is distinct from the shipped
TypeScript public-surface reexport BFS cache (#4765), which it does not touch.
Contributors adding a new index builder should extend the shared dispatch walk
rather than add another full-tree walk when the builder has no dependency on
another builder's completed output.

The Fastify registration bases computed inside that shared dispatch walk were
previously computed again in two downstream consumers
(`javaScriptFrameworkRegisteredDeadCodeRootKinds` and
`detectFastifySemantics`), each running its own full-tree traversal. The
precomputed map is now threaded from `buildJavaScriptRootIndexes` through
both call chains, removing two redundant traversals per file that imports
Fastify (#4905).

Performance Evidence: microbenchmark against a synthetic 4500-line Fastify-fixture
file (500 routes each over GET/POST/PUT, 500 handlers) measured before-and-after
on an Apple M5 Max, `go test -bench BenchmarkParseFastifyFixture -benchmem`:

- Before (3× compute): 226 ms/op, 133.0 MB/op, 1,398,488 allocs/op
- After  (1× compute): 200 ms/op, 128.5 MB/op, 1,239,340 allocs/op
- Delta: −11.5 % ns, −3.4 % memory, −11.4 % allocs

Output is byte-identical on the fixture corpus (JSTS_PARSE_DUMP manual
differential 0/0), the B-12 golden snapshot is unchanged, and the JS/TS parser
package tests stay green.

No-Observability-Change: the threading removes internal recomputation; no
metric, span, structured log, status field, queue, graph-write, worker, lease,
batch, or runtime knob is added or removed. Operators still diagnose parser
behavior through the existing collector `telemetry.FileParseDuration` instrument
and parse-stage logs.
