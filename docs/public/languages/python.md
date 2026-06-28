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
| aiohttp/Tornado routes | `aiohttp-tornado-routes` | partial | - | - | - | `go/internal/parser/python/semantics_test.go::TestBuildPythonFrameworkSemanticsUnknownDecoratorRemainsUnclassified` | Explicit unsupported-route wording on this page | aiohttp and Tornado route declarations are not audited as route entries or handler bindings; tracked by #4093. |
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
- Celery task decorators, Click and Typer command callbacks, and AWS Lambda
  handler shapes are modeled as entrypoint roots.
- Dataclasses, properties, dunder protocol methods, `__all__`, package
  reexports, and bounded public API evidence protect known live API surfaces.

Not claimed today:

- aiohttp and Tornado route declarations are not audited route roots; follow-up
  work is tracked in #4093.
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

## Related Docs

- [Dead Code Language Maturity](../reference/dead-code-language-maturity.md)
- [Parser Support Matrix](support-maturity.md)
- [Language Query DSL](../reference/language-query-dsl.md)
