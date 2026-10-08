# TypeScript Parser

This page describes the current Go parser and query contract for TypeScript.

## Parser Contract

| Field | Value |
| --- | --- |
| Language | `typescript` |
| Parser | `DefaultEngine (typescript)` |
| Entrypoint | `go/internal/parser/javascript_language.go` |
| Fixture repo | `tests/fixtures/ecosystems/typescript_comprehensive/` |
| Main parser tests | `go/internal/parser/javascript/engine_semantics_test.go`, `go/internal/parser/javascript/engine_typescript_advanced_semantics_test.go` |
| Runtime validation | Compose-backed fixture verification; see [Local Testing](../reference/local-testing.md) |

## Supported Surfaces

| Surface | Current contract |
| --- | --- |
| Source entities | Functions, classes, interfaces, imports, variables, enums, modules, namespaces, type aliases, and declaration-merge groups. |
| Type metadata | Type parameters, mapped and conditional type aliases, decorators, type references, and declaration-merge metadata. |
| Framework and package roots | JavaScript-family Node package, React, Next.js, Express, Koa, Fastify, NestJS, Hapi, AWS SDK, and GCP SDK packs. |
| Query surfacing | `code/language-query`, `code/search`, entity resolve/context, relationships, complexity, and dead-code responses preserve TypeScript metadata when graph or content rows carry it. |
| Cross-repository package calls | A real call bound to a bare package import that a `package.json` between the file and the repository root declares as a dependency (`local()`, `new Local()`, `<Local />`, `ns.member()` on a namespace import) carries `package_export_symbol`; an exported top-level function or class carries `package_id` (nearest `package.json` name) and `export_name`. The reducer links the call only when exactly one indexed definition carries the key. Type references, type-only imports, subpaths, default-import members, static or instance method calls, export clauses, and CommonJS producers are not linked yet (#7601). A `jsconfig.json` alias resolves in-repo like a `tsconfig.json` one and stays unkeyed even when the name is also declared; an npm alias dependency is keyed under its target package, and an unparseable target stays unkeyed (#7613). Files owned by a `package.json` also carry `node_package_name` (nearest `package.json` name; absent when no manifest owns the file); an unresolved key to an external package no longer falls back to a same-named in-repository function, while a key to a same-repository workspace package keeps its repo-unique fallback, so same-repository export-clause and CommonJS producers link when their name is repo-unique (#7610). |

## Capability Claim Ledger

| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Source entities | `source-entities` | supported | parser buckets | `name, line_number` where applicable | `execute_language_query` | `go/internal/parser/javascript/engine_typescript_advanced_semantics_test.go` | Compose-backed fixture verification | Tree-sitter-backed TypeScript entity extraction. |
| Type metadata | `type-metadata` | supported | parser metadata buckets | type/decorator metadata where applicable | `execute_language_query` | `go/internal/parser/javascript/engine_typescript_advanced_semantics_test.go`, `go/internal/query/typescript_graph_metadata_test.go` | Compose-backed fixture verification | Deterministic TypeScript metadata, no provider key. |
| JavaScript-family roots | `javascript-family-roots` | supported | `dead_code_root_kinds`, package metadata, framework metadata | source-proven root kind and location | `find_dead_code` | `go/internal/query/codequery/deadcode/node_typescript_matrix_test.go`, `go/internal/query/codequery/deadcode/typescript_semantics_test.go` | Compose-backed fixture verification | Derived root evidence applies the JavaScript-family root model to TypeScript. |
| Express/Hapi route truth | `express-hapi-route-truth` | supported | `framework_semantics.route_entries` | `method, path`; `handler` only for exact named handlers | `HANDLES_ROUTE` when reducer can resolve the exact handler | `go/internal/parser/javascript/engine_ast_conversion_test.go::TestDefaultEngineParsePathExpressESMRoutesFromAST`, `go/internal/reducer/code/call/materialization/routes_test.go` | Shared reducer route projection proof | TypeScript uses the JavaScript parser path for exact Express/Hapi route entries. |
| Next.js route-handler truth | `nextjs-route-handler-truth` | supported | `framework_semantics.nextjs.route_entries` | `method, path, handler` for app-router exported HTTP method handlers; `ANY, path, handler` for named `pages/api` default exports | `HANDLES_ROUTE` when reducer can resolve the exact handler | `go/internal/parser/javascript/engine_nextjs_route_entries_test.go`, `go/internal/reducer/code/call/materialization/routes_test.go` | Shared reducer route projection proof | Exact Next.js route handlers emit route entries through the JavaScript-family parser; page/layout roots, anonymous defaults, rewrites, middleware matchers, generated manifests, and plugin conventions do not fabricate handler edges. |
| Koa/Fastify/NestJS route truth | `koa-fastify-nestjs-route-truth` | supported | `framework_semantics.{koa,fastify,nestjs}.route_entries` | `method, path`; `handler` only for exact named handlers or methods | `HANDLES_ROUTE` when reducer can resolve the exact handler | `go/internal/parser/javascript/engine_koa_fastify_nestjs_route_entries_test.go`, `go/internal/reducer/code/call/materialization/routes_javascript_frameworks_test.go`, `go/internal/query/content_reader_framework_routes_test.go` | Parser-to-reducer-to-query route-entry proof | Literal Koa router calls, Fastify verb/route-object registrations (including typed-parameter and autoload/plugin patterns such as `const plugin: FastifyPluginAsyncTypebox = async (fastify) => { fastify.get(...) }`), and NestJS literal controller/method decorators emit exact route entries through the JavaScript-family parser. Middleware chains, plugin loading, computed paths/methods, generated routes, and DI/container-only behavior stay unclaimed. |
| Query surfacing | `query-surfacing` | supported | graph/content query rows | TypeScript metadata when present | `code/language-query`, `get_code_relationship_story`, `find_dead_code` | `go/internal/query/typescript_graph_metadata_test.go` | Compose-backed fixture verification | Query paths preserve TypeScript metadata when graph or content rows carry it. |
| Outbound contracts | `outbound-contracts` | partial | - | - | - | `go/internal/parser/javascript/engine_semantics_test.go` | Explicit unsupported-contract wording on this page | SDK/client evidence does not create deterministic cross-repo outbound contract edges today. |
| Decorator/container behavior | `decorator-container-behavior` | partial | - | - | - | Support-maturity guardrails | Explicit decorator/container wording on this page | Decorators are metadata today, not whole-framework runtime reachability truth. |
| Generated clients | `generated-clients` | partial | - | - | - | Support-maturity guardrails | Explicit generated-client wording on this page | Generated clients and runtime route/client manifests are not parser-owned route or contract truth. |
| Dead-code roots | `dead-code-roots` | derived | `dead_code_root_kinds` | modeled root kind and source location | `find_dead_code` | `go/internal/query/codequery/deadcode/node_typescript_matrix_test.go` | Compose-backed fixture verification | Derived liveness roots are not cleanup-safe exact truth. |

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
- Next.js app-router `route.{js,jsx,ts,tsx}` modules emit one exact
  `route_entries` row per directly exported HTTP method handler, and `pages/api`
  modules emit an `ANY` route entry only when a named default handler is exact.
- Koa router calls, Fastify verb calls and route-object registrations (including
  typed-parameter and autoload/plugin patterns such as
  `const plugin: FastifyPluginAsyncTypebox = async (fastify) => { fastify.get(...) }`),
  and NestJS literal controller/method decorators emit exact `route_entries`.
  `handler` is recorded only for exact named handlers or methods. Middleware
  chains, plugin loading, computed paths/methods, generated route maps, and
  DI/container-only behavior remain root or unsupported evidence only.
- Supported roots include package entrypoints, package exports, scripts,
  migrations, and seeds.
- TypeScript adds interface implementations, module-contract exports, public
  API exports and reexports, and public API type references.

Not claimed today:

- Decorator/container behavior is not modeled as whole-framework reachability.
- Dynamic imports, plugin loading, runtime property dispatch, and broad package
  declaration surfaces remain exactness blockers.
- A TypeScript file larger than 1 MiB has its tree-sitter parse skipped
  entirely in the normal parse stage (the shared javascript-family parser
  bounds JavaScript, TypeScript, and TSX identically); see
  [JavaScript Parser](javascript.md#known-limitations) for the bound, which
  also covers the repository pre-scan stage (#4766,
  [#4808](https://github.com/eshu-hq/eshu/issues/4808)).

## Parser Performance

The TypeScript parser shares the JavaScript-family parser performance
characteristics described in [JavaScript Parser Performance](javascript.md#parser-performance),
including the Fastify registration-base threading optimization (#4905) and the
framework semantics gather-resolve optimization (#4925) that eliminates
per-framework full-tree re-walks in `buildJavaScriptFrameworkSemantics`.

No-Observability-Change: same as the JavaScript-family parser — no metric,
span, structured log, status field, queue, graph-write, worker, lease, batch,
or runtime knob is added or removed.

## Import Flags

TypeScript import rows carry `type_only: true` when the import is erased at
compile time and so cannot close a runtime import cycle (issue #7344). The
flag is set for `import type { A } from "m"`, `import type D from "m"`,
`import type * as NS from "m"`, `import type X = require("m")`, a per-specifier
modifier such as `import { type B, C } from "m"` (only `B` is flagged), and for
`export type { A } from "./m"`, `export type * from "./m"`, and
`export { type Z, W } from "./m"`. Value imports, side-effect imports, and
`export { I } from "./m"` never carry it. A flag is present only when true.

The parser sets no `deferred` or `inferred` flag for TypeScript or JavaScript:
the tsconfig resolver only returns files that exist on disk, and dynamic
`import()` is not part of the import bucket. Some spellings stay unflagged on
purpose or by limit, and all of them err toward keeping the edge:
`export { type as Y } from "m"` re-exports the value named `type` and is not
type-only; `export { type as }` and `import { type as as Y } from "m"` (a
type-only use of a binding named `as`) are left unflagged because the grammar
yields the same node as the value spelling; and `export type * from "m" with
{ ... }` (a star re-export with an import attribute, recovered from a grammar
error shape) loses its statement-level `type`. The fixture
gates are `TestDefaultEngineParsePathTypeScriptImportFlags` and
`TestDefaultEngineParsePathTypeScriptReExportTypeModifierEdges`.

## Related Docs

- [TypeScript JSX Parser](typescriptjsx.md)
- [Dead Code Language Maturity](../reference/dead-code-language-maturity.md)
- [Parser Support Matrix](support-maturity.md)
