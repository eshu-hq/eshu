# Python Parser

This page describes the current Go parser contract for Python.

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

The Python parser emits source entities, semantic metadata, and root hints used
by graph-backed query surfaces.

| Surface | Current contract |
| --- | --- |
| Functions and classes | Emits named functions, classes, variables, imports, function calls, inheritance, and module rows. |
| Module docstrings | Preserves module docstrings as first-class module metadata and query/story signal. |
| Decorators | Preserves decorator metadata for graph-backed language query, search, resolve, context, story, and dead-code surfaces. |
| Async and generators | Marks async functions and generator functions so semantic summaries can distinguish them. |
| Lambda assignments | Materializes identifier, attribute, and anonymous inline lambdas as function entities with `semantic_kind=lambda`; anonymous lambdas use synthetic `lambda@<line>_<column>` names. |
| Type annotations | Emits function and assignment type-annotation signal; graph-backed rows keep compact annotation summaries and first-class `TypeAnnotation` entities where materialized. |
| Metaclasses | Preserves metaclass metadata and persists `USES_METACLASS` relationships on the normal graph-backed relationship path. |
| Receiver and constructor evidence | Tracks class context, simple constructor calls, `self` receiver calls, class receiver references, and inherited classmethod evidence. |

## Dead-Code Support

Python dead-code support is `derived`. Eshu models source-proven roots and
reference evidence, then keeps cleanup truth conservative because Python can
load code dynamically.

Modeled roots and evidence include:

- FastAPI, Flask, Celery, Click, and Typer callbacks.
- Python script-main guards.
- AWS Lambda handlers declared in bounded SAM, CloudFormation, or Serverless
  configuration.
- Dataclasses, dataclass post-init hooks, properties, dunder protocol methods,
  and class receiver references.
- Same-module `__all__`, package `__init__.py` reexports, public base classes,
  and public methods on parser-proven public classes.
- Constructor calls, `self` receiver calls, inherited classmethods,
  cross-file imports, and anonymous lambda suppression.

Focused coverage lives in:

- `go/internal/parser/python_dead_code_roots_test.go`
- `go/internal/parser/engine_python_dead_code_semantics_test.go`
- `go/internal/query/code_dead_code_python_roots_test.go`
- `go/internal/query/code_dead_code_python_public_roots_test.go`

## Capability Checklist

| Capability | Status | Notes |
| --- | --- | --- |
| Functions, classes, imports, function calls, variables, inheritance | supported | Base Python extraction path. |
| Module docstrings | supported | Query and story surfaces preserve documented-module signal. |
| Decorators | supported | Metadata survives graph-backed and content-backed query enrichment. |
| Async functions and generator functions | supported | Semantic summaries expose async/generator surface kinds. |
| Lambda assignments | supported | Named and synthetic lambda functions participate in graph-first modeling. |
| Type annotations | supported | Compact graph projections plus content fallback preserve annotation signal. |
| Metaclass relationships | supported | `USES_METACLASS` edges are persisted for graph-backed relationship queries. |
| Dead-code roots | derived | Parser/query roots suppress known live Python entrypoints, callbacks, model helpers, protocol callbacks, and bounded public APIs. |

## Known Limitations

- Dead-code results remain `derived`, not exact cleanup-safe truth.
- Dynamic imports, monkey-patching, plugin discovery, dependency injection, and
  runtime reflection can keep symbols live without a static edge.
- Remaining validation is broader corpus breadth; the documented Python parser
  and query surfaces are implemented on the Go path.

## Related Docs

- [Dead Code Reachability Spec](../reference/dead-code-reachability-spec.md)
- [Parser Support Matrix](support-maturity.md)
- [Language Query DSL](../reference/language-query-dsl.md)
