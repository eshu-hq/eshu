# Python Parser

This page describes the current Go parser and query contract for Python.

## Parser Contract

| Field | Value |
| --- | --- |
| Language | `python` |
| Parser | `DefaultEngine (python)` |
| Entrypoint | `go/internal/parser/python_language.go` |
| Registry | `go/internal/parser/registry.go` |
| Fixture repo | `tests/fixtures/ecosystems/python_comprehensive/` |
| Main parser tests | `go/internal/parser/engine_python_semantics_test.go`, `go/internal/parser/engine_python_metaclass_test.go` |
| Runtime validation | Compose-backed fixture verification; see [Local Testing](../reference/local-testing.md) |

## Supported Surfaces

| Surface | Current contract |
| --- | --- |
| Source entities | Functions, classes, variables, imports, calls, inheritance, and module rows. |
| Semantic metadata | Module docstrings, decorators, async/generator flags, lambda assignments, type annotations, and metaclass metadata. |
| Relationships | Constructor evidence, receiver calls, inherited classmethod evidence, and `USES_METACLASS` edges where parsed. |
| Query surfacing | Graph-backed and content-backed language query, search, resolve, context, story, and dead-code surfaces preserve Python metadata when present. |

## Capability Claim Ledger

| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Source entities | `source-entities` | supported | `functions`, `classes`, `modules`, `variables`, `imports`, `function_calls` | `name, line_number` where applicable | `execute_language_query` | `go/internal/parser/engine_python_semantics_test.go` | Compose-backed fixture verification | Tree-sitter-backed Python entity extraction. |
| Semantic metadata | `semantic-metadata` | supported | parser metadata buckets | `language, semantic_kind` where applicable | `execute_language_query` | `go/internal/parser/engine_python_semantics_test.go` | Compose-backed fixture verification | Deterministic parser metadata, no provider key. |
| Relationships | `relationships` | supported | call and receiver evidence | source and target symbol evidence | `get_code_relationship_story` | `go/internal/parser/engine_python_call_semantics_test.go` | Compose-backed fixture verification | Relationship evidence remains parser/query-backed, not semantic inference. |
| FastAPI/Flask route roots | `fastapi-flask-route-roots` | supported | `framework_semantics.route_entries` | `method, path`; `handler` only when a decorator wraps an exact `def` | `HANDLES_ROUTE` when reducer can resolve the exact handler | `go/internal/parser/python/semantics_test.go::TestBuildPythonFrameworkSemanticsFastAPIRouterPrefix`, `go/internal/parser/python/semantics_test.go::TestBuildPythonFrameworkSemanticsFlaskMultipleMethods` | B-7 `rc-8` `HANDLES_ROUTE` gate through the Flask `api-svc` fixture | Exact route evidence only; orphan or ambiguous decorators do not fabricate handlers. |
| Django/DRF routes | `django-drf-routes` | supported | `framework_semantics.route_entries` | `method, path`; `handler` only for same-file function identifiers, same-file class-view methods, literal DRF `as_view` action maps, and bounded router/viewset actions | `HANDLES_ROUTE` when reducer can resolve the exact handler | `go/internal/parser/python/django_drf_routes_test.go`, `go/internal/parser/engine_python_django_drf_routes_test.go`, `go/internal/reducer/handles_route_intents_test.go::TestBuildHandlesRouteIntentRowsResolvesClassMethodHandler` | Shared reducer route projection proof; route-to-caller API/MCP readbacks use existing `HANDLES_ROUTE` truth | Bounded Django `path(...)`, DRF `ViewSet.as_view({...})`, router registrations, literal URLconf router mounts, and literal `@action` routes emit exact entries; dynamic `include()`, generated URLconf, runtime resolver/plugin discovery, and nonliteral action maps stay unclaimed. |
| aiohttp/Tornado routes | `aiohttp-tornado-routes` | supported | `framework_semantics.route_entries` | `method, path`; `handler` for same-file aiohttp functions, imported identifiers used as param-app handlers, or same-file Tornado request-handler methods | `HANDLES_ROUTE` when reducer can resolve the exact handler | `go/internal/parser/python/aiohttp_tornado_routes_test.go`, `go/internal/parser/engine_python_aiohttp_tornado_routes_test.go`, `go/internal/reducer/handles_route_python_framework_intents_test.go` | Shared reducer route projection proof; route-to-caller API/MCP readbacks use existing `HANDLES_ROUTE` truth | Bounded aiohttp `RouteTableDef` decorators, literal `app.router.add_*`, literal `app.router.add_route`, literal `app.add_routes([web.*(...)])`, injected-param `setup_routes(app)` detection, and Tornado `Application` URL specs emit exact entries. Nonliteral path/method/handler values, imported Tornado handlers, app factories, generated route lists, plugin loading, and runtime-discovered routes stay unclaimed. |
| Outbound contracts | `outbound-contracts` | partial | - | - | - | `go/internal/parser/python/semantics_test.go::TestBuildPythonFrameworkSemanticsUnknownDecoratorRemainsUnclassified` | Explicit unsupported-contract wording on this page | `requests`, `httpx`, gRPC, Celery topic, and generated-client usage do not create deterministic cross-repo contract edges today. |
| Dead-code roots | `dead-code-roots` | derived | `dead_code_root_kinds` | modeled root kind and source location | `find_dead_code` | `go/internal/parser/python_dead_code_roots_test.go` | Compose-backed fixture verification | Derived liveness roots are not cleanup-safe exact truth. |

## Dead-Code Support

Python dead-code support is `derived`. Modeled roots include FastAPI, Flask,
Celery, Click, Typer, script-main guards, AWS Lambda handlers, dataclasses,
properties, dunder protocol methods, `__all__`, package reexports, and bounded
public API evidence.

It is not cleanup-safe exact truth. Dynamic imports, monkey-patching, plugin
discovery, dependency injection, and runtime reflection can keep symbols live
without a static edge.

## Framework And Library Support

Supported today:

- FastAPI and Flask route decorators are modeled as framework roots and route
  entries. A `handler` is recorded only when the decorator applies to an exact
  Python `def` or `async def`; unresolved or orphan decorators keep the route
  evidence but do not invent handler truth.
- Bounded Django and DRF routes emit route entries when the parser sees literal
  `path(...)` patterns under `urlpatterns`, same-file function identifiers,
  same-file class-view methods, literal DRF `ViewSet.as_view({...})` method/action maps, router
  registrations with literal URLconf router mounts or `urlpatterns = router.urls`,
  or literal `@action` routes.
  Django route literals preserve their trailing slash shape; DRF router entries
  use DRF's trailing slash convention and include literal mount prefixes such as
  `path("api/", include(router.urls))`. Django URLconf entries without an HTTP
  verb use `ANY`; DRF method/action maps and router actions carry the exact
  HTTP method. `HANDLES_ROUTE` is projected only when the reducer resolves the
  exact function or class-method handler.
- Bounded aiohttp and Tornado routes emit route entries only for exact static
  declarations: aiohttp `RouteTableDef` decorators, literal
  `app.router.add_*` and `app.router.add_route(...)` calls on module-level app
  symbols or on injected function parameters (the `setup_routes(app)` pattern),
  literal `app.add_routes([web.*(...)])` entries, and Tornado `Application` URL specs
  with same-file `RequestHandler` methods. For injected-param routes the handler
  identifier is recorded regardless of whether it is defined in the same file
  (standard aiohttp demo pattern: imported route handlers). Module-level
  aiohttp call-style handlers must still be same-file functions; Tornado
  handler classes must be same-file classes with exact HTTP verb methods.
  `HANDLES_ROUTE` is projected only when the reducer resolves those exact
  function or class-method handlers.
- Celery task decorators, Click and Typer command callbacks, and AWS Lambda
  handler shapes are modeled as entrypoint roots.
- Dataclasses, properties, dunder protocol methods, `__all__`, package
  reexports, and bounded public API evidence protect known live API surfaces.

Not claimed today:

- Dynamic aiohttp route construction, nonliteral aiohttp path/method/handler
  values, app factory indirection, imported Tornado handler attributes,
  generated Tornado URL specs, plugin loading, and runtime-discovered routes
  are not emitted as exact route truth.
- Dynamic Django `include()`, generated URLconfs, runtime resolver/plugin
  discovery, nonliteral route strings, dynamic DRF router prefixes, and nonliteral
  `as_view` action maps are not emitted as `route_entries` and are not eligible
  for `HANDLES_ROUTE`.
- Imported Django view targets such as `from .views import health`,
  `views.index`, and `views.ReportView.as_view()` keep method/path route
  evidence but do not emit a `handler` or `HANDLES_ROUTE` edge until import-aware
  handler resolution is available.
- DRF routers without mount evidence, or mounted through nonliteral URLconf
  prefixes, stay unclaimed instead of falling back to root-prefixed route
  evidence.
- `requests`, `httpx`, gRPC stubs, Celery task topics, and generated clients do
  not create deterministic outbound contract edges.
- SQLAlchemy ORM semantics are not modeled as reachability truth.
- Plugin discovery, monkey-patching, dependency injection, and runtime
  reflection remain exactness blockers.

## Parser Performance

The Python parser consolidates its per-file, full-tree tree-sitter walks so
independent index builders that only read the AST (dataclass-decorated class
names, `if __name__ == "__main__"` guard call roots, and module `__all__`
exports) run in a single pass instead of one walk each, and the duplicate
FastAPI server-symbol walk is folded into one traversal (epic #4831, #4867).
Parser output is byte-identical before and after this change, verified by a
one-time old-vs-new `0/0` symmetric-diff over the fixture corpus via the opt-in
`PY_PARSE_DUMP` harness (`equivalence_dump_test.go`, a manual differential —
not a standing CI gate); standing regression protection comes from the Python
parser package tests and the B-12 golden snapshot. Contributors adding new
index builders should extend the shared pass rather than adding another
full-tree walk when the new builder has no dependency on another builder's
completed output.

- **Framework-route re-walk elimination (#4922, epic #4917):**
  `buildPythonFrameworkSemantics` (`go/internal/parser/python/framework_routes.go`)
  and `buildPythonORMTableMappings` were dispatching every per-framework detector
  (FastAPI, Flask, Django, DRF, aiohttp, Tornado) to run independent full-tree
  `walkNamed` traversals for each file — separate walks for server-symbol
  assignments, route decorators, call-site routes, import-statement scanning,
  HTTP-method-by-class mapping, and class-definition ORM-table extraction. Each
  detector's walks were redundant because the main parse walk already visits
  every named node. Resolution-candidate nodes (assignment, decorator, call,
  function_definition, class_definition, import_statement/import_from_statement)
  are now gathered (cloned) during a single `gatherPythonFrameworkNodes` pass,
  and every framework detector resolves against the pre-gathered in-memory slices
  via plain `for` loops — eliminating all per-framework full-tree walks. The
  old code ran up to ~20 `walkNamed` traversals per file; the new code runs
  exactly 2 (the main walk + the gather walk).

Performance Evidence: throwaway microbenchmark parsing `deadcode/python/app.py`
(85-LOC Python with FastAPI + Flask routes, Apple M5 Max):
  - **BEFORE** (merge-base 8085fd1b8): **1,821,348 ns/op**, **301,979 B/op**, **12,493 allocs/op**
  - **AFTER** (gather-then-resolve): **414,693 ns/op**, **41,448 B/op**, **1,507 allocs/op**
  - Delta: **-77.2% wall**, **-86.3% memory**, **-87.9% allocs** (framework-semantics + ORM path only)

`BenchmarkParse/python` (10,019-LOC corpus, 10-count):
  - **BEFORE** (merge-base 8085fd1b8): ~10.6s/op, ~1,875 MB/op, ~75.7M allocs/op
  - **AFTER** (gather-then-resolve): ~9.4s/op, ~1,841 MB/op, ~74.2M allocs/op
  - Delta: **-11% wall**, **-1.8% memory**, **-1.9% allocs** (full `Parse`)

No-Regression Evidence: parser output is byte-identical old-vs-new (the `0/0`
symmetric-diff over the full fixtures corpus via `PY_PARSE_DUMP`, plus
byte-identical `diff` output), so no accuracy regression is possible; the
change only removes duplicate traversal work.

No-Observability-Change: this is a pure structural walk consolidation with no
runtime-behavior change and no new metric/span/log surface. Per-file Python parse
timing is already covered by the existing parser parse-stage telemetry; no new
operator signals are warranted.

## Related Docs

- [Dead Code Language Maturity](../reference/dead-code-language-maturity.md)
- [Parser Support Matrix](support-maturity.md)
- [Language Query DSL](../reference/language-query-dsl.md)
