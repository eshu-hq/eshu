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
| Django/DRF routes | `django-drf-routes` | partial | - | - | - | `go/internal/parser/python/semantics_test.go::TestBuildPythonFrameworkSemanticsUnknownDecoratorRemainsUnclassified` | Explicit unsupported-route wording on this page | Django ORM table hints exist, but Django/DRF URL routing is not emitted as route entries or `HANDLES_ROUTE` truth; tracked by #4092. |
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
- Celery task decorators, Click and Typer command callbacks, and AWS Lambda
  handler shapes are modeled as entrypoint roots.
- Dataclasses, properties, dunder protocol methods, `__all__`, package
  reexports, and bounded public API evidence protect known live API surfaces.

Not claimed today:

- Django and DRF URL routing, including `urlpatterns`, class-based views, and
  router registrations, are not emitted as `route_entries` and are not eligible
  for `HANDLES_ROUTE`; follow-up work is tracked in #4092.
- aiohttp and Tornado route declarations are not audited route roots; follow-up
  work is tracked in #4093.
- `requests`, `httpx`, gRPC stubs, Celery task topics, and generated clients do
  not create deterministic outbound contract edges.
- SQLAlchemy ORM semantics are not modeled as reachability truth.
- Plugin discovery, monkey-patching, dependency injection, and runtime
  reflection remain exactness blockers.

## Related Docs

- [Dead Code Language Maturity](../reference/dead-code-language-maturity.md)
- [Parser Support Matrix](support-maturity.md)
- [Language Query DSL](../reference/language-query-dsl.md)
