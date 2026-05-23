# Java Parser

This page describes the current Go parser and query contract for Java. Detailed
parser mechanics live in `go/internal/parser/java/README.md`.

## Parser Contract

| Field | Value |
| --- | --- |
| Language | `java` |
| Family | `language` |
| Parser | `DefaultEngine (java)` |
| Entrypoint | `go/internal/parser/java_language.go` |
| Package detail | `go/internal/parser/java/README.md` |
| Fixture repo | `tests/fixtures/ecosystems/java_comprehensive/` |
| Main parser tests | `go/internal/parser/engine_managed_oo_test.go`, `go/internal/parser/java_*_test.go` |
| Runtime validation | Compose-backed fixture verification; see [Local Testing](../reference/local-testing.md) |

## Supported Surfaces

| Surface | Current contract |
| --- | --- |
| Source entities | Methods, constructors, classes, interfaces, enums, annotation types, local variables, fields, imports, method invocations, and object creation. |
| Call metadata | Method and constructor arity, local receiver type inference, argument counts, typed receiver variables, records, nested-class context, and same-class helper return types. |
| Annotation metadata | Applied annotations persist as first-class graph entities and remain graph-first on `code/language-query`, with content fallback when the graph is empty. |
| Dead-code roots | Parser metadata and reducer `REFERENCES` edges suppress parser-proven runtime and framework roots from cleanup candidates. |

Primary proof:

- `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathJava`
- `go/internal/parser/java_dead_code_roots_test.go`
- `go/internal/parser/java_dead_code_framework_roots_test.go`
- `go/internal/parser/java_reflection_test.go`
- `go/internal/reducer/code_call_materialization_java_reflection_test.go`
- `go/internal/query/code_dead_code_java_roots_test.go`

## Dead-Code Support

Java dead-code support is `derived`. Eshu suppresses parser-proven runtime
roots and uses reducer-produced `REFERENCES` edges when source or metadata
proves reachability. It does not claim whole-program exactness for broad
reflection or dependency injection.

Modeled roots and evidence include:

- JVM entrypoints: `main`, constructors, `@Override`, serialization hooks, and
  Externalizable hook signatures.
- Build and framework callbacks: Ant setters, Gradle plugin/task/DSL roots,
  Spring roots, JUnit roots, Jenkins/Stapler roots, and lifecycle callbacks.
- Java reference evidence: same-class method references, bounded literal
  reflection, ServiceLoader providers, Spring Boot AutoConfiguration imports,
  and legacy `spring.factories`.
- Overload-sensitive calls: parameter counts, argument counts, parameter types,
  class-literal argument types, typed lambda receivers, and same-class helper
  return types.

## Support Maturity

| Dimension | Status |
| --- | --- |
| Grammar routing | `supported` |
| Normalization | `supported` |
| Framework packs | Spring, Gradle, JUnit, Jenkins, Stapler, ServiceLoader, serialization, bounded reflection |
| Query surfacing | `supported` |
| Real-repo validation | `supported` |
| End-to-end indexing | `supported` |
| Dead-code exactness | `derived`, not cleanup-safe exact truth |

## Known Limitations

- Generic type bounds and wildcards are not captured as structured data beyond
  the receiver type leaf needed for local method-call resolution.
- Anonymous inner classes are not separately tracked.
- Lambda expressions are not individually modeled as functions.
- Dead-code does not infer arbitrary string-built reflection, container-driven
  dependency injection, runtime classpath scanning, or framework behavior that
  is not represented in source or checked metadata files.
