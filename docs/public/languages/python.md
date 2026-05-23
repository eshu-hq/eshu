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

## Dead-Code Support

Python dead-code support is `derived`. Modeled roots include FastAPI, Flask,
Celery, Click, Typer, script-main guards, AWS Lambda handlers, dataclasses,
properties, dunder protocol methods, `__all__`, package reexports, and bounded
public API evidence.

It is not cleanup-safe exact truth. Dynamic imports, monkey-patching, plugin
discovery, dependency injection, and runtime reflection can keep symbols live
without a static edge.

## Related Docs

- [Dead Code Language Maturity](../reference/dead-code-language-maturity.md)
- [Parser Support Matrix](support-maturity.md)
- [Language Query DSL](../reference/language-query-dsl.md)
